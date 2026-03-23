package controlplane

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// RegisterRequest is the JSON body for POST /api/v1/agents/register.
type RegisterRequest struct {
	Hostname  string            `json:"hostname"`
	OS        string            `json:"os"`
	Version   string            `json:"version"`
	Labels    map[string]string `json:"labels"`
	Detectors []string          `json:"detectors"`
}

// RegisterResponse is returned on successful agent registration.
type RegisterResponse struct {
	AgentID    string `json:"agent_id"`
	AgentToken string `json:"agent_token"`
}

// HeartbeatRequest is the JSON body for PUT /api/v1/agents/{id}/heartbeat.
type HeartbeatRequest struct {
	PolicyVersion int `json:"policy_version"`
}

// HeartbeatResponse is returned from a heartbeat.
type HeartbeatResponse struct {
	LatestPolicyVersion int `json:"latest_policy_version"`
}

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if req.Hostname == "" {
		http.Error(w, `{"error":"hostname is required"}`, http.StatusBadRequest)
		return
	}

	// Serialize labels to JSON string for storage
	labelsJSON := ""
	if len(req.Labels) > 0 {
		b, _ := json.Marshal(req.Labels)
		labelsJSON = string(b)
	}

	// Generate agent ID and token
	agentID := uuid.New().String()
	token, err := generateToken()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to generate agent token")
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	agent := Agent{
		AgentID:       agentID,
		Hostname:      req.Hostname,
		OS:            req.OS,
		Version:       req.Version,
		Labels:        labelsJSON,
		Status:        "active",
		LastHeartbeat: now,
		Detectors:     len(req.Detectors),
		PolicyVersion: 0,
		RegisteredAt:  now,
		TokenHash:     hashToken(token),
	}

	if err := s.store.RegisterAgent(agent); err != nil {
		s.logger.Error().Err(err).Msg("failed to register agent")
		http.Error(w, `{"error":"failed to register agent"}`, http.StatusInternalServerError)
		return
	}

	s.logger.Info().Str("agent_id", agentID).Str("hostname", req.Hostname).Msg("agent registered")

	resp := RegisterResponse{
		AgentID:    agentID,
		AgentToken: token,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// handleAgentAutoRegister handles POST /api/v1/agents/register with API key authentication.
// If an agent with the same hostname already exists, it returns the existing agent_id with a new token.
// If new, it creates the agent and returns agent_id + agent_token.
func (s *Server) handleAgentAutoRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if req.Hostname == "" {
		http.Error(w, `{"error":"hostname is required"}`, http.StatusBadRequest)
		return
	}

	// If authenticated via department API key, auto-set department label
	if deptID := r.Header.Get("X-TrustGate-Dept-ID"); deptID != "" {
		if req.Labels == nil {
			req.Labels = make(map[string]string)
		}
		req.Labels["department"] = deptID
	}

	// Serialize labels to JSON string for storage
	labelsJSON := ""
	if len(req.Labels) > 0 {
		b, _ := json.Marshal(req.Labels)
		labelsJSON = string(b)
	}

	// Generate a new token regardless
	token, err := generateToken()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to generate agent token")
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Validate department label if departments master is configured
	if deptID, ok := req.Labels["department"]; ok && deptID != "" {
		hasDepts, err := s.store.HasDepartments()
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to check departments")
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		if hasDepts {
			exists, err := s.store.DepartmentExists(deptID)
			if err != nil {
				s.logger.Error().Err(err).Msg("failed to validate department")
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			if !exists {
				depts, _ := s.store.ListDepartments()
				ids := make([]string, 0, len(depts))
				for _, d := range depts {
					ids = append(ids, d.ID)
				}
				resp := map[string]any{
					"error":                "invalid department id",
					"department":           deptID,
					"available_departments": ids,
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(resp)
				return
			}
		}
	}

	// Check if agent with same hostname already exists
	existing, err := s.store.GetAgentByHostname(req.Hostname)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to look up agent by hostname")
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	if existing != nil {
		// Re-registration: update token and metadata
		if err := s.store.UpdateAgentToken(existing.AgentID, hashToken(token), req.OS, req.Version, labelsJSON); err != nil {
			s.logger.Error().Err(err).Msg("failed to update agent token")
			http.Error(w, `{"error":"failed to update agent"}`, http.StatusInternalServerError)
			return
		}

		s.logger.Info().Str("agent_id", existing.AgentID).Str("hostname", req.Hostname).Msg("agent re-registered (existing hostname)")

		resp := RegisterResponse{
			AgentID:    existing.AgentID,
			AgentToken: token,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// New agent registration
	agentID := uuid.New().String()
	now := time.Now().UTC()
	agent := Agent{
		AgentID:       agentID,
		Hostname:      req.Hostname,
		OS:            req.OS,
		Version:       req.Version,
		Labels:        labelsJSON,
		Status:        "active",
		LastHeartbeat: now,
		Detectors:     len(req.Detectors),
		PolicyVersion: 0,
		RegisteredAt:  now,
		TokenHash:     hashToken(token),
	}

	if err := s.store.RegisterAgent(agent); err != nil {
		s.logger.Error().Err(err).Msg("failed to register agent")
		http.Error(w, `{"error":"failed to register agent"}`, http.StatusInternalServerError)
		return
	}

	s.logger.Info().Str("agent_id", agentID).Str("hostname", req.Hostname).Msg("agent auto-registered via API key")

	resp := RegisterResponse{
		AgentID:    agentID,
		AgentToken: token,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")

	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.UpdateHeartbeat(agentID, req.PolicyVersion); err != nil {
		s.logger.Error().Err(err).Str("agent_id", agentID).Msg("failed to update heartbeat")
		http.Error(w, `{"error":"failed to update heartbeat"}`, http.StatusInternalServerError)
		return
	}

	// Return latest policy version
	latestVersion, err := s.store.GetLatestPolicyVersion()
	if err != nil {
		latestVersion = 0
	}

	resp := HeartbeatResponse{
		LatestPolicyVersion: latestVersion,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAgentList(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list agents")
		http.Error(w, `{"error":"failed to list agents"}`, http.StatusInternalServerError)
		return
	}

	if agents == nil {
		agents = []Agent{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"agents": agents,
		"total":  len(agents),
	})
}

// generateToken creates a cryptographically random 32-byte hex token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
