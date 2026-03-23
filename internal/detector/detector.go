package detector

// Detector inspects text and returns findings.
type Detector interface {
	Name() string
	Detect(input string) []Finding
}

// LLMDetector performs deep inspection using a local LLM model.
// Used as Stage 2 in the two-stage detection pipeline.
type LLMDetector interface {
	Name() string
	// Classify takes text and returns findings with confidence scores.
	// Only called for gray-zone inputs where regex is uncertain.
	Classify(input string) ([]Finding, error)
	// Ready returns true if the model is loaded and ready for inference.
	Ready() bool
	// Close releases model resources.
	Close() error
}

// Finding represents a single detection result.
type Finding struct {
	Detector    string  `json:"detector"`
	Category    string  `json:"category"`
	Severity    string  `json:"severity"`    // low, medium, high, critical
	Confidence  float64 `json:"confidence"`  // 0.0-1.0, used for gray-zone escalation
	Description string  `json:"description"`
	Matched     string  `json:"matched"`
	Position    int     `json:"position"`
	Length      int     `json:"length"`
}

// IsGrayZone returns true if the finding's confidence is below the
// threshold for definitive classification, indicating it should be
// escalated to the LLM detector for Stage 2 analysis.
func (f Finding) IsGrayZone(threshold float64) bool {
	return f.Confidence > 0 && f.Confidence < threshold
}

// SeverityOrder returns the numeric priority of a severity level (higher = more severe).
func SeverityOrder(s string) int {
	switch s {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// EscalationReason describes why a text was escalated to Stage 2.
type EscalationReason string

const (
	EscalationLowConfidence  EscalationReason = "low_confidence"    // regex matched but confidence < threshold
	EscalationMixedLanguage  EscalationReason = "mixed_language"    // language mixing detected
	EscalationEncodedContent EscalationReason = "encoded_content"   // encoded/obfuscated content
	EscalationHighRiskSession EscalationReason = "high_risk_session" // session risk score elevated
	EscalationSeparators     EscalationReason = "separators"        // suspicious separator patterns
)
