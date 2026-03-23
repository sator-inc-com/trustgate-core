//go:build controlplane

package audit

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store handles audit log persistence using SQLite.
// Only available in Control Plane builds (build tag: controlplane).
type Store struct {
	db *sql.DB
}

// NewStore opens or creates an SQLite database for audit logs.
func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open audit db: %w", err)
	}

	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &Store{db: db}, nil
}

func createSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_logs (
			audit_id       TEXT PRIMARY KEY,
			timestamp      DATETIME NOT NULL,
			user_id        TEXT,
			role           TEXT,
			department     TEXT,
			clearance      TEXT,
			auth_method    TEXT,
			session_id     TEXT,
			app_id         TEXT,
			model          TEXT,
			input_hash     TEXT,
			input_tokens   INTEGER,
			output_hash    TEXT,
			output_tokens  INTEGER,
			finish_reason  TEXT,
			action         TEXT NOT NULL,
			policy_name    TEXT,
			reason         TEXT,
			detections     TEXT,
			risk_score     REAL,
			duration_ms    INTEGER,
			request_ip     TEXT,
			error          TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_logs(timestamp);
		CREATE INDEX IF NOT EXISTS idx_audit_user ON audit_logs(user_id);
		CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_logs(action);
		CREATE INDEX IF NOT EXISTS idx_audit_session ON audit_logs(session_id);
	`)
	return err
}

// Write inserts an audit log record.
func (s *Store) Write(r Record) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_logs (
			audit_id, timestamp, user_id, role, department, clearance, auth_method,
			session_id, app_id, model, input_hash, input_tokens,
			output_hash, output_tokens, finish_reason,
			action, policy_name, reason, detections, risk_score,
			duration_ms, request_ip, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.AuditID, r.Timestamp, r.UserID, r.Role, r.Department, r.Clearance, r.AuthMethod,
		r.SessionID, r.AppID, r.Model, r.InputHash, r.InputTokens,
		r.OutputHash, r.OutputTokens, r.FinishReason,
		r.Action, r.PolicyName, r.Reason, r.Detections, r.RiskScore,
		r.DurationMs, r.RequestIP, r.Error,
	)
	return err
}

// Query searches audit logs with filters.
func (s *Store) Query(opts QueryOpts) ([]Record, error) {
	where := []string{"1=1"}
	args := []any{}

	if opts.Action != "" {
		where = append(where, "action = ?")
		args = append(args, opts.Action)
	}
	if opts.UserID != "" {
		where = append(where, "user_id = ?")
		args = append(args, opts.UserID)
	}
	if opts.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, opts.SessionID)
	}
	if !opts.Since.IsZero() {
		where = append(where, "timestamp >= ?")
		args = append(args, opts.Since)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	query := fmt.Sprintf(
		"SELECT audit_id, timestamp, user_id, role, department, action, policy_name, reason, detections, risk_score, duration_ms, app_id FROM audit_logs WHERE %s ORDER BY timestamp DESC LIMIT ?",
		strings.Join(where, " AND "),
	)
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var r Record
		var ts string
		if err := rows.Scan(&r.AuditID, &ts, &r.UserID, &r.Role, &r.Department, &r.Action, &r.PolicyName, &r.Reason, &r.Detections, &r.RiskScore, &r.DurationMs, &r.AppID); err != nil {
			return nil, err
		}
		r.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		records = append(records, r)
	}
	return records, rows.Err()
}

// Count returns the total number of audit logs.
func (s *Store) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM audit_logs").Scan(&count)
	return count, err
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// DeleteOlderThan removes records older than the given duration.
func (s *Store) DeleteOlderThan(d time.Duration) (int64, error) {
	cutoff := time.Now().Add(-d)
	result, err := s.db.Exec("DELETE FROM audit_logs WHERE timestamp < ?", cutoff.Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Ensure Store implements Logger
var _ Logger = (*Store)(nil)
