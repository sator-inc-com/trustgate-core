package config

import "time"

type Config struct {
	Version string `yaml:"version"`
	Mode    string `yaml:"mode"` // standalone | managed

	Listen   ListenConfig   `yaml:"listen"`
	Identity IdentityConfig `yaml:"identity"`
	Context  ContextConfig  `yaml:"context"`

	Detectors          DetectorConfig          `yaml:"detectors"`
	ResponseInspection ResponseInspectionConfig `yaml:"response_inspection"`

	Policy  PolicyConfig  `yaml:"policy"`
	Audit   AuditConfig   `yaml:"audit"`
	Logging LoggingConfig `yaml:"logging"`

	Notifications NotificationConfig `yaml:"notifications"`

	Backend           BackendConfig           `yaml:"backend"`
	ContentInspection ContentInspectionConfig `yaml:"content_inspection"`

	Sync SyncConfig `yaml:"sync"`
}

// ContentInspectionConfig configures async file content inspection.
// Default: disabled. Enable to inspect PDF, Office, and image files
// uploaded via browser extension. Files are processed asynchronously
// (non-blocking) and results are returned via polling.
type ContentInspectionConfig struct {
	Enabled     bool     `yaml:"enabled"`      // default: false (opt-in)
	MaxFileSize int64    `yaml:"max_file_size"` // bytes, default: 10MB
	MaxQueue    int      `yaml:"max_queue"`     // max concurrent inspections, default: 4
	// Confidential keywords to check in image filenames
	ImageKeywords []string `yaml:"image_keywords"` // e.g., ["機密", "社外秘", "confidential"]
	// Max image file size for metadata check (no OCR)
	MaxImageSize  int64    `yaml:"max_image_size"` // bytes, default: 50MB
}

// BackendConfig configures the LLM backend for /v1/chat/completions proxy.
type BackendConfig struct {
	Provider string        `yaml:"provider"` // "bedrock" | "mock"
	Bedrock  BedrockConfig `yaml:"bedrock"`
	Mock     MockConfig    `yaml:"mock"`
}

type BedrockConfig struct {
	Region string `yaml:"region"` // AWS region (e.g., "ap-northeast-1")
	Model  string `yaml:"model"`  // Bedrock model ID (e.g., "anthropic.claude-3-7-sonnet-20250219-v1:0")
}

type MockConfig struct {
	DelayMs int `yaml:"delay_ms"` // simulated latency in ms
}

type ListenConfig struct {
	Host               string        `yaml:"host"`
	Port               int           `yaml:"port"`
	ReadTimeout        time.Duration `yaml:"read_timeout"`
	WriteTimeout       time.Duration `yaml:"write_timeout"`
	IncludeTrustgate   bool          `yaml:"include_trustgate_body"`
	AllowDebug         bool          `yaml:"allow_debug"`
}

type IdentityConfig struct {
	Mode          string            `yaml:"mode"` // header | api_key | jwt
	Headers       map[string]string `yaml:"headers"`
	OnMissing     string            `yaml:"on_missing"` // allow | block | anonymous
	AnonymousRole string            `yaml:"anonymous_role"`
}

type ContextConfig struct {
	Session     SessionConfig     `yaml:"session"`
	RiskScoring RiskScoringConfig `yaml:"risk_scoring"`
}

type SessionConfig struct {
	TTL   time.Duration `yaml:"ttl"`
	Store string        `yaml:"store"` // memory
}

type RiskScoringConfig struct {
	InjectionDetected float64 `yaml:"injection_detected"`
	PIIDetected       float64 `yaml:"pii_detected"`
	ConfidentialDetected float64 `yaml:"confidential_detected"`
	BlockOccurred     float64 `yaml:"block_occurred"`
	DecayPerMinute    float64 `yaml:"decay_per_minute"`
	ThresholdWarn     float64 `yaml:"threshold_warn"`
	ThresholdBlock    float64 `yaml:"threshold_block"`
}

type DetectorConfig struct {
	PII            PIIConfig            `yaml:"pii"`
	Injection      InjectionConfig      `yaml:"injection"`
	Confidential   ConfidentialConfig   `yaml:"confidential"`
	ToolInspection ToolInspectionConfig `yaml:"tool_inspection"`
	LLM            LLMDetectorConfig    `yaml:"llm"`
}

