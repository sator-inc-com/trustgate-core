package inspector

import (
	"context"
	"sync"
	"time"

	"github.com/trustgate/trustgate/internal/config"
	"github.com/trustgate/trustgate/internal/detector"
)

// processTimeout is the maximum time allowed for a single file inspection.
const processTimeout = 30 * time.Second

// resultTTL is how long completed results are kept before auto-cleanup.
const resultTTL = 5 * time.Minute

// InspectionJob represents a file inspection request.
type InspectionJob struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	FileType  string    `json:"file_type"`
	FileSize  int64     `json:"file_size"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	Data      []byte    `json:"-"` // file content, not serialized
}

// InspectionResult holds the result of an async file inspection.
type InspectionResult struct {
	ID        string             `json:"id"`
	Filename  string             `json:"filename"`
	FileType  string             `json:"file_type"`
	Status    string             `json:"status"` // pending | processing | completed | error
	Action    string             `json:"action"`  // allow | warn | block
	Findings  []detector.Finding `json:"findings,omitempty"`
	Error     string             `json:"error,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	DoneAt    *time.Time         `json:"done_at,omitempty"`
}

// Queue processes file inspection jobs asynchronously.
type Queue struct {
	cfg            config.ContentInspectionConfig
	registry       *detector.Registry
	imageInspector *ImageInspector
	results        map[string]*InspectionResult
	mu             sync.RWMutex
	sem            chan struct{} // concurrency limiter
	stopCleanup    chan struct{}
}

// NewQueue creates a new async inspection queue.
// Starts a background goroutine that cleans up results older than 5 minutes.
func NewQueue(cfg config.ContentInspectionConfig, registry *detector.Registry) *Queue {
	maxQueue := cfg.MaxQueue
	if maxQueue <= 0 {
		maxQueue = 4
	}
	q := &Queue{
		cfg:            cfg,
		registry:       registry,
		imageInspector: NewImageInspector(cfg),
		results:        make(map[string]*InspectionResult),
		sem:            make(chan struct{}, maxQueue),
		stopCleanup:    make(chan struct{}),
	}
	go q.cleanupLoop()
	return q
}

// cleanupLoop periodically removes results older than resultTTL.
func (q *Queue) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			q.CleanOld(resultTTL)
		case <-q.stopCleanup:
			return
		}
	}
}

// Stop terminates the background cleanup goroutine.
func (q *Queue) Stop() {
	close(q.stopCleanup)
}

// Submit enqueues a file for async inspection. Returns the job ID immediately.
func (q *Queue) Submit(job InspectionJob) string {
	maxSize := q.cfg.MaxFileSize
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024 // 10MB default
	}

	result := &InspectionResult{
		ID:        job.ID,
		Filename:  job.Filename,
		FileType:  job.FileType,
		Status:    "pending",
		Action:    "allow",
		CreatedAt: job.CreatedAt,
	}

	// Check file size limit
	if job.FileSize > maxSize {
		result.Status = "error"
		result.Error = "file too large"
		now := time.Now()
		result.DoneAt = &now
		q.mu.Lock()
		q.results[job.ID] = result
		q.mu.Unlock()
		return job.ID
	}

	q.mu.Lock()
	q.results[job.ID] = result
	q.mu.Unlock()

	// Process asynchronously
	go q.process(job, result)

	return job.ID
}

// GetResult returns the current result for a job ID.
func (q *Queue) GetResult(id string) *InspectionResult {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.results[id]
}

// CleanOld removes results older than the given duration.
func (q *Queue) CleanOld(maxAge time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for id, r := range q.results {
		if r.CreatedAt.Before(cutoff) {
			delete(q.results, id)
		}
	}
}

func (q *Queue) process(job InspectionJob, result *InspectionResult) {
	// Acquire semaphore (concurrency limit)
	q.sem <- struct{}{}
	defer func() { <-q.sem }()

	q.mu.Lock()
	result.Status = "processing"
	q.mu.Unlock()

	// Run with timeout
	ctx, cancel := context.WithTimeout(context.Background(), processTimeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		q.processInner(job, result)
	}()

	select {
	case <-done:
		// Completed within timeout
	case <-ctx.Done():
		// Timeout — mark as error and give up
		q.mu.Lock()
		result.Status = "error"
		result.Error = "processing timeout (30s exceeded)"
		result.Action = "allow" // fail-safe: allow on timeout
		now := time.Now()
		result.DoneAt = &now
		q.mu.Unlock()
	}
}

func (q *Queue) processInner(job InspectionJob, result *InspectionResult) {
	// Step 1: Extract text from file
	extracted, err := ExtractText(job.Data, job.Filename)
	if err != nil {
		q.mu.Lock()
		result.Status = "error"
		result.Error = err.Error()
		now := time.Now()
		result.DoneAt = &now
		q.mu.Unlock()
		return
	}

	if extracted.Error != "" && extracted.Text == "" {
		q.mu.Lock()
		result.Status = "completed"
		result.Action = "allow"
		result.Error = extracted.Error
		now := time.Now()
		result.DoneAt = &now
		q.mu.Unlock()
		return
	}

	// Step 2: Run text through detectors
	var findings []detector.Finding
	if extracted.Text != "" {
		findings = q.registry.DetectAll(extracted.Text)
	}

	// Also check image keywords and size via ImageInspector
	if extracted.FileType == "image" && q.imageInspector != nil {
		imageFindings := q.imageInspector.InspectImage(job.Filename, job.FileSize)
		findings = append(findings, imageFindings...)
	}

	// Step 3: Determine action based on findings
	action := "allow"
	for _, f := range findings {
		switch f.Severity {
		case "critical":
			action = "block"
		case "high":
			if action != "block" {
				action = "warn"
			}
		}
	}

	q.mu.Lock()
	result.Status = "completed"
	result.Action = action
	result.Findings = findings
	result.FileType = extracted.FileType
	now := time.Now()
	result.DoneAt = &now
	q.mu.Unlock()
}
