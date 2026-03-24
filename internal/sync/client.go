package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/trustgate/trustgate/internal/audit"
	"github.com/trustgate/trustgate/internal/config"
)

// PolicyUpdateFunc is called when new policies are pulled from the Control Plane.
// The function receives the raw policy content strings (YAML) and the new version number.
type PolicyUpdateFunc func(version int, policyContents []string)

// Client manages sync between Agent and Control Plane.
type Client struct {
	cfg    config.SyncConfig
	logger zerolog.Logger
	http   *http.Client
	stopCh chan struct{}

	mu             sync.Mutex
	byAction       map[string]map[string]int // action -> detector -> count
	byUser         []userActionCount
	byPolicy       []policyTriggerCount

	policyMu       sync.RWMutex
	policyVersion  int
	onPolicyUpdate PolicyUpdateFunc

	// WAL-based audit flush
	auditWAL *audit.WALWriter
}

type userActionCount struct {
	UserID string `json:"user_id"`
	Action string `json:"action"`
	Count  int    `json:"count"`
}

type policyTriggerCount struct {
	PolicyName         string `json:"policy_name"`
	TriggerCount       int    `json:"trigger_count"`
	FalsePositiveCount int    `json:"false_positive_count"`
}

type statsPushRequest struct {
	AgentID  string                    `json:"agent_id"`
	Period   string                    `json:"period"`
	ByAction map[string]map[string]int `json:"by_action"`
	ByUser   []userActionCount         `json:"by_user"`
	ByPolicy []policyTriggerCount      `json:"by_policy"`
}

type heartbeatRequest struct {
	PolicyVersion int               `json:"policy_version"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// NewClient creates a new sync client for managed mode.
func NewClient(cfg config.SyncConfig, logger zerolog.Logger) *Client {
	return &Client{
		cfg:      cfg,
		logger:   logger.With().Str("component", "sync").Logger(),
		http:     &http.Client{Timeout: 10 * time.Second},
		stopCh:   make(chan struct{}),
		byAction: make(map[string]map[string]int),
	}
}

// SetPolicyUpdateCallback registers a function to be called when new policies
// are pulled from the Control Plane. Must be called before Start().
func (c *Client) SetPolicyUpdateCallback(fn PolicyUpdateFunc) {
	c.onPolicyUpdate = fn
}

// SetAuditWAL sets the WAL writer for audit log flush to the CP.
// Must be called before Start(). If not set, audit flush is disabled.
func (c *Client) SetAuditWAL(wal *audit.WALWriter) {
	c.auditWAL = wal
}

// PolicyVersion returns the current policy version known to the sync client.
func (c *Client) PolicyVersion() int {
	c.policyMu.RLock()
	defer c.policyMu.RUnlock()
	return c.policyVersion
}

// Start begins background goroutines for heartbeat, policy pull, and stats push.
// If AgentToken is empty but ApiKey is set, it performs auto-registration first.
// If auto-registration fails, it logs the error and falls back to standalone mode.
func (c *Client) Start() error {
	if c.cfg.HeartbeatSec <= 0 {
		c.cfg.HeartbeatSec = 30
	}
	if c.cfg.StatsPushSec <= 0 {
		c.cfg.StatsPushSec = 60
	}
	if c.cfg.PolicyPullSec <= 0 {
		c.cfg.PolicyPullSec = 60
	}

	// Auto-registration: if no agent_token but api_key is set
	if c.cfg.AgentToken == "" && c.cfg.ApiKey != "" {
		// Try loading saved credentials first
		creds, err := LoadCredentials()
		if err != nil {
			c.logger.Warn().Err(err).Msg("failed to load saved credentials, will re-register")
		}
		if creds != nil && creds.AgentToken != "" && creds.AgentID != "" {
			c.cfg.AgentID = creds.AgentID
			c.cfg.AgentToken = creds.AgentToken
			c.logger.Info().Str("agent_id", creds.AgentID).Msg("loaded saved credentials")
		} else {
			// Perform auto-registration
			if err := c.autoRegister(); err != nil {
				c.logger.Error().Err(err).Msg("auto-registration failed, falling back to standalone mode")
				return fmt.Errorf("auto-registration failed: %w", err)
			}
		}
	}

	// Pull policies immediately on startup
	c.pullPolicies()

	go c.heartbeatLoop()
	go c.statsPushLoop()
	go c.policyPullLoop()
	if c.auditWAL != nil {
		go c.auditFlushLoop()
	}

	c.logger.Info().
		Str("server_url", c.cfg.ServerURL).
		Str("agent_id", c.cfg.AgentID).
		Int("heartbeat_sec", c.cfg.HeartbeatSec).
		Int("stats_push_sec", c.cfg.StatsPushSec).
		Int("policy_pull_sec", c.cfg.PolicyPullSec).
		Bool("audit_flush", c.auditWAL != nil).
		Msg("sync client started")

	return nil
}

// autoRegister performs agent registration using the org API key.
func (c *Client) autoRegister() error {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	reqBody := struct {
		Hostname string            `json:"hostname"`
		OS       string            `json:"os"`
		Version  string            `json:"version"`
		Labels   map[string]string `json:"labels,omitempty"`
	}{
		Hostname: hostname,
		OS:       runtime.GOOS + "/" + runtime.GOARCH,
		Version:  "1.0.0", // TODO: inject build version
		Labels:   c.cfg.Labels,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal registration request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/agents/register", c.cfg.ServerURL)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create registration request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", c.cfg.ApiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("registration returned status %d", resp.StatusCode)
	}

	var regResp struct {
		AgentID    string `json:"agent_id"`
		AgentToken string `json:"agent_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return fmt.Errorf("decode registration response: %w", err)
	}

	// Update sync config with received credentials
	c.cfg.AgentID = regResp.AgentID
	c.cfg.AgentToken = regResp.AgentToken

	// Save credentials to disk
	creds := Credentials{
		AgentID:    regResp.AgentID,
		AgentToken: regResp.AgentToken,
	}
	if err := SaveCredentials(creds); err != nil {
		c.logger.Warn().Err(err).Msg("failed to save credentials (will re-register on next start)")
	} else {
		path, _ := CredentialsPath()
		c.logger.Info().Str("path", path).Msg("credentials saved")
	}

	c.logger.Info().
		Str("agent_id", regResp.AgentID).
		Str("hostname", hostname).
		Msg("agent auto-registered successfully")

	return nil
}