// LLMDetectorConfig configures the local LLM-based detector (Stage 2).
// Uses Prompt Guard 2 (86M) for prompt injection/jailbreak classification.
// Small enough to run on Desktop Agent (200MB RAM, no GPU required).
type LLMDetectorConfig struct {
	Enabled bool   `yaml:"enabled"`
	// Model selection
	Model   string `yaml:"model"`    // "prompt-guard-2-86m" (default) | "prompt-guard-2-22m"
	// Path to model files (auto-downloaded if not present)
	ModelDir string `yaml:"model_dir"` // default: ~/.trustgate/models/
	// Gray-zone escalation threshold: findings with confidence below this
	// value are escalated from Stage 1 (regex) to Stage 2 (LLM).
	EscalationThreshold float64 `yaml:"escalation_threshold"` // default: 0.8
	// Also escalate when these conditions are met (even without regex match)
	EscalateOnMixedLanguage  bool `yaml:"escalate_on_mixed_language"`  // default: true
	EscalateOnEncodedContent bool `yaml:"escalate_on_encoded_content"` // default: true
	EscalateOnSeparators     bool `yaml:"escalate_on_separators"`      // default: true
	// Inference settings
	MaxConcurrent int `yaml:"max_concurrent"` // default: 4
	TimeoutMs     int `yaml:"timeout_ms"`     // default: 100 (ms)
}

type PIIConfig struct {
	Enabled  bool              `yaml:"enabled"`
	Patterns map[string]bool   `yaml:"patterns"`
	Custom   []CustomPattern   `yaml:"custom_patterns"`
}

type InjectionConfig struct {
	Enabled  bool            `yaml:"enabled"`
	Language []string        `yaml:"language"`
	Custom   []CustomPattern `yaml:"custom_patterns"`
}

type ConfidentialConfig struct {
	Enabled  bool                `yaml:"enabled"`
	Keywords map[string][]string `yaml:"keywords"`
	Custom   []CustomKeyword     `yaml:"custom_keywords"`
}

type ToolInspectionConfig struct {
	Enabled             bool `yaml:"enabled"`
	InspectArguments    bool `yaml:"inspect_arguments"`
	InspectToolResults  bool `yaml:"inspect_tool_results"`
}

type CustomPattern struct {
	Name     string `yaml:"name"`
	Pattern  string `yaml:"pattern"`
	Severity string `yaml:"severity"`
}

type CustomKeyword struct {
	Term     string `yaml:"term"`
	Severity string `yaml:"severity"`
}

type ResponseInspectionConfig struct {
	Mode             string `yaml:"mode"` // none | async | windowed | buffered
	WindowSize       int    `yaml:"window_size"`
	Overlap          int    `yaml:"overlap"`
	MaxBuffer        int    `yaml:"max_buffer"`
	OnBufferOverflow string `yaml:"on_buffer_overflow"` // allow | block
}

type PolicyConfig struct {
	Source string `yaml:"source"` // local | remote
	File   string `yaml:"file"`
}

type AuditConfig struct {
	Mode          string `yaml:"mode"` // local
	Path          string `yaml:"path"`
	RetentionDays int    `yaml:"retention_days"`
	MaxSizeMB     int    `yaml:"max_size_mb"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug | info | warn | error
	Format string `yaml:"format"` // json | text
}

type NotificationConfig struct {
	Enabled  bool              `yaml:"enabled"`
	Channels []ChannelConfig   `yaml:"channels"`
}

type ChannelConfig struct {
	Type     string            `yaml:"type"` // webhook | file
	Name     string            `yaml:"name"`
	URL      string            `yaml:"url"`
	Path     string            `yaml:"path"`
	On       NotifyCondition   `yaml:"on"`
	Throttle string            `yaml:"throttle"`
}

type NotifyCondition struct {
	Actions     []string `yaml:"actions"`
	MinSeverity string   `yaml:"min_severity"`
}

// SyncConfig configures the Agent's connection to the Control Plane (managed mode).
type SyncConfig struct {
	ServerURL     string            `yaml:"server_url"`
	ApiKey        string            `yaml:"api_key"`        // org key (tg_org_*) or department key (tg_dept_*) for auto-registration
	AgentID       string            `yaml:"agent_id"`       // auto-filled after registration
	AgentToken    string            `yaml:"agent_token"`    // auto-filled after registration
	Labels        map[string]string `yaml:"labels"`         // department, environment, etc.
	HeartbeatSec  int               `yaml:"heartbeat_sec"`
	PolicyPullSec int               `yaml:"policy_pull_sec"`
	StatsPushSec  int               `yaml:"stats_push_sec"`
}

// ServerConfig is the configuration for aigw-server (Control Plane).
type ServerConfig struct {
	Version  string             `yaml:"version"`
	Listen   ServerListenConfig `yaml:"listen"`
	Database DatabaseConfig     `yaml:"database"`
	Auth     AuthConfig         `yaml:"auth"`
	Logging  LoggingConfig      `yaml:"logging"`
}

// ServerListenConfig configures the Control Plane HTTP server.
type ServerListenConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// DatabaseConfig configures the Control Plane SQLite database.
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// AuthConfig configures authentication for the Control Plane.
type AuthConfig struct {
	ApiKey      string `yaml:"api_key"`      // org API key for agent auto-registration
	MFARequired *bool  `yaml:"mfa_required"` // default: true. Set false to disable MFA requirement.
}

// IsMFARequired returns whether MFA is required. Defaults to true if not set.
func (a AuthConfig) IsMFARequired() bool {
	if a.MFARequired == nil {
		return true
	}
	return *a.MFARequired
}
