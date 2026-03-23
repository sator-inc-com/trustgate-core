package controlplane

import (
	"database/sql"
	"fmt"
	"time"
)

// PolicyRecord represents a single policy stored in the Control Plane.
type PolicyRecord struct {
	ID        int       `json:"id"`
	Version   int       `json:"version"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	Scope     string    `json:"scope"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// PolicyVersion represents a policy version record.
type PolicyVersion struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Comment   string    `json:"comment"`
}

// GetLatestPolicyVersion returns the latest policy version number.
func (s *Store) GetLatestPolicyVersion() (int, error) {
	var version int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM policy_versions`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("get latest policy version: %w", err)
	}
	return version, nil
}

// CreatePolicyVersion creates a new policy version and inserts all policies.
func (s *Store) CreatePolicyVersion(comment string, createdBy string, policies []PolicyRecord) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Get next version
	var nextVersion int
	err = tx.QueryRow(`SELECT COALESCE(MAX(version), 0) + 1 FROM policy_versions`).Scan(&nextVersion)
	if err != nil {
		return 0, fmt.Errorf("get next version: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert version record
	_, err = tx.Exec(`INSERT INTO policy_versions (version, created_at, comment) VALUES (?, ?, ?)`,
		nextVersion, now, comment)
	if err != nil {
		return 0, fmt.Errorf("insert policy version: %w", err)
	}

	// Insert each policy
	for _, p := range policies {
		scope := p.Scope
		if scope == "" {
			scope = "global"
		}
		_, err = tx.Exec(`INSERT INTO policies (version, name, content, scope, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?)`,
			nextVersion, p.Name, p.Content, scope, now, createdBy)
		if err != nil {
			return 0, fmt.Errorf("insert policy %q: %w", p.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return nextVersion, nil
}

// GetPoliciesByVersion returns all policies for a specific version.
func (s *Store) GetPoliciesByVersion(version int) ([]PolicyRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, version, name, content, scope, created_at, created_by
		FROM policies WHERE version = ? ORDER BY name`, version)
	if err != nil {
		return nil, fmt.Errorf("get policies by version: %w", err)
	}
	defer rows.Close()

	return scanPolicies(rows)
}

// GetPoliciesByVersionAndScope returns policies for a specific version filtered by scope.
// If scope is empty, all policies are returned. If scope is a department ID,
// both global and department-specific policies are returned.
func (s *Store) GetPoliciesByVersionAndScope(version int, scope string) ([]PolicyRecord, error) {
	var rows *sql.Rows
	var err error

	if scope == "" || scope == "all" {
		// Return all policies
		rows, err = s.db.Query(`
			SELECT id, version, name, content, scope, created_at, created_by
			FROM policies WHERE version = ? ORDER BY scope, name`, version)
	} else if scope == "global" {
		// Return only global policies
		rows, err = s.db.Query(`
			SELECT id, version, name, content, scope, created_at, created_by
			FROM policies WHERE version = ? AND scope = 'global' ORDER BY name`, version)
	} else {
		// Return global + department-specific policies
		rows, err = s.db.Query(`
			SELECT id, version, name, content, scope, created_at, created_by
			FROM policies WHERE version = ? AND (scope = 'global' OR scope = ?) ORDER BY scope, name`, version, scope)
	}

	if err != nil {
		return nil, fmt.Errorf("get policies by version and scope: %w", err)
	}
	defer rows.Close()

	return scanPolicies(rows)
}

// GetPoliciesForDepartment returns global + department-specific policies at the latest version.
func (s *Store) GetPoliciesForDepartment(deptID string) (int, []PolicyRecord, error) {
	version, err := s.GetLatestPolicyVersion()
	if err != nil {
		return 0, nil, err
	}
	if version == 0 {
		return 0, nil, nil
	}
	policies, err := s.GetPoliciesByVersionAndScope(version, deptID)
	if err != nil {
		return 0, nil, err
	}
	return version, policies, nil
}

// GetLatestPolicies returns all policies at the latest version.
func (s *Store) GetLatestPolicies() (int, []PolicyRecord, error) {
	version, err := s.GetLatestPolicyVersion()
	if err != nil {
		return 0, nil, err
	}
	if version == 0 {
		return 0, nil, nil
	}
	policies, err := s.GetPoliciesByVersion(version)
	if err != nil {
		return 0, nil, err
	}
	return version, policies, nil
}

// ListPolicyVersions returns all policy versions.
func (s *Store) ListPolicyVersions() ([]PolicyVersion, error) {
	rows, err := s.db.Query(`SELECT version, created_at, comment FROM policy_versions ORDER BY version DESC`)
	if err != nil {
		return nil, fmt.Errorf("list policy versions: %w", err)
	}
	defer rows.Close()

	var versions []PolicyVersion
	for rows.Next() {
		var v PolicyVersion
		var createdAt string
		if err := rows.Scan(&v.Version, &createdAt, &v.Comment); err != nil {
			return nil, fmt.Errorf("scan policy version: %w", err)
		}
		v.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func scanPolicies(rows *sql.Rows) ([]PolicyRecord, error) {
	var policies []PolicyRecord
	for rows.Next() {
		var p PolicyRecord
		var createdAt string
		if err := rows.Scan(&p.ID, &p.Version, &p.Name, &p.Content, &p.Scope, &createdAt, &p.CreatedBy); err != nil {
			return nil, fmt.Errorf("scan policy: %w", err)
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if p.Scope == "" {
			p.Scope = "global"
		}
		policies = append(policies, p)
	}
	return policies, rows.Err()
}
