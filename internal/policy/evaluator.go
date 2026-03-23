package policy

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/trustgate/trustgate/internal/detector"
	"github.com/trustgate/trustgate/internal/identity"
)

// EvalContext holds the context for policy evaluation.
type EvalContext struct {
	Phase     string // input | output
	Identity  identity.Identity
	Findings  []detector.Finding
	RiskScore float64
	AppID     string
}

// EvalResult represents the result of evaluating all policies.
type EvalResult struct {
	Action        string            `json:"action"` // allow | warn | mask | block
	PolicyName    string            `json:"policy_name,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	Message       string            `json:"message,omitempty"`
	ShadowResults []ShadowResult    `json:"shadow_results,omitempty"`
	Trace         []PolicyTraceItem `json:"trace,omitempty"`
}

// ShadowResult records what would have happened in shadow mode.
type ShadowResult struct {
	PolicyName string `json:"policy_name"`
	Action     string `json:"action"`
	Reason     string `json:"reason"`
}

// PolicyTraceItem records evaluation details for debug mode.
type PolicyTraceItem struct {
	Policy  string `json:"policy"`
	Phase   string `json:"phase"`
	Matched bool   `json:"matched"`
	Reason  string `json:"reason"`
	Action  string `json:"action,omitempty"`
}

// Evaluator evaluates policies against findings.
// Thread-safe: policies can be updated at runtime via UpdatePolicies.
type Evaluator struct {
	mu       sync.RWMutex
	policies []Policy
}

func NewEvaluator(policies []Policy) *Evaluator {
	return &Evaluator{policies: policies}
}

// UpdatePolicies replaces the current policy set with new policies.
// Thread-safe: uses write lock. Ongoing Evaluate calls will complete
// with the old policies; subsequent calls will use the new set.
func (e *Evaluator) UpdatePolicies(policies []Policy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policies = policies
}

// PolicyCount returns the number of loaded policies. Thread-safe.
func (e *Evaluator) PolicyCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.policies)
}

// Evaluate runs all policies and returns the final action.
func (e *Evaluator) Evaluate(ctx EvalContext) EvalResult {
	e.mu.RLock()
	policies := e.policies
	e.mu.RUnlock()

	result := EvalResult{Action: "allow"}
	actionPriority := map[string]int{"block": 4, "mask": 3, "warn": 2, "allow": 1}

	for _, p := range policies {
		if p.Mode == "disabled" {
			continue
		}
		if p.Phase != ctx.Phase {
			continue
		}

		matched, reason := e.matchPolicy(p, ctx)
		trace := PolicyTraceItem{
			Policy:  p.Name,
			Phase:   p.Phase,
			Matched: matched,
			Reason:  reason,
		}

		if matched {
			trace.Action = p.Action

			if p.Mode == "shadow" {
				result.ShadowResults = append(result.ShadowResults, ShadowResult{
					PolicyName: p.Name,
					Action:     p.Action,
					Reason:     reason,
				})
			} else {
				if actionPriority[p.Action] > actionPriority[result.Action] {
					result.Action = p.Action
					result.PolicyName = p.Name
					result.Reason = reason
					result.Message = p.Message
				}
			}
		}

		result.Trace = append(result.Trace, trace)

		// Short-circuit on block (enforce mode only)
		if matched && p.Action == "block" && p.Mode == "enforce" {
			break
		}
	}

	return result
}

func (e *Evaluator) matchPolicy(p Policy, ctx EvalContext) (bool, string) {
	// Check whitelist first
	if p.Whitelist != nil && e.isWhitelisted(p.Whitelist, ctx) {
		return false, "whitelisted"
	}

	// Session-based conditions
	if p.When.Session != nil {
		if ctx.RiskScore >= p.When.Session.RiskScoreGte {
			return true, formatReason("risk_score %.2f >= threshold %.2f", ctx.RiskScore, p.When.Session.RiskScoreGte)
		}
		return false, formatReason("risk_score %.2f < threshold %.2f", ctx.RiskScore, p.When.Session.RiskScoreGte)
	}

	// Identity-based conditions
	if p.When.Identity != nil {
		if !e.matchIdentity(p.When.Identity, ctx.Identity) {
			return false, "identity condition not met"
		}
		// Identity match + detector check (if specified)
		if p.When.Detector != "" {
			return e.matchDetector(p.When, ctx.Findings)
		}
		return true, "identity condition met"
	}

	// Detector-based conditions
	if p.When.Detector != "" {
		return e.matchDetector(p.When, ctx.Findings)
	}

	return false, "no conditions matched"
}

func (e *Evaluator) matchDetector(cond PolicyCondition, findings []detector.Finding) (bool, string) {
	minSev := detector.SeverityOrder(cond.MinSeverity)

	for _, f := range findings {
		if f.Detector == cond.Detector && detector.SeverityOrder(f.Severity) >= minSev {
			return true, formatReason("%s detected (%s, severity=%s)", cond.Detector, f.Category, f.Severity)
		}
	}
	return false, formatReason("no %s detected (min_severity=%s)", cond.Detector, cond.MinSeverity)
}

func (e *Evaluator) matchIdentity(im *IdentityMatch, id identity.Identity) bool {
	if im.Clearance != nil {
		for _, c := range im.Clearance.NotIn {
			if strings.EqualFold(id.Clearance, c) {
				return false
			}
		}
		return true
	}
	return true
}

func (e *Evaluator) isWhitelisted(wl *Whitelist, ctx EvalContext) bool {
	if wl.Identity != nil {
		for _, role := range wl.Identity.Role {
			if strings.EqualFold(ctx.Identity.Role, role) {
				return true
			}
		}
	}

	if len(wl.AppID) > 0 {
		for _, appID := range wl.AppID {
			if ctx.AppID == appID {
				return true
			}
		}
	}

	if len(wl.Patterns) > 0 {
		// Check if any finding matches a whitelist pattern
		for _, f := range ctx.Findings {
			for _, pattern := range wl.Patterns {
				if matched, _ := regexp.MatchString(pattern, f.Matched); matched {
					return true
				}
			}
		}
	}

	return false
}

func formatReason(format string, args ...any) string {
	return strings.TrimSpace(strings.ReplaceAll(
		formatString(format, args...),
		"  ", " ",
	))
}

func formatString(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
