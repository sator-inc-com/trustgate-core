package controlplane

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Department represents a department in the master list.
type Department struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ApiKey      string    `json:"api_key"`
	AgentCount  int       `json:"agent_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// generateDeptApiKey creates a department-specific API key: tg_dept_{id}_{random16hex}
func generateDeptApiKey(deptID string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("tg_dept_%s_%s", deptID, hex.EncodeToString(b)), nil
}

// ListDepartments returns all departments with agent counts.
func (s *Store) ListDepartments() ([]Department, error) {
	rows, err := s.db.Query(`
		SELECT d.id, d.name, d.description, d.api_key, d.created_at,
		       COUNT(a.agent_id) AS agent_count
		FROM departments d
		LEFT JOIN agents a ON json_extract(a.labels, '$.department') = d.id
		GROUP BY d.id
		ORDER BY d.name
	`)
	if err != nil {
		return nil, fmt.Errorf("list departments: %w", err)
	}
	defer rows.Close()

	var departments []Department
	for rows.Next() {
		var d Department
		if err := rows.Scan(&d.ID, &d.Name, &d.Description, &d.ApiKey, &d.CreatedAt, &d.AgentCount); err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		departments = append(departments, d)
	}
	return departments, rows.Err()
}

// GetDepartment returns a single department by ID.
func (s *Store) GetDepartment(id string) (*Department, error) {
	row := s.db.QueryRow(`
		SELECT d.id, d.name, d.description, d.api_key, d.created_at,
		       COUNT(a.agent_id) AS agent_count
		FROM departments d
		LEFT JOIN agents a ON json_extract(a.labels, '$.department') = d.id
		WHERE d.id = ?
		GROUP BY d.id
	`, id)

	var d Department
	err := row.Scan(&d.ID, &d.Name, &d.Description, &d.ApiKey, &d.CreatedAt, &d.AgentCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get department: %w", err)
	}
	return &d, nil
}

// GetDepartmentByApiKey returns a department by its API key.
func (s *Store) GetDepartmentByApiKey(apiKey string) (*Department, error) {
	if !strings.HasPrefix(apiKey, "tg_dept_") {
		return nil, nil
	}
	row := s.db.QueryRow(`
		SELECT d.id, d.name, d.description, d.api_key, d.created_at,
		       COUNT(a.agent_id) AS agent_count
		FROM departments d
		LEFT JOIN agents a ON json_extract(a.labels, '$.department') = d.id
		WHERE d.api_key = ?
		GROUP BY d.id
	`, apiKey)

	var d Department
	err := row.Scan(&d.ID, &d.Name, &d.Description, &d.ApiKey, &d.CreatedAt, &d.AgentCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get department by api key: %w", err)
	}
	return &d, nil
}

// CreateDepartment inserts a new department with an auto-generated API key.
func (s *Store) CreateDepartment(id, name, description string) error {
	apiKey, err := generateDeptApiKey(id)
	if err != nil {
		return fmt.Errorf("generate dept api key: %w", err)
	}
	_, err = s.db.Exec(
		"INSERT INTO departments (id, name, description, api_key, created_at) VALUES (?, ?, ?, ?, ?)",
		id, name, description, apiKey, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("create department: %w", err)
	}
	return nil
}

// UpdateDepartment updates an existing department's name and description.
func (s *Store) UpdateDepartment(id, name, description string) error {
	result, err := s.db.Exec(
		"UPDATE departments SET name = ?, description = ? WHERE id = ?",
		name, description, id,
	)
	if err != nil {
		return fmt.Errorf("update department: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("department not found: %s", id)
	}
	return nil
}

// DeleteDepartment removes a department by ID.
func (s *Store) DeleteDepartment(id string) error {
	result, err := s.db.Exec("DELETE FROM departments WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete department: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("department not found: %s", id)
	}
	return nil
}

// HasDepartments returns true if the departments table has any entries.
func (s *Store) HasDepartments() (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM departments").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count departments: %w", err)
	}
	return count > 0, nil
}

// DepartmentExists returns true if a department with the given ID exists.
func (s *Store) DepartmentExists(id string) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM departments WHERE id = ?", id).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check department exists: %w", err)
	}
	return count > 0, nil
}

// GetDepartmentMap returns a map of department ID to department name.
func (s *Store) GetDepartmentMap() (map[string]string, error) {
	rows, err := s.db.Query("SELECT id, name FROM departments")
	if err != nil {
		return nil, fmt.Errorf("get department map: %w", err)
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("scan department map: %w", err)
		}
		m[id] = name
	}
	return m, rows.Err()
}
