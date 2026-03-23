package controlplane

import (
	"database/sql"
	"fmt"
	"time"
)

// Agent represents a registered TrustGate Agent.
type Agent struct {
	AgentID       string    `json:"agent_id"`
	Hostname      string    `json:"hostname"`
	OS            string    `json:"os"`
	Version       string    `json:"version"`
	Labels        string    `json:"labels"`
	Status        string    `json:"status"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	Detectors     int       `json:"detectors"`
	PolicyVersion int       `json:"policy_version"`
	RegisteredAt  time.Time `json:"registered_at"`
	TokenHash     string    `json:"-"`
}

// RegisterAgent inserts a new agent record.
func (s *Store) RegisterAgent(a Agent) error {
	_, err := s.db.Exec(`
		INSERT INTO agents (agent_id, hostname, os, version, labels, status, last_heartbeat, detectors, policy_version, registered_at, token_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.AgentID, a.Hostname, a.OS, a.Version, a.Labels, a.Status,
		a.LastHeartbeat, a.Detectors, a.PolicyVersion, a.RegisteredAt, a.TokenHash,
	)
	if err != nil {
		return fmt.Errorf("register agent: %w", err)
	}
	return nil
}

// GetAgent retrieves an agent by ID.
func (s *Store) GetAgent(agentID string) (*Agent, error) {
	var a Agent
	var lastHB, regAt string
	err := s.db.QueryRow(`
		SELECT agent_id, hostname, os, version, labels, status, last_heartbeat, detectors, policy_version, registered_at, token_hash
		FROM agents WHERE agent_id = ?`, agentID,
	).Scan(&a.AgentID, &a.Hostname, &a.OS, &a.Version, &a.Labels, &a.Status,
		&lastHB, &a.Detectors, &a.PolicyVersion, &regAt, &a.TokenHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("agent not found: %s", agentID)
		}
		return nil, fmt.Errorf("get agent: %w", err)
	}
	a.LastHeartbeat, _ = time.Parse(time.RFC3339, lastHB)
	a.RegisteredAt, _ = time.Parse(time.RFC3339, regAt)
	a.Status = agentStatus(a.LastHeartbeat)
	return &a, nil
}

// ListAgents returns all registered agents.
func (s *Store) ListAgents() ([]Agent, error) {
	rows, err := s.db.Query(`
		SELECT agent_id, hostname, os, version, labels, status, last_heartbeat, detectors, policy_version, registered_at
		FROM agents ORDER BY registered_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var a Agent
		var lastHB, regAt string
		if err := rows.Scan(&a.AgentID, &a.Hostname, &a.OS, &a.Version, &a.Labels, &a.Status,
			&lastHB, &a.Detectors, &a.PolicyVersion, &regAt); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		a.LastHeartbeat, _ = time.Parse(time.RFC3339, lastHB)
		a.RegisteredAt, _ = time.Parse(time.RFC3339, regAt)
		a.Status = agentStatus(a.LastHeartbeat)
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// GetAgentByHostname retrieves an agent by hostname (for auto-registration dedup).
func (s *Store) GetAgentByHostname(hostname string) (*Agent, error) {
	var a Agent
	var lastHB, regAt string
	err := s.db.QueryRow(`
		SELECT agent_id, hostname, os, version, labels, status, last_heartbeat, detectors, policy_version, registered_at, token_hash
		FROM agents WHERE hostname = ?`, hostname,
	).Scan(&a.AgentID, &a.Hostname, &a.OS, &a.Version, &a.Labels, &a.Status,
		&lastHB, &a.Detectors, &a.PolicyVersion, &regAt, &a.TokenHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // not found, not an error
		}
		return nil, fmt.Errorf("get agent by hostname: %w", err)
	}
	a.LastHeartbeat, _ = time.Parse(time.RFC3339, lastHB)
	a.RegisteredAt, _ = time.Parse(time.RFC3339, regAt)
	a.Status = agentStatus(a.LastHeartbeat)
	return &a, nil
}

// UpdateAgentToken updates an agent's token hash (for re-registration).
func (s *Store) UpdateAgentToken(agentID, tokenHash, os, version, labels string) error {
	_, err := s.db.Exec(`
		UPDATE agents SET token_hash = ?, os = ?, version = ?, labels = ?, last_heartbeat = ?
		WHERE agent_id = ?`,
		tokenHash, os, version, labels, time.Now().UTC().Format(time.RFC3339), agentID,
	)
	if err != nil {
		return fmt.Errorf("update agent token: %w", err)
	}
	return nil
}

// UpdateHeartbeat updates an agent's last heartbeat and optional fields.
func (s *Store) UpdateHeartbeat(agentID string, policyVersion int) error {
	_, err := s.db.Exec(`
		UPDATE agents SET last_heartbeat = ?, policy_version = ?
		WHERE agent_id = ?`,
		time.Now().UTC().Format(time.RFC3339), policyVersion, agentID,
	)
	if err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}
	return nil
}

// CountAgents returns the count of agents by status, computed dynamically
// from last_heartbeat rather than the static status column.
func (s *Store) CountAgents() (total int, active int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM agents`).Scan(&total)
	if err != nil {
		return
	}

	// Count agents with heartbeat within the last 2 minutes as active
	cutoff := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339)
	err = s.db.QueryRow(`SELECT COUNT(*) FROM agents WHERE last_heartbeat >= ?`, cutoff).Scan(&active)
	return
}

// DeleteAgent removes an agent from the registry.
func (s *Store) DeleteAgent(agentID string) error {
	result, err := s.db.Exec(`DELETE FROM agents WHERE agent_id = ?`, agentID)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("agent not found: %s", agentID)
	}
	return nil
}

// agentStatus computes the dynamic status of an agent based on the age of
// its last heartbeat:
//   - active:  heartbeat within the last 2 minutes
//   - stale:   heartbeat between 2 and 10 minutes ago
//   - offline: heartbeat over 10 minutes ago, or never received
func agentStatus(lastHeartbeat time.Time) string {
	if lastHeartbeat.IsZero() {
		return "offline"
	}
	since := time.Since(lastHeartbeat)
	switch {
	case since > 10*time.Minute:
		return "offline"
	case since > 2*time.Minute:
		return "stale"
	default:
		return "active"
	}
}