// Stop signals all background goroutines to stop.
func (c *Client) Stop() {
	close(c.stopCh)
}

// RecordEvent accumulates stats for a single inspection event.
// Called from the Agent pipeline after each request.
func (c *Client) RecordEvent(action, detector, userID, policyName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Accumulate action x detector
	if c.byAction[action] == nil {
		c.byAction[action] = make(map[string]int)
	}
	c.byAction[action][detector]++

	// Accumulate user x action
	if userID != "" {
		found := false
		for i, u := range c.byUser {
			if u.UserID == userID && u.Action == action {
				c.byUser[i].Count++
				found = true
				break
			}
		}
		if !found {
			c.byUser = append(c.byUser, userActionCount{
				UserID: userID, Action: action, Count: 1,
			})
		}
	}

	// Accumulate policy triggers
	if policyName != "" {
		found := false
		for i, p := range c.byPolicy {
			if p.PolicyName == policyName {
				c.byPolicy[i].TriggerCount++
				found = true
				break
			}
		}
		if !found {
			c.byPolicy = append(c.byPolicy, policyTriggerCount{
				PolicyName: policyName, TriggerCount: 1,
			})
		}
	}
}

// RecordFalsePositive increments the false positive count for a policy.
// The count is pushed to the Control Plane on the next stats push cycle.
func (c *Client) RecordFalsePositive(policyName string) {
	if policyName == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, p := range c.byPolicy {
		if p.PolicyName == policyName {
			c.byPolicy[i].FalsePositiveCount++
			return
		}
	}
	c.byPolicy = append(c.byPolicy, policyTriggerCount{
		PolicyName:         policyName,
		FalsePositiveCount: 1,
	})
}

func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(time.Duration(c.cfg.HeartbeatSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.sendHeartbeat()
		}
	}
}

func (c *Client) statsPushLoop() {
	ticker := time.NewTicker(time.Duration(c.cfg.StatsPushSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.pushStats()
		}
	}
}

func (c *Client) policyPullLoop() {
	ticker := time.NewTicker(time.Duration(c.cfg.PolicyPullSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.pullPolicies()
		}
	}
}

