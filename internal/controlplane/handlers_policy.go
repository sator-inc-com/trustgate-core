package controlplane

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// extractScopeFromYAML extracts the scope field from YAML content.
// Returns "global" if scope is not found or empty.
func extractScopeFromYAML(content string) string {
	re := regexp.MustCompile(`(?m)^scope:\s*(.+)$`)
	matches := re.FindStringSubmatch(content)
	if len(matches) >= 2 {
		scope := matches[1]
		// Trim quotes and whitespace
		scope = regexp.MustCompile(`^['"]|['"]$`).ReplaceAllString(scope, "")
		if scope != "" {
			return scope
		}
	}
	return "global"
}

// CreatePolicyRequest is the JSON body for POST /api/v1/policies.
// Policies can be either:
//   - Full PolicyRecord with name + content (from UI YAML editor)
//   - Raw policy objects with name/phase/when/action fields (from API)
type CreatePolicyRequest struct {
	Comment   string            `json:"comment"`
	CreatedBy string            `json:"created_by"`
	Policies  []json.RawMessage `json:"policies"`
}

// policyInput is used to detect whether content is already provided.
type policyInput struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Scope   string `json:"scope"`
}

// handlePolicyVersionGet handles GET /api/v1/policies/version.
// Lightweight endpoint for agents to check if policies have changed.
func (s *Server) handlePolicyVersionGet(w http.ResponseWriter, r *http.Request) {
	latestVersion, err := s.store.GetLatestPolicyVersion()
	if err != nil {
		http.Error(w, `{"error":"failed to get policy version"}`, http.StatusInternalServerError)
		return
	}

	// Get the updated_at from the latest policy version
	var updatedAt time.Time
	if latestVersion > 0 {
		versions, err := s.store.ListPolicyVersions()
		if err == nil && len(versions) > 0 {
			updatedAt = versions[0].CreatedAt // versions are ordered DESC
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"version":    latestVersion,
		"updated_at": updatedAt.Format(time.RFC3339),
	})
}

