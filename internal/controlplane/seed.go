package controlplane

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

// defaultDepartment defines a department to seed on first startup.
type defaultDepartment struct {
	ID          string
	Name        string
	Description string
}

// defaultPolicy defines a policy to seed on first startup.
type defaultPolicy struct {
	Name    string
	Content string
}

var defaultDepartments = []defaultDepartment{
	{ID: "admin", Name: "管理部門", Description: "総務・人事・経理・法務"},
	{ID: "business", Name: "事業部門", Description: "営業・マーケティング・開発"},
}

var defaultPolicies = []defaultPolicy{
	{
		Name: "block_injection_critical",
		Content: `name: block_injection_critical
phase: input
when:
    detector: injection
    min_severity: critical
action: block
message: Prompt injection detected. This attempt has been logged.
whitelist:
    identity:
        role:
            - admin
            - security
`,
	},
	{
		Name: "warn_injection_high",
		Content: `name: warn_injection_high
phase: input
when:
    detector: injection
    min_severity: high
action: warn
whitelist:
    identity:
        role:
            - admin
            - security
`,
	},
	{
		Name: "block_injection_output_critical",
		Content: `name: block_injection_output_critical
phase: output
when:
    detector: injection
    min_severity: critical
action: block
message: Malicious instructions detected in AI response.
whitelist:
    identity:
        role:
            - admin
            - security
`,
	},
	{
		Name: "warn_injection_output_high",
		Content: `name: warn_injection_output_high
phase: output
when:
    detector: injection
    min_severity: high
action: warn
whitelist:
    identity:
        role:
            - admin
            - security
`,
	},
	{
		Name: "block_pii_input",
		Content: `name: block_pii_input
phase: input
when:
    detector: pii
    min_severity: high
action: block
message: Personal information detected. Please remove PII before sending.
whitelist:
    identity:
        role:
            - admin
            - security
`,
	},
	{
		Name: "warn_pii_output",
		Content: `name: warn_pii_output
phase: output
when:
    detector: pii
    min_severity: high
action: warn
whitelist:
    identity:
        role:
            - admin
            - security
`,
	},
	{
		Name: "block_confidential_input",
		Content: `name: block_confidential_input
phase: input
when:
    detector: confidential
    min_severity: critical
action: block
mode: shadow
message: Confidential information detected.
whitelist:
    identity:
        role:
            - admin
            - security
`,
	},
	{
		Name: "block_confidential_output",
		Content: `name: block_confidential_output
phase: output
when:
    detector: confidential
    min_severity: critical
action: block
mode: shadow
message: Confidential information detected in AI response.
whitelist:
    identity:
        role:
            - admin
            - security
`,
	},
}

// SeedDefaults creates default departments, policies, and admin account if the database is empty.
// This is called on server startup so the admin can use the system immediately.
func (s *Store) SeedDefaults(logger zerolog.Logger) error {
	deptCount, err := s.seedDepartments(logger)
	if err != nil {
		return fmt.Errorf("seed departments: %w", err)
	}

	policyCount, err := s.seedPolicies(logger)
	if err != nil {
		return fmt.Errorf("seed policies: %w", err)
	}

	adminSeeded, err := s.seedDefaultAdmin(logger)
	if err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}

	if deptCount > 0 || policyCount > 0 || adminSeeded {
		logger.Info().
			Int("departments", deptCount).
			Int("policies", policyCount).
			Bool("admin_seeded", adminSeeded).
			Msg("seeded default data for fresh database")
	}

	return nil
}

func (s *Store) seedDefaultAdmin(logger zerolog.Logger) (bool, error) {
	hasAdmins, err := s.HasAdmins()
	if err != nil {
		return false, err
	}
	if hasAdmins {
		return false, nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("changeme"), 10)
	if err != nil {
		return false, fmt.Errorf("hash default password: %w", err)
	}

	admin := &Admin{
		ID:           "admin",
		DisplayName:  "Administrator",
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}
	if err := s.CreateAdmin(admin); err != nil {
		return false, fmt.Errorf("create default admin: %w", err)
	}

	logger.Warn().Str("username", "admin").Msg("seeded default admin account (password: changeme). Change it immediately!")
	return true, nil
}

func (s *Store) seedDepartments(logger zerolog.Logger) (int, error) {
	hasDepts, err := s.HasDepartments()
	if err != nil {
		return 0, err
	}
	if hasDepts {
		return 0, nil
	}

	for _, d := range defaultDepartments {
		if err := s.CreateDepartment(d.ID, d.Name, d.Description); err != nil {
			return 0, fmt.Errorf("create department %q: %w", d.ID, err)
		}
		logger.Debug().Str("id", d.ID).Str("name", d.Name).Msg("seeded department")
	}

	return len(defaultDepartments), nil
}

func (s *Store) seedPolicies(logger zerolog.Logger) (int, error) {
	version, err := s.GetLatestPolicyVersion()
	if err != nil {
		return 0, err
	}
	if version > 0 {
		return 0, nil
	}

	records := make([]PolicyRecord, 0, len(defaultPolicies))
	for _, p := range defaultPolicies {
		records = append(records, PolicyRecord{
			Name:    p.Name,
			Content: p.Content,
			Scope:   "global",
		})
	}

	_, err = s.CreatePolicyVersion("default seed policies", "system", records)
	if err != nil {
		return 0, fmt.Errorf("create default policy version: %w", err)
	}

	for _, p := range defaultPolicies {
		logger.Debug().Str("name", p.Name).Msg("seeded policy")
	}

	return len(defaultPolicies), nil
}