// policyVersionResponse is the response from GET /api/v1/agents/policies/version.
type policyVersionResponse struct {
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// policyPullResponse is the response from GET /api/v1/agents/policies.
type policyPullResponse struct {
	Version  int              `json:"version"`
	Policies []policyRecord   `json:"policies"`
}

type policyRecord struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (c *Client) pullPolicies() {
	// Step 1: lightweight version check
	url := fmt.Sprintf("%s/api/v1/agents/policies/version", c.cfg.ServerURL)
	httpReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to create policy version request")
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.AgentToken)
	httpReq.Header.Set("X-Agent-ID", c.cfg.AgentID)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		c.logger.Warn().Err(err).Msg("policy version check failed (CP unreachable)")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Warn().Int("status", resp.StatusCode).Msg("policy version check returned non-200")
		return
	}

	var versionResp policyVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&versionResp); err != nil {
		c.logger.Warn().Err(err).Msg("failed to decode policy version response")
		return
	}

	c.policyMu.RLock()
	currentVersion := c.policyVersion
	c.policyMu.RUnlock()

	// No update needed
	if versionResp.Version <= currentVersion {
		c.logger.Debug().Int("version", currentVersion).Msg("policy version unchanged")
		return
	}

	// Step 2: pull full policies
	c.logger.Info().
		Int("current_version", currentVersion).
		Int("remote_version", versionResp.Version).
		Msg("new policy version detected, pulling full policies")

	fullURL := fmt.Sprintf("%s/api/v1/agents/policies?version=%d", c.cfg.ServerURL, currentVersion)
	httpReq2, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to create policy pull request")
		return
	}
	httpReq2.Header.Set("Authorization", "Bearer "+c.cfg.AgentToken)
	httpReq2.Header.Set("X-Agent-ID", c.cfg.AgentID)

	resp2, err := c.http.Do(httpReq2)
	if err != nil {
		c.logger.Warn().Err(err).Msg("policy pull failed (CP unreachable)")
		return
	}
	defer resp2.Body.Close()

	if resp2.StatusCode == http.StatusNotModified {
		// Already up to date (race condition)
		return
	}

	if resp2.StatusCode != http.StatusOK {
		c.logger.Warn().Int("status", resp2.StatusCode).Msg("policy pull returned non-200")
		return
	}

	var pullResp policyPullResponse
	if err := json.NewDecoder(resp2.Body).Decode(&pullResp); err != nil {
		c.logger.Warn().Err(err).Msg("failed to decode policy pull response")
		return
	}

	// Extract policy contents
	contents := make([]string, len(pullResp.Policies))
	for i, p := range pullResp.Policies {
		contents[i] = p.Content
	}

	// Update local version
	c.policyMu.Lock()
	oldVersion := c.policyVersion
	c.policyVersion = pullResp.Version
	c.policyMu.Unlock()

	// Notify callback
	if c.onPolicyUpdate != nil {
		c.onPolicyUpdate(pullResp.Version, contents)
	}

	c.logger.Info().
		Int("old_version", oldVersion).
		Int("new_version", pullResp.Version).
		Int("policies", len(pullResp.Policies)).
		Msgf("policy updated: v%d → v%d (%d policies)", oldVersion, pullResp.Version, len(pullResp.Policies))
}

func (c *Client) sendHeartbeat() {
	c.policyMu.RLock()
	pv := c.policyVersion
	c.policyMu.RUnlock()
	req := heartbeatRequest{PolicyVersion: pv, Labels: c.cfg.Labels}
	body, _ := json.Marshal(req)

	agentID := c.cfg.AgentID
	url := fmt.Sprintf("%s/api/v1/agents/%s/heartbeat", c.cfg.ServerURL, agentID)
	httpReq, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to create heartbeat request")
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.AgentToken)
	httpReq.Header.Set("X-Agent-ID", agentID)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		c.logger.Warn().Err(err).Msg("heartbeat failed")
		return
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		c.logger.Warn().Int("status", resp.StatusCode).Msg("heartbeat rejected (agent may have been deleted), attempting re-registration")
		if err := c.reRegister(); err != nil {
			c.logger.Error().Err(err).Msg("auto re-registration failed")
		}
		return
	}
	if resp.StatusCode != http.StatusOK {
		c.logger.Warn().Int("status", resp.StatusCode).Msg("heartbeat returned non-200")
	}
}

// reRegister clears saved credentials and attempts to re-register with the CP.
func (c *Client) reRegister() error {
	c.logger.Info().Msg("clearing credentials and re-registering")

	// Delete saved credentials file
	if err := DeleteCredentials(); err != nil {
		c.logger.Warn().Err(err).Msg("failed to delete credentials file")
	}

	// Clear in-memory credentials
	c.cfg.AgentID = ""
	c.cfg.AgentToken = ""

	// Re-register using api_key
	if c.cfg.ApiKey == "" {
		return fmt.Errorf("no api_key configured, cannot re-register")
	}

	if err := c.autoRegister(); err != nil {
		return fmt.Errorf("re-registration failed: %w", err)
	}

	c.logger.Info().Str("agent_id", c.cfg.AgentID).Msg("agent re-registered successfully")
	return nil
}

