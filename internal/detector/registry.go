package detector

import (
	"unicode"

	"github.com/trustgate/trustgate/internal/config"
)

// Registry holds all active detectors and manages the two-stage pipeline.
type Registry struct {
	detectors []Detector
	llm       LLMDetector        // Stage 2 (nil if disabled)
	llmCfg    config.LLMDetectorConfig
}

// NewRegistry creates a registry with detectors enabled by config.
func NewRegistry(cfg config.DetectorConfig) *Registry {
	r := &Registry{
		llmCfg: cfg.LLM,
	}

	if cfg.PII.Enabled {
		r.Register(NewPIIDetector(cfg.PII))
	}
	if cfg.Injection.Enabled {
		r.Register(NewInjectionDetector(cfg.Injection))
	}
	if cfg.Confidential.Enabled {
		r.Register(NewConfidentialDetector(cfg.Confidential))
	}

	return r
}

// Register adds a detector to the registry.
func (r *Registry) Register(d Detector) {
	r.detectors = append(r.detectors, d)
}

// SetLLMDetector sets the Stage 2 LLM detector.
func (r *Registry) SetLLMDetector(llm LLMDetector) {
	r.llm = llm
}

// DetectAll runs Stage 1 (regex) detectors. If an LLM detector is
// configured and the input has gray-zone characteristics, it escalates
// to Stage 2 for deeper analysis.
func (r *Registry) DetectAll(input string) []Finding {
	// Stage 1: regex-based detection (all requests, <5ms)
	var findings []Finding
	for _, d := range r.detectors {
		findings = append(findings, d.Detect(input)...)
	}

	// Check if Stage 2 escalation is needed
	if r.llm == nil || !r.llm.Ready() {
		return findings
	}

	threshold := r.llmCfg.EscalationThreshold
	if threshold == 0 {
		threshold = 0.8
	}

	shouldEscalate, reasons := r.shouldEscalate(input, findings, threshold)
	if !shouldEscalate {
		return findings
	}

	// Stage 2: LLM-based detection (gray-zone only, 1-5ms with Prompt Guard 2)
	llmFindings, err := r.llm.Classify(input)
	if err != nil {
		// On LLM error, return Stage 1 findings as-is (fail-open for Stage 2)
		return findings
	}

	// Merge: LLM findings supplement/override low-confidence regex findings
	return mergeFindings(findings, llmFindings, reasons, threshold)
}

// DetectAllStage1Only runs only Stage 1 (regex) detectors, skipping LLM.
func (r *Registry) DetectAllStage1Only(input string) []Finding {
	var findings []Finding
	for _, d := range r.detectors {
		findings = append(findings, d.Detect(input)...)
	}
	return findings
}

// Detectors returns the list of registered detectors.
func (r *Registry) Detectors() []Detector {
	return r.detectors
}

// LLMReady returns true if a Stage 2 LLM detector is loaded and ready.
func (r *Registry) LLMReady() bool {
	return r.llm != nil && r.llm.Ready()
}

// shouldEscalate determines if input needs Stage 2 LLM analysis.
func (r *Registry) shouldEscalate(input string, findings []Finding, threshold float64) (bool, []EscalationReason) {
	var reasons []EscalationReason

	// 1. Any finding with low confidence → escalate
	for _, f := range findings {
		if f.IsGrayZone(threshold) {
			reasons = append(reasons, EscalationLowConfidence)
			break
		}
	}

	// 2. Mixed language detection (potential evasion)
	if r.llmCfg.EscalateOnMixedLanguage && isMixedLanguage(input) {
		reasons = append(reasons, EscalationMixedLanguage)
	}

	// 3. Encoded content markers
	if r.llmCfg.EscalateOnEncodedContent && hasEncodedContent(input) {
		reasons = append(reasons, EscalationEncodedContent)
	}

	return len(reasons) > 0, reasons
}

// mergeFindings combines Stage 1 and Stage 2 results.
// LLM findings with higher confidence override regex findings for the same category.
func mergeFindings(regexFindings, llmFindings []Finding, _ []EscalationReason, threshold float64) []Finding {
	result := make([]Finding, 0, len(regexFindings)+len(llmFindings))

	// Keep high-confidence regex findings as-is
	for _, f := range regexFindings {
		if f.Confidence >= threshold {
			result = append(result, f)
		}
		// Low-confidence regex findings may be replaced by LLM results
	}

	// Add LLM findings (these have higher accuracy for gray-zone cases)
	result = append(result, llmFindings...)

	return result
}

// isMixedLanguage detects if text contains both CJK and Latin characters
// in patterns that suggest language-mixing evasion.
func isMixedLanguage(input string) bool {
	hasCJK := false
	hasLatin := false
	for _, r := range input {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hiragana, r) {
			hasCJK = true
		}
		if unicode.IsLetter(r) && r < 0x0100 {
			hasLatin = true
		}
		if hasCJK && hasLatin {
			return true
		}
	}
	return false
}

// hasEncodedContent checks for base64-like or hex-encoded content.
func hasEncodedContent(input string) bool {
	// Look for long sequences of base64 characters (40+ chars)
	consecutive := 0
	for _, r := range input {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			consecutive++
			if consecutive >= 40 {
				return true
			}
		} else {
			consecutive = 0
		}
	}
	return false
}
