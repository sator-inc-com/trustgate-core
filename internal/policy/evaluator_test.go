package policy

import (
	"testing"

	"github.com/trustgate/trustgate/internal/detector"
	"github.com/trustgate/trustgate/internal/identity"
)

func testPolicies() []Policy {
	return []Policy{
		{Name: "block_injection", Phase: "input", When: PolicyCondition{Detector: "injection", MinSeverity: "critical"}, Action: "block", Mode: "enforce", Message: "blocked"},
		{Name: "warn_injection", Phase: "input", When: PolicyCondition{Detector: "injection", MinSeverity: "high"}, Action: "warn", Mode: "enforce"},
		{Name: "mask_pii", Phase: "input", When: PolicyCondition{Detector: "pii", MinSeverity: "high"}, Action: "mask", Mode: "enforce"},
		{Name: "shadow_test", Phase: "input", When: PolicyCondition{Detector: "pii", MinSeverity: "high"}, Action: "block", Mode: "shadow"},
	}
}

func TestEvaluator_Allow(t *testing.T) {
	e := NewEvaluator(testPolicies())
	result := e.Evaluate(EvalContext{
		Phase:    "input",
		Identity: identity.Identity{UserID: "test"},
		Findings: nil,
	})
	if result.Action != "allow" {
		t.Errorf("expected allow, got %s", result.Action)
	}
}

func TestEvaluator_Block(t *testing.T) {
	e := NewEvaluator(testPolicies())
	result := e.Evaluate(EvalContext{
		Phase:    "input",
		Identity: identity.Identity{UserID: "test"},
		Findings: []detector.Finding{
			{Detector: "injection", Category: "instruction_override", Severity: "critical"},
		},
	})
	if result.Action != "block" {
		t.Errorf("expected block, got %s", result.Action)
	}
	if result.PolicyName != "block_injection" {
		t.Errorf("expected policy block_injection, got %s", result.PolicyName)
	}
}

func TestEvaluator_Mask(t *testing.T) {
	e := NewEvaluator(testPolicies())
	result := e.Evaluate(EvalContext{
		Phase:    "input",
		Identity: identity.Identity{UserID: "test"},
		Findings: []detector.Finding{
			{Detector: "pii", Category: "email", Severity: "high"},
		},
	})
	if result.Action != "mask" {
		t.Errorf("expected mask, got %s", result.Action)
	}
}

func TestEvaluator_Warn(t *testing.T) {
	e := NewEvaluator(testPolicies())
	result := e.Evaluate(EvalContext{
		Phase:    "input",
		Identity: identity.Identity{UserID: "test"},
		Findings: []detector.Finding{
			{Detector: "injection", Category: "role_change", Severity: "high"},
		},
	})
	if result.Action != "warn" {
		t.Errorf("expected warn, got %s", result.Action)
	}
}

func TestEvaluator_ShadowMode(t *testing.T) {
	e := NewEvaluator(testPolicies())
	result := e.Evaluate(EvalContext{
		Phase:    "input",
		Identity: identity.Identity{UserID: "test"},
		Findings: []detector.Finding{
			{Detector: "pii", Category: "email", Severity: "high"},
		},
	})
	// Should be mask (enforce), not block (shadow)
	if result.Action != "mask" {
		t.Errorf("expected mask (shadow block should not apply), got %s", result.Action)
	}
	// Shadow result should be recorded
	if len(result.ShadowResults) == 0 {
		t.Error("expected shadow results to be recorded")
	}
}

func TestEvaluator_PhaseFiltering(t *testing.T) {
	e := NewEvaluator(testPolicies())
	// Output phase should not match input policies
	result := e.Evaluate(EvalContext{
		Phase:    "output",
		Identity: identity.Identity{UserID: "test"},
		Findings: []detector.Finding{
			{Detector: "injection", Category: "instruction_override", Severity: "critical"},
		},
	})
	if result.Action != "allow" {
		t.Errorf("expected allow (no output policies), got %s", result.Action)
	}
}

func TestEvaluator_Whitelist(t *testing.T) {
	policies := []Policy{
		{
			Name:  "mask_pii",
			Phase: "input",
			When:  PolicyCondition{Detector: "pii", MinSeverity: "high"},
			Action: "mask",
			Mode:  "enforce",
			Whitelist: &Whitelist{
				Identity: &WhitelistIdentity{Role: []string{"admin"}},
			},
		},
	}
	e := NewEvaluator(policies)

	// Admin should be whitelisted
	result := e.Evaluate(EvalContext{
		Phase:    "input",
		Identity: identity.Identity{UserID: "admin_user", Role: "admin"},
		Findings: []detector.Finding{
			{Detector: "pii", Category: "email", Severity: "high"},
		},
	})
	if result.Action != "allow" {
		t.Errorf("expected allow (whitelisted admin), got %s", result.Action)
	}

	// Non-admin should not be whitelisted
	result = e.Evaluate(EvalContext{
		Phase:    "input",
		Identity: identity.Identity{UserID: "normal_user", Role: "analyst"},
		Findings: []detector.Finding{
			{Detector: "pii", Category: "email", Severity: "high"},
		},
	})
	if result.Action != "mask" {
		t.Errorf("expected mask (not whitelisted), got %s", result.Action)
	}
}

func TestEvaluator_ActionPriority(t *testing.T) {
	// Block > Mask > Warn > Allow
	policies := []Policy{
		{Name: "warn_first", Phase: "input", When: PolicyCondition{Detector: "pii", MinSeverity: "medium"}, Action: "warn", Mode: "enforce"},
		{Name: "mask_second", Phase: "input", When: PolicyCondition{Detector: "pii", MinSeverity: "high"}, Action: "mask", Mode: "enforce"},
	}
	e := NewEvaluator(policies)
	result := e.Evaluate(EvalContext{
		Phase:    "input",
		Findings: []detector.Finding{
			{Detector: "pii", Category: "email", Severity: "high"},
		},
	})
	if result.Action != "mask" {
		t.Errorf("expected mask (higher priority than warn), got %s", result.Action)
	}
}