// handlePolicyGet handles GET /api/v1/policies.
// For agents: accepts ?version=N query param, returns 304 if same.
// For admins: returns all policies at latest version with version list.
// Supports ?scope=<dept_id|global|all> for filtering by scope.
func (s *Server) handlePolicyGet(w http.ResponseWriter, r *http.Request) {
	scopeFilter := r.URL.Query().Get("scope")

	// Check if agent is requesting with a version
	versionStr := r.URL.Query().Get("version")
	if versionStr != "" {
		requestedVersion, err := strconv.Atoi(versionStr)
		if err != nil {
			http.Error(w, `{"error":"invalid version parameter"}`, http.StatusBadRequest)
			return
		}

		latestVersion, err := s.store.GetLatestPolicyVersion()
		if err != nil {
			http.Error(w, `{"error":"failed to get policy version"}`, http.StatusInternalServerError)
			return
		}

		// Agent already has latest version
		if requestedVersion >= latestVersion {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		// Return latest policies (optionally filtered by scope)
		var policies []PolicyRecord
		if scopeFilter != "" {
			policies, err = s.store.GetPoliciesByVersionAndScope(latestVersion, scopeFilter)
		} else {
			policies, err = s.store.GetPoliciesByVersion(latestVersion)
		}
		if err != nil {
			http.Error(w, `{"error":"failed to get policies"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"version":  latestVersion,
			"policies": policies,
		})
		return
	}

	// Admin request: return latest policies with version history
	latestVersion, policies, err := s.store.GetLatestPolicies()
	if err != nil {
		http.Error(w, `{"error":"failed to get policies"}`, http.StatusInternalServerError)
		return
	}

	// Apply scope filter if provided
	if scopeFilter != "" && latestVersion > 0 {
		policies, err = s.store.GetPoliciesByVersionAndScope(latestVersion, scopeFilter)
		if err != nil {
			http.Error(w, `{"error":"failed to get policies"}`, http.StatusInternalServerError)
			return
		}
	}

	versions, err := s.store.ListPolicyVersions()
	if err != nil {
		http.Error(w, `{"error":"failed to list versions"}`, http.StatusInternalServerError)
		return
	}

	if policies == nil {
		policies = []PolicyRecord{}
	}
	if versions == nil {
		versions = []PolicyVersion{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"current_version": latestVersion,
		"policies":        policies,
		"versions":        versions,
	})
}

// handlePolicyCreate handles POST /api/v1/policies.
// Accepts two formats:
//  1. UI YAML editor: {"policies": [{"name":"...", "content":"<full YAML>"}]}
//  2. API objects: {"policies": [{"name":"...", "phase":"input", "when":{...}, "action":"block"}]}
//
// For format 2, the policy object is serialized to YAML and stored as content.
func (s *Server) handlePolicyCreate(w http.ResponseWriter, r *http.Request) {
	var req CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if len(req.Policies) == 0 {
		http.Error(w, `{"error":"policies array is required"}`, http.StatusBadRequest)
		return
	}

	// Convert each raw policy JSON into a PolicyRecord with YAML content.
	records := make([]PolicyRecord, 0, len(req.Policies))
	for _, raw := range req.Policies {
		// First, check if content is already provided (UI YAML editor path).
		var input policyInput
		if err := json.Unmarshal(raw, &input); err != nil {
			http.Error(w, `{"error":"invalid policy object"}`, http.StatusBadRequest)
			return
		}

		if input.Content != "" {
			// UI path: content is already YAML.
			// Extract scope from YAML content if present
			scope := input.Scope
			if scope == "" {
				scope = extractScopeFromYAML(input.Content)
			}
			records = append(records, PolicyRecord{
				Name:    input.Name,
				Content: input.Content,
				Scope:   scope,
			})
			continue
		}

		// API path: the raw JSON is a policy object (name/phase/when/action/...).
		// Unmarshal into a generic map, then marshal to YAML for storage.
		var policyObj map[string]any
		if err := json.Unmarshal(raw, &policyObj); err != nil {
			http.Error(w, `{"error":"invalid policy object"}`, http.StatusBadRequest)
			return
		}

		name, _ := policyObj["name"].(string)
		if name == "" {
			http.Error(w, `{"error":"policy name is required"}`, http.StatusBadRequest)
			return
		}

		// Extract scope from the policy object
		scope, _ := policyObj["scope"].(string)
		if scope == "" {
			scope = "global"
		}

		yamlBytes, err := yaml.Marshal(policyObj)
		if err != nil {
			s.logger.Error().Err(err).Str("name", name).Msg("failed to marshal policy to YAML")
			http.Error(w, `{"error":"failed to serialize policy"}`, http.StatusInternalServerError)
			return
		}

		records = append(records, PolicyRecord{
			Name:    name,
			Content: string(yamlBytes),
			Scope:   scope,
		})
	}

	version, err := s.store.CreatePolicyVersion(req.Comment, req.CreatedBy, records)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to create policy version")
		http.Error(w, `{"error":"failed to create policy version"}`, http.StatusInternalServerError)
		return
	}

	s.logger.Info().Int("version", version).Int("policies", len(records)).Msg("policy version created")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"version":  version,
		"policies": len(records),
	})
}

// handleAgentPolicyGet handles GET /api/v1/agents/policies for agent policy pull.
// It is department-aware: the agent's department is resolved from its labels,
// and only global + department-specific policies are returned.
func (s *Server) handleAgentPolicyGet(w http.ResponseWriter, r *http.Request) {
	agentID := r.Header.Get("X-Agent-ID")

	// Resolve the agent's department from labels
	var deptID string
	if agentID != "" {
		agent, err := s.store.GetAgent(agentID)
		if err == nil && agent.Labels != "" {
			var labels map[string]string
			if err := json.Unmarshal([]byte(agent.Labels), &labels); err == nil {
				deptID = labels["department"]
			}
		}
	}

	// Check if agent is requesting with a version
	versionStr := r.URL.Query().Get("version")
	if versionStr != "" {
		requestedVersion, err := strconv.Atoi(versionStr)
		if err != nil {
			http.Error(w, `{"error":"invalid version parameter"}`, http.StatusBadRequest)
			return
		}

		latestVersion, err := s.store.GetLatestPolicyVersion()
		if err != nil {
			http.Error(w, `{"error":"failed to get policy version"}`, http.StatusInternalServerError)
			return
		}

		// Agent already has latest version
		if requestedVersion >= latestVersion {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		// Return policies filtered by agent's department
		var policies []PolicyRecord
		if deptID != "" {
			policies, err = s.store.GetPoliciesByVersionAndScope(latestVersion, deptID)
		} else {
			policies, err = s.store.GetPoliciesByVersion(latestVersion)
		}
		if err != nil {
			http.Error(w, `{"error":"failed to get policies"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"version":  latestVersion,
			"policies": policies,
		})
		return
	}

	// Return latest policies filtered by agent's department
	var latestVersion int
	var policies []PolicyRecord
	var err error
	if deptID != "" {
		latestVersion, policies, err = s.store.GetPoliciesForDepartment(deptID)
	} else {
		latestVersion, policies, err = s.store.GetLatestPolicies()
	}
	if err != nil {
		http.Error(w, `{"error":"failed to get policies"}`, http.StatusInternalServerError)
		return
	}

	if policies == nil {
		policies = []PolicyRecord{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"version":  latestVersion,
		"policies": policies,
	})
}
