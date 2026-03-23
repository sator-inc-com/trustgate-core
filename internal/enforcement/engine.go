package enforcement

import (
	"strings"

	"github.com/trustgate/trustgate/internal/detector"
)

// MaskFindings replaces matched text with mask characters.
func MaskFindings(text string, findings []detector.Finding, maskChar rune) string {
	if len(findings) == 0 {
		return text
	}

	// Sort findings by position (descending) to replace from end to start
	// so positions don't shift
	sorted := make([]detector.Finding, len(findings))
	copy(sorted, findings)
	sortByPositionDesc(sorted)

	runes := []rune(text)
	for _, f := range sorted {
		if f.Position < 0 || f.Position+f.Length > len(runes) {
			continue
		}
		mask := []rune(strings.Repeat(string(maskChar), f.Length))
		runes = append(runes[:f.Position], append(mask, runes[f.Position+f.Length:]...)...)
	}

	return string(runes)
}

func sortByPositionDesc(findings []detector.Finding) {
	for i := 0; i < len(findings); i++ {
		for j := i + 1; j < len(findings); j++ {
			if findings[j].Position > findings[i].Position {
				findings[i], findings[j] = findings[j], findings[i]
			}
		}
	}
}

// MaskString masks a matched string for safe display in logs/debug.
func MaskString(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	runes := []rune(s)
	visible := 3
	if len(runes) < 6 {
		visible = 1
	}
	masked := string(runes[:visible]) + strings.Repeat("*", len(runes)-visible*2)
	if len(runes) > visible*2 {
		masked += string(runes[len(runes)-visible:])
	}
	return masked
}