func (c *Client) pushStats() {
	c.mu.Lock()
	if len(c.byAction) == 0 && len(c.byUser) == 0 && len(c.byPolicy) == 0 {
		c.mu.Unlock()
		return
	}

	// Swap buffers
	byAction := c.byAction
	byUser := c.byUser
	byPolicy := c.byPolicy
	c.byAction = make(map[string]map[string]int)
	c.byUser = nil
	c.byPolicy = nil
	c.mu.Unlock()

	req := statsPushRequest{
		AgentID:  c.cfg.AgentID,
		Period:   time.Now().UTC().Format("2006-01-02"),
		ByAction: byAction,
		ByUser:   byUser,
		ByPolicy: byPolicy,
	}

	body, _ := json.Marshal(req)
	url := fmt.Sprintf("%s/api/v1/audit/stats", c.cfg.ServerURL)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to create stats push request")
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.AgentToken)
	httpReq.Header.Set("X-Agent-ID", c.cfg.AgentID)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		c.logger.Warn().Err(err).Msg("stats push failed")
		return
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Warn().Int("status", resp.StatusCode).Msg("stats push returned non-200")
	} else {
		c.logger.Debug().Msg("stats pushed successfully")
	}
}

// --- Audit WAL Flush ---

type auditFlushRequest struct {
	AgentID  string             `json:"agent_id"`
	LastSeq  uint64             `json:"last_seq"`
	LastHash string             `json:"last_hash"`
	Records  []audit.WALRecord  `json:"records"`
}

type auditFlushResponse struct {
	AcceptedSeq  uint64 `json:"accepted_seq"`
	AcceptedHash string `json:"accepted_hash"`
}

func (c *Client) auditFlushLoop() {
	ticker := time.NewTicker(time.Duration(c.cfg.StatsPushSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			// Final flush on shutdown
			c.flushAuditRecords()
			return
		case <-ticker.C:
			c.flushAuditRecords()
		}
	}
}

func (c *Client) flushAuditRecords() {
	if c.auditWAL == nil {
		return
	}

	records, err := c.auditWAL.UnflushedRecords()
	if err != nil {
		c.logger.Warn().Err(err).Msg("failed to read unflushed audit records")
		return
	}
	if len(records) == 0 {
		return
	}

	lastSeq, lastHash := c.auditWAL.LastSeqAndHash()

	// Send in batches of 500 to avoid huge payloads
	const batchSize = 500
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}
		batch := records[i:end]

		req := auditFlushRequest{
			AgentID:  c.cfg.AgentID,
			LastSeq:  lastSeq,
			LastHash: lastHash,
			Records:  batch,
		}

		body, _ := json.Marshal(req)
		url := fmt.Sprintf("%s/api/v1/audit/flush", c.cfg.ServerURL)
		httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			c.logger.Error().Err(err).Msg("failed to create audit flush request")
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.AgentToken)
		httpReq.Header.Set("X-Agent-ID", c.cfg.AgentID)

		resp, err := c.http.Do(httpReq)
		if err != nil {
			c.logger.Warn().Err(err).Int("records", len(batch)).Msg("audit flush failed (CP unreachable)")
			return // Stop batching, retry next cycle
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			c.logger.Warn().Int("status", resp.StatusCode).Msg("audit flush returned non-200")
			return
		}

		var flushResp auditFlushResponse
		if err := json.NewDecoder(resp.Body).Decode(&flushResp); err != nil {
			resp.Body.Close()
			c.logger.Warn().Err(err).Msg("failed to decode audit flush response")
			return
		}
		resp.Body.Close()

		// Update cursor: only mark as flushed what CP confirmed
		if err := c.auditWAL.MarkFlushed(flushResp.AcceptedSeq, flushResp.AcceptedHash); err != nil {
			c.logger.Error().Err(err).Msg("failed to update audit cursor")
			return
		}

		c.logger.Debug().
			Uint64("accepted_seq", flushResp.AcceptedSeq).
			Int("batch_size", len(batch)).
			Msg("audit records flushed to CP")
	}
}
