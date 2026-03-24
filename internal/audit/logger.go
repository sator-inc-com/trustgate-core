package audit

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// Record represents a single audit log entry.
type Record struct {
	AuditID      string    `json:"audit_id"`
	Timestamp    time.Time `json:"timestamp"`
	UserID       string    `json:"user_id"`
	Role         string    `json:"role"`
	Department   string    `json:"department"`
	Clearance    string    `json:"clearance"`
	AuthMethod   string    `json:"auth_method"`
	SessionID    string    `json:"session_id"`
	AppID        string    `json:"app_id"`
	Model        string    `json:"model"`
	InputHash    string    `json:"input_hash"`
	InputTokens  int       `json:"input_tokens"`
	OutputHash   string    `json:"output_hash"`
	OutputTokens int       `json:"output_tokens"`
	FinishReason string    `json:"finish_reason"`
	Action       string    `json:"action"`
	PolicyName   string    `json:"policy_name"`
	Reason       string    `json:"reason"`
	Detections   string    `json:"detections"`
	RiskScore    float64   `json:"risk_score"`
	DurationMs   int64     `json:"duration_ms"`
	RequestIP    string    `json:"request_ip"`
	Error        string    `json:"error,omitempty"`
}

// QueryOpts defines filters for querying audit logs.
type QueryOpts struct {
	Action    string
	UserID    string
	SessionID string
	Since     time.Time
	Limit     int
}

// ErrNotFound is returned when an audit record is not found.
var ErrNotFound = fmt.Errorf("audit record not found")

// Logger is the interface for audit log backends.
// Standalone mode uses MemoryLogger (ring buffer, no disk).
// Managed mode uses WALWriter (JSONLines with hash chain).
// Control Plane uses Store (SQLite with retention).
type Logger interface {
	Write(r Record) error
	Query(opts QueryOpts) ([]Record, error)
	GetByID(auditID string) (*Record, error)
	Count() (int, error)
	Close() error
}

// HashText returns a SHA256 hash of the input text.
func HashText(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("sha256:%x", h[:8])
}
