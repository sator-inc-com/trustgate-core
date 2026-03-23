package controlplane

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

// departmentIDPattern allows lowercase letters, digits, hyphens, and underscores.
var departmentIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// DepartmentRequest is the JSON body for creating/updating a department.
type DepartmentRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleDepartmentList(w http.ResponseWriter, r *http.Request) {
	departments, err := s.store.ListDepartments()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list departments")
		http.Error(w, `{"error":"failed to list departments"}`, http.StatusInternalServerError)
		return
	}

	if departments == nil {
		departments = []Department{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"departments": departments,
		"total":       len(departments),
	})
}

func (s *Server) handleDepartmentCreate(w http.ResponseWriter, r *http.Request) {
	var req DepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)

	if req.ID == "" {
		http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
		return
	}
	if !departmentIDPattern.MatchString(req.ID) {
		http.Error(w, `{"error":"id must be lowercase alphanumeric with hyphens/underscores, 1-63 chars"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.CreateDepartment(req.ID, req.Name, req.Description); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "PRIMARY KEY") {
			http.Error(w, `{"error":"department id already exists"}`, http.StatusConflict)
			return
		}
		s.logger.Error().Err(err).Msg("failed to create department")
		http.Error(w, `{"error":"failed to create department"}`, http.StatusInternalServerError)
		return
	}

	s.logger.Info().Str("id", req.ID).Str("name", req.Name).Msg("department created")

	dept, _ := s.store.GetDepartment(req.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dept)
}

func (s *Server) handleDepartmentUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req DepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)

	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.UpdateDepartment(id, req.Name, req.Description); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, `{"error":"department not found"}`, http.StatusNotFound)
			return
		}
		s.logger.Error().Err(err).Msg("failed to update department")
		http.Error(w, `{"error":"failed to update department"}`, http.StatusInternalServerError)
		return
	}

	s.logger.Info().Str("id", id).Str("name", req.Name).Msg("department updated")

	dept, _ := s.store.GetDepartment(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dept)
}

func (s *Server) handleDepartmentDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Check if any agents are using this department
	dept, err := s.store.GetDepartment(id)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get department for delete check")
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if dept == nil {
		http.Error(w, `{"error":"department not found"}`, http.StatusNotFound)
		return
	}
	if dept.AgentCount > 0 {
		http.Error(w, `{"error":"cannot delete department with assigned agents"}`, http.StatusConflict)
		return
	}

	if err := s.store.DeleteDepartment(id); err != nil {
		s.logger.Error().Err(err).Msg("failed to delete department")
		http.Error(w, `{"error":"failed to delete department"}`, http.StatusInternalServerError)
		return
	}

	s.logger.Info().Str("id", id).Msg("department deleted")

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"deleted"}`))
}
