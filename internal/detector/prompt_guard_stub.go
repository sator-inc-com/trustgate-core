//go:build nollm

package detector

import "github.com/trustgate/trustgate/internal/config"

// PromptGuardDetector is a stub when built without LLM support.
type PromptGuardDetector struct{}

// PromptGuardResult is a stub (must match prompt_guard.go).
type PromptGuardResult struct {
	Label      string
	Confidence float64
	Scores     [2]float64
}

func NewPromptGuardDetector(_ config.LLMDetectorConfig) *PromptGuardDetector {
	return &PromptGuardDetector{}
}

func (d *PromptGuardDetector) Name() string        { return "prompt_guard" }
func (d *PromptGuardDetector) Ready() bool          { return false }
func (d *PromptGuardDetector) LoadModel() error     { return nil }
func (d *PromptGuardDetector) Classify(_ string) ([]Finding, error) { return nil, nil }
func (d *PromptGuardDetector) Close() error         { return nil }
