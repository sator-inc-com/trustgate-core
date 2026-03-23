package audit

import (
	"strings"
	"sync"
)

// MemoryLogger is an in-memory ring buffer audit logger for Standalone mode.
// Holds the most recent N records. No disk I/O.
type MemoryLogger struct {
	mu      sync.RWMutex
	records []Record
	maxSize int
	head    int // next write position
	count   int // total written (for Count())
	full    bool
}

// NewMemoryLogger creates a ring buffer logger with the given capacity.
func NewMemoryLogger(maxSize int) *MemoryLogger {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &MemoryLogger{
		records: make([]Record, maxSize),
		maxSize: maxSize,
	}
}

func (m *MemoryLogger) Write(r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.records[m.head] = r
	m.head = (m.head + 1) % m.maxSize
	m.count++
	if m.count >= m.maxSize {
		m.full = true
	}
	return nil
}

func (m *MemoryLogger) Query(opts QueryOpts) ([]Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect all records in reverse chronological order
	var all []Record
	size := m.maxSize
	if !m.full {
		size = m.head
	}

	for i := 0; i < size; i++ {
		// Read backwards from head
		idx := (m.head - 1 - i + m.maxSize) % m.maxSize
		r := m.records[idx]
		if r.AuditID == "" {
			continue
		}

		// Apply filters
		if opts.Action != "" && !strings.EqualFold(r.Action, opts.Action) {
			continue
		}
		if opts.UserID != "" && r.UserID != opts.UserID {
			continue
		}
		if opts.SessionID != "" && r.SessionID != opts.SessionID {
			continue
		}
		if !opts.Since.IsZero() && r.Timestamp.Before(opts.Since) {
			continue
		}

		all = append(all, r)

		limit := opts.Limit
		if limit <= 0 {
			limit = 50
		}
		if len(all) >= limit {
			break
		}
	}

	return all, nil
}

func (m *MemoryLogger) Count() (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.full {
		return m.maxSize, nil
	}
	return m.head, nil
}

func (m *MemoryLogger) Close() error {
	return nil
}

// Ensure MemoryLogger implements Logger
var _ Logger = (*MemoryLogger)(nil)
