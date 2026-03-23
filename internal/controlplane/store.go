package controlplane

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// AdminAPIToken represents an admin API token stored in the database.
type AdminAPIToken struct {
	TokenHash string
	Username  string
	Name      string
	CreatedAt time.Time
	ExpiresAt *time.Time
	LastUsed  *time.Time
}

// MFAConfig represents the MFA configuration stored in the database.
type MFAConfig struct {
	Secret    string
	Enabled   bool
	CreatedAt time.Time
}

// Admin represents an admin user account.
type Admin struct {
	ID           string
	DisplayName  string
	PasswordHash string
	MFASecret    string
	MFAEnabled   bool
	CreatedAt    time.Time
	LastLogin    *time.Time
}

// Store is the Control Plane SQLite database.
type Store struct {
	db *sql.DB
}

// NewStore opens or creates the Control Plane SQLite database with WAL mode.
func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open controlplane db: %w", err)
	}

	if err := createCPSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create controlplane schema: %w", err)
	}

	return &Store{db: db}, nil
}

func createCPSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			agent_id       TEXT PRIMARY KEY,
			hostname       TEXT NOT NULL,
			os             TEXT,
			version        TEXT,
			labels         TEXT,
			status         TEXT DEFAULT 'active',
			last_heartbeat DATETIME,
			detectors      INTEGER DEFAULT 0,
			policy_version INTEGER DEFAULT 0,
			registered_at  DATETIME NOT NULL,
			token_hash     TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS policies (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			version    INTEGER NOT NULL,
			name       TEXT NOT NULL,
			content    TEXT NOT NULL,
			scope      TEXT NOT NULL DEFAULT 'global',
			created_at DATETIME NOT NULL,
			created_by TEXT,
			UNIQUE(version, name)
		);

		CREATE TABLE IF NOT EXISTS policy_versions (
			version    INTEGER PRIMARY KEY,
			created_at DATETIME NOT NULL,
			comment    TEXT
		);

		CREATE TABLE IF NOT EXISTS audit_daily (
			date     TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			action   TEXT NOT NULL,
			detector TEXT NOT NULL,
			count    INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (date, agent_id, action, detector)
		);

		CREATE TABLE IF NOT EXISTS audit_user_daily (
			date     TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			user_id  TEXT NOT NULL,
			action   TEXT NOT NULL,
			count    INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (date, agent_id, user_id, action)
		);

		CREATE TABLE IF NOT EXISTS audit_policy_daily (
			date                 TEXT NOT NULL,
			agent_id             TEXT NOT NULL,
			policy_name          TEXT NOT NULL,
			trigger_count        INTEGER NOT NULL DEFAULT 0,
			false_positive_count INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (date, agent_id, policy_name)
		);

		CREATE TABLE IF NOT EXISTS mfa_config (
			id         INTEGER PRIMARY KEY CHECK (id = 1),
			secret     TEXT NOT NULL,
			enabled    INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL
		);

		CREATE TABLE IF NOT EXISTS departments (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			description TEXT DEFAULT '',
			api_key     TEXT DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS admins (
			id            TEXT PRIMARY KEY,
			display_name  TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			mfa_secret    TEXT,
			mfa_enabled   BOOLEAN DEFAULT 0,
			created_at    DATETIME NOT NULL,
			last_login    DATETIME
		);

		CREATE TABLE IF NOT EXISTS admin_api_tokens (
			token_hash  TEXT PRIMARY KEY,
			username    TEXT NOT NULL,
			name        TEXT NOT NULL,
			created_at  DATETIME NOT NULL,
			expires_at  DATETIME,
			last_used   DATETIME
		);
	`)
	if err != nil {
		return err
	}

	// Migration: add api_key column to departments if missing (existing DBs)
	_, _ = db.Exec(`ALTER TABLE departments ADD COLUMN api_key TEXT DEFAULT ''`)

	// Migration: add scope column to policies if missing (existing DBs)
	_, _ = db.Exec(`ALTER TABLE policies ADD COLUMN scope TEXT NOT NULL DEFAULT 'global'`)

	return nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// GetMFAConfig returns the MFA configuration, or nil if not set up.
func (s *Store) GetMFAConfig() (*MFAConfig, error) {
	row := s.db.QueryRow("SELECT secret, enabled, created_at FROM mfa_config WHERE id = 1")
	var cfg MFAConfig
	var enabled int
	err := row.Scan(&cfg.Secret, &enabled, &cfg.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get mfa config: %w", err)
	}
	cfg.Enabled = enabled == 1
	return &cfg, nil
}

// SaveMFASecret stores a new MFA secret (not yet enabled).
func (s *Store) SaveMFASecret(secret string) error {
	_, err := s.db.Exec(
		`INSERT INTO mfa_config (id, secret, enabled, created_at) VALUES (1, ?, 0, ?)
		 ON CONFLICT(id) DO UPDATE SET secret = excluded.secret, enabled = 0, created_at = excluded.created_at`,
		secret, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("save mfa secret: %w", err)
	}
	return nil
}

// EnableMFA marks MFA as active.
func (s *Store) EnableMFA() error {
	_, err := s.db.Exec("UPDATE mfa_config SET enabled = 1 WHERE id = 1")
	if err != nil {
		return fmt.Errorf("enable mfa: %w", err)
	}
	return nil
}

// IsMFAEnabled returns whether MFA is set up and active.
func (s *Store) IsMFAEnabled() (bool, error) {
	cfg, err := s.GetMFAConfig()
	if err != nil {
		return false, err
	}
	if cfg == nil {
		return false, nil
	}
	return cfg.Enabled, nil
}

// --- Admin CRUD ---

// CreateAdmin inserts a new admin user.
func (s *Store) CreateAdmin(admin *Admin) error {
	_, err := s.db.Exec(
		`INSERT INTO admins (id, display_name, password_hash, mfa_secret, mfa_enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		admin.ID, admin.DisplayName, admin.PasswordHash, admin.MFASecret, admin.MFAEnabled, admin.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	return nil
}

// GetAdmin returns an admin by username (id), or nil if not found.
func (s *Store) GetAdmin(id string) (*Admin, error) {
	row := s.db.QueryRow(
		`SELECT id, display_name, password_hash, mfa_secret, mfa_enabled, created_at, last_login
		 FROM admins WHERE id = ?`, id)
	var a Admin
	var mfaSecret sql.NullString
	var mfaEnabled int
	var lastLogin sql.NullTime
	err := row.Scan(&a.ID, &a.DisplayName, &a.PasswordHash, &mfaSecret, &mfaEnabled, &a.CreatedAt, &lastLogin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get admin: %w", err)
	}
	a.MFASecret = mfaSecret.String
	a.MFAEnabled = mfaEnabled == 1
	if lastLogin.Valid {
		a.LastLogin = &lastLogin.Time
	}
	return &a, nil
}

// ListAdmins returns all admin accounts.
func (s *Store) ListAdmins() ([]Admin, error) {
	rows, err := s.db.Query(
		`SELECT id, display_name, password_hash, mfa_secret, mfa_enabled, created_at, last_login
		 FROM admins ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	defer rows.Close()

	var admins []Admin
	for rows.Next() {
		var a Admin
		var mfaSecret sql.NullString
		var mfaEnabled int
		var lastLogin sql.NullTime
		if err := rows.Scan(&a.ID, &a.DisplayName, &a.PasswordHash, &mfaSecret, &mfaEnabled, &a.CreatedAt, &lastLogin); err != nil {
			return nil, fmt.Errorf("scan admin: %w", err)
		}
		a.MFASecret = mfaSecret.String
		a.MFAEnabled = mfaEnabled == 1
		if lastLogin.Valid {
			a.LastLogin = &lastLogin.Time
		}
		admins = append(admins, a)
	}
	return admins, nil
}

// DeleteAdmin removes an admin by username (id).
func (s *Store) DeleteAdmin(id string) error {
	result, err := s.db.Exec(`DELETE FROM admins WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete admin: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("admin not found: %s", id)
	}
	return nil
}

// UpdateAdminPassword updates an admin's password hash.
func (s *Store) UpdateAdminPassword(id, passwordHash string) error {
	result, err := s.db.Exec(`UPDATE admins SET password_hash = ? WHERE id = ?`, passwordHash, id)
	if err != nil {
		return fmt.Errorf("update admin password: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("admin not found: %s", id)
	}
	return nil
}

// UpdateAdminMFA updates an admin's MFA secret and enabled state.
func (s *Store) UpdateAdminMFA(id, secret string, enabled bool) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	result, err := s.db.Exec(`UPDATE admins SET mfa_secret = ?, mfa_enabled = ? WHERE id = ?`, secret, enabledInt, id)
	if err != nil {
		return fmt.Errorf("update admin mfa: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("admin not found: %s", id)
	}
	return nil
}

// UpdateAdminLastLogin sets the last_login timestamp.
func (s *Store) UpdateAdminLastLogin(id string) error {
	_, err := s.db.Exec(`UPDATE admins SET last_login = ? WHERE id = ?`, time.Now(), id)
	return err
}

// HasAdmins returns true if at least one admin exists.
func (s *Store) HasAdmins() (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM admins`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count admins: %w", err)
	}
	return count > 0, nil
}

// AdminCount returns the number of admin accounts.
func (s *Store) AdminCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM admins`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return count, nil
}

// --- Admin API Token CRUD ---

// CreateAdminAPIToken inserts a new admin API token (stores hash, not raw token).
func (s *Store) CreateAdminAPIToken(token *AdminAPIToken) error {
	_, err := s.db.Exec(
		`INSERT INTO admin_api_tokens (token_hash, username, name, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		token.TokenHash, token.Username, token.Name, token.CreatedAt, token.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create admin api token: %w", err)
	}
	return nil
}

// GetAdminAPIToken looks up a token by its SHA256 hash. Returns nil if not found or expired.
func (s *Store) GetAdminAPIToken(tokenHash string) (*AdminAPIToken, error) {
	row := s.db.QueryRow(
		`SELECT token_hash, username, name, created_at, expires_at, last_used
		 FROM admin_api_tokens WHERE token_hash = ?`, tokenHash)
	var t AdminAPIToken
	var expiresAt sql.NullTime
	var lastUsed sql.NullTime
	err := row.Scan(&t.TokenHash, &t.Username, &t.Name, &t.CreatedAt, &expiresAt, &lastUsed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get admin api token: %w", err)
	}
	if expiresAt.Valid {
		t.ExpiresAt = &expiresAt.Time
	}
	if lastUsed.Valid {
		t.LastUsed = &lastUsed.Time
	}
	// Check expiry
	if t.ExpiresAt != nil && t.ExpiresAt.Before(time.Now()) {
		return nil, nil
	}
	return &t, nil
}

// UpdateAdminAPITokenLastUsed updates the last_used timestamp for a token.
func (s *Store) UpdateAdminAPITokenLastUsed(tokenHash string) error {
	_, err := s.db.Exec(`UPDATE admin_api_tokens SET last_used = ? WHERE token_hash = ?`, time.Now(), tokenHash)
	return err
}

// ListAdminAPITokens returns all tokens for a given username.
func (s *Store) ListAdminAPITokens(username string) ([]AdminAPIToken, error) {
	rows, err := s.db.Query(
		`SELECT token_hash, username, name, created_at, expires_at, last_used
		 FROM admin_api_tokens WHERE username = ? ORDER BY created_at DESC`, username)
	if err != nil {
		return nil, fmt.Errorf("list admin api tokens: %w", err)
	}
	defer rows.Close()

	var tokens []AdminAPIToken
	for rows.Next() {
		var t AdminAPIToken
		var expiresAt sql.NullTime
		var lastUsed sql.NullTime
		if err := rows.Scan(&t.TokenHash, &t.Username, &t.Name, &t.CreatedAt, &expiresAt, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan admin api token: %w", err)
		}
		if expiresAt.Valid {
			t.ExpiresAt = &expiresAt.Time
		}
		if lastUsed.Valid {
			t.LastUsed = &lastUsed.Time
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

// ListAllAdminAPITokens returns all admin API tokens.
func (s *Store) ListAllAdminAPITokens() ([]AdminAPIToken, error) {
	rows, err := s.db.Query(
		`SELECT token_hash, username, name, created_at, expires_at, last_used
		 FROM admin_api_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list all admin api tokens: %w", err)
	}
	defer rows.Close()

	var tokens []AdminAPIToken
	for rows.Next() {
		var t AdminAPIToken
		var expiresAt sql.NullTime
		var lastUsed sql.NullTime
		if err := rows.Scan(&t.TokenHash, &t.Username, &t.Name, &t.CreatedAt, &expiresAt, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan admin api token: %w", err)
		}
		if expiresAt.Valid {
			t.ExpiresAt = &expiresAt.Time
		}
		if lastUsed.Valid {
			t.LastUsed = &lastUsed.Time
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

// DeleteAdminAPIToken removes a token by its hash.
func (s *Store) DeleteAdminAPIToken(tokenHash string) error {
	result, err := s.db.Exec(`DELETE FROM admin_api_tokens WHERE token_hash = ?`, tokenHash)
	if err != nil {
		return fmt.Errorf("delete admin api token: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}
