package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/trustgate/trustgate/internal/inspector"
)

// handleInspectFile accepts a multipart file upload for async inspection.
// POST /v1/inspect/file
// Returns immediately with a job ID. Use GET /v1/inspect/file/{id} to poll result.
func (s *Server) handleInspectFile(w http.ResponseWriter, r *http.Request) {
	if s.inspectionQueue == nil {
		http.Error(w, `{"error":"content inspection is disabled — set content_inspection.enabled: true in agent.yaml"}`, http.StatusServiceUnavailable)
		return
	}

	// Parse multipart form (max 10MB default, configurable)
	maxSize := s.cfg.ContentInspection.MaxFileSize
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSize+1024) // +1KB for form overhead
	if err := r.ParseMultipartForm(maxSize); err != nil {
		http.Error(w, `{"error":"file too large or invalid multipart form"}`, http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"missing 'file' field in multipart form"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, `{"error":"failed to read file"}`, http.StatusInternalServerError)
		return
	}

	// Get user from headers
	userID := r.Header.Get("X-TrustGate-User")
	if userID == "" {
		userID = "anonymous"
	}

	jobID := fmt.Sprintf("file-%d", time.Now().UnixNano())
	job := inspector.InspectionJob{
		ID:        jobID,
		Filename:  header.Filename,
		FileType:  header.Header.Get("Content-Type"),
		FileSize:  int64(len(data)),
		UserID:    userID,
		CreatedAt: time.Now(),
		Data:      data,
	}

	s.inspectionQueue.Submit(job)

	s.logger.Info().
		Str("job_id", jobID).
		Str("filename", header.Filename).
		Int64("size", job.FileSize).
		Str("user", userID).
		Msg("file inspection submitted")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"id":     jobID,
		"status": "pending",
	})
}

// handleInspectFileResult returns the result of an async file inspection.
// GET /v1/inspect/file/{id}
func (s *Server) handleInspectFileResult(w http.ResponseWriter, r *http.Request) {
	if s.inspectionQueue == nil {
		http.Error(w, `{"error":"content inspection is disabled"}`, http.StatusServiceUnavailable)
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing job id"}`, http.StatusBadRequest)
		return
	}

	result := s.inspectionQueue.GetResult(id)
	if result == nil {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
