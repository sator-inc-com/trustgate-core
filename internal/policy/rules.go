package policy

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// PolicyFile represents the top-level policies YAML structure.
type PolicyFile struct {
	Version  string   `yaml:"version"`
	Policies []Policy `yaml:"policies"`
}

// Policy defines a single policy rule.
type Policy struct {
	Name    string         `yaml:"name"`
	Phase   string         `yaml:"phase"` // input | output
	When    PolicyCondition `yaml:"when"`
	Action  string         `yaml:"action"` // allow | warn | mask | block
	Mode    string         `yaml:"mode"`   // enforce | shadow | disabled
	Message string         `yaml:"message"`
	Whitelist *Whitelist   `yaml:"whitelist,omitempty"`
}

// PolicyCondition defines when a policy triggers.
type PolicyCondition struct {
	Detector    string           `yaml:"detector"`
	MinSeverity string           `yaml:"min_severity"`
	Identity    *IdentityMatch   `yaml:"identity,omitempty"`
}

type IdentityMatch struct {
	Clearance *ClearanceMatch `yaml:"clearance,omitempty"`
	Role      *RoleMatch      `yaml:"role,omitempty"`
}

type ClearanceMatch struct {
	NotIn []string `yaml:"not_in,omitempty"`
}

type RoleMatch struct {
	In    []string `yaml:"in,omitempty"`
	NotIn []string `yaml:"not_in,omitempty"`
}

type Whitelist struct {
	Identity *WhitelistIdentity `yaml:"identity,omitempty"`
	Patterns []string           `yaml:"patterns,omitempty"`
	AppID    []string           `yaml:"app_id,omitempty"`
}

type WhitelistIdentity struct {
	Role []string `yaml:"role,omitempty"`
}

// LoadPolicies reads and parses a policies YAML file.
func LoadPolicies(path string) ([]Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policies file: %w", err)
	}

	var pf PolicyFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("parse policies file: %w", err)
	}

	// Set defaults
	for i := range pf.Policies {
		if pf.Policies[i].Mode == "" {
			pf.Policies[i].Mode = "enforce"
		}
	}

	return pf.Policies, nil
}
