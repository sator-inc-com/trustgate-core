package controlplane

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// handleStatsPush handles POST /api/v1/audit/stats.
func (s *Server) handleStatsPush(w http.ResponseWriter, r *http.Request) {
	var req StatsPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if req.AgentID == "" || req.Period == "" {
		http.Error(w, `{"error":"agent_id and period are required"}`, http.StatusBadRequest)
		return
	}

	// Upsert action x detector stats
	for action, detectors := range req.ByAction {
		for detector, count := range detectors {
			if err := s.store.UpsertActionStats(req.Period, req.AgentID, action, detector, count); err != nil {
				s.logger.Error().Err(err).Msg("failed to upsert action stats")
				http.Error(w, `{"error":"failed to store stats"}`, http.StatusInternalServerError)
				return
			}
		}
	}

	// Upsert user x action stats
	for _, u := range req.ByUser {
		if err := s.store.UpsertUserStats(req.Period, req.AgentID, u.UserID, u.Action, u.Count); err != nil {
			s.logger.Error().Err(err).Msg("failed to upsert user stats")
			http.Error(w, `{"error":"failed to store stats"}`, http.StatusInternalServerError)
			return
		}
	}

	// Upsert policy stats
	for _, p := range req.ByPolicy {
		if err := s.store.UpsertPolicyStats(req.Period, req.AgentID, p.PolicyName, p.TriggerCount, p.FalsePositiveCount); err != nil {
			s.logger.Error().Err(err).Msg("failed to upsert policy stats")
			http.Error(w, `{"error":"failed to store stats"}`, http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleStatsSummary handles GET /api/v1/stats/summary?days=7.
func (s *Server) handleStatsSummary(w http.ResponseWriter, r *http.Request) {
	daysStr := r.URL.Query().Get("days")
	days := 7
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	kpi, err := s.store.GetKPISummary(days)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get KPI summary")
		http.Error(w, `{"error":"failed to get summary"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(kpi)
}
