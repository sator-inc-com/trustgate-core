package detector

import (
	"regexp"
	"strings"

	"github.com/trustgate/trustgate/internal/config"
)

// negationPattern matches negation words immediately before confidential keywords.
// Handles: "not confidential", "isn't confidential", "non-confidential", "非機密"
var negationPattern = regexp.MustCompile(`(?i)(not|isn'?t|non-?|no longer|never|without|除く|非|ではない|じゃない)\s*$`)

type confidentialEntry struct {
	term     string
	lower    string
	severity string
}

type ConfidentialDetector struct {
	entries []confidentialEntry
}

func NewConfidentialDetector(cfg config.ConfidentialConfig) *ConfidentialDetector {
	d := &ConfidentialDetector{}

	for severity, keywords := range cfg.Keywords {
		for _, kw := range keywords {
			d.entries = append(d.entries, confidentialEntry{
				term:     kw,
				lower:    strings.ToLower(kw),
				severity: severity,
			})
		}
	}

	for _, ck := range cfg.Custom {
		sev := ck.Severity
		if sev == "" {
			sev = "high"
		}
		d.entries = append(d.entries, confidentialEntry{
			term:     ck.Term,
			lower:    strings.ToLower(ck.Term),
			severity: sev,
		})
	}

	return d
}

func (d *ConfidentialDetector) Name() string { return "confidential" }

func (d *ConfidentialDetector) Detect(input string) []Finding {
	normalized := NormalizeText(input)
	var findings []Finding
	lower := strings.ToLower(normalized)

	for _, e := range d.entries {
		idx := 0
		for {
			pos := strings.Index(lower[idx:], e.lower)
			if pos == -1 {
				break
			}
			absPos := idx + pos
			matched := normalized[absPos : absPos+len(e.term)]

			// Check for negation context before the match
			if isNegated(lower, absPos) {
				idx = absPos + len(e.term)
				continue
			}

			findings = append(findings, Finding{
				Detector:    "confidential",
				Category:    "keyword",
				Severity:    e.severity,
				Description: "confidential keyword: " + e.term,
				Matched:     matched,
				Position:    absPos,
				Length:      len(e.term),
			})
			idx = absPos + len(e.term)
		}
	}

	return findings
}

// isNegated checks if the text immediately before position contains a negation word.
func isNegated(lower string, pos int) bool {
	if pos == 0 {
		return false
	}
	// Look at up to 20 characters before the match
	start := pos - 20
	if start < 0 {
		start = 0
	}
	prefix := lower[start:pos]
	return negationPattern.MatchString(prefix)
}
