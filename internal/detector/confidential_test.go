package detector

import (
	"testing"

	"github.com/trustgate/trustgate/internal/config"
)

func newTestConfidentialDetector() *ConfidentialDetector {
	return NewConfidentialDetector(config.ConfidentialConfig{
		Enabled: true,
		Keywords: map[string][]string{
			"critical": {"極秘", "社外秘", "CONFIDENTIAL", "TOP SECRET"},
			"high":     {"機密", "内部限定", "INTERNAL ONLY"},
		},
	})
}

func TestConfidentialDetector_Keywords(t *testing.T) {
	d := newTestConfidentialDetector()

	tests := []struct {
		input    string
		want     int
		severity string
	}{
		{"この文書は社外秘です", 1, "critical"},
		{"極秘プロジェクトについて", 1, "critical"},
		{"CONFIDENTIAL document", 1, "critical"},
		{"TOP SECRET material", 1, "critical"},
		{"機密情報を含む", 1, "high"},
		{"INTERNAL ONLY: for staff", 1, "high"},
		{"内部限定の資料", 1, "high"},
		{"普通の文書です", 0, ""},
		{"今日の天気", 0, ""},
	}

	for _, tt := range tests {
		findings := d.Detect(tt.input)
		if len(findings) != tt.want {
			t.Errorf("input=%q: got %d findings, want %d", tt.input, len(findings), tt.want)
			continue
		}
		if tt.want > 0 && findings[0].Severity != tt.severity {
			t.Errorf("input=%q: severity=%s, want %s", tt.input, findings[0].Severity, tt.severity)
		}
	}
}

func TestConfidentialDetector_CaseInsensitive(t *testing.T) {
	d := newTestConfidentialDetector()

	tests := []string{
		"confidential",
		"Confidential",
		"CONFIDENTIAL",
		"top secret",
		"Top Secret",
		"internal only",
	}

	for _, input := range tests {
		findings := d.Detect(input)
		if len(findings) == 0 {
			t.Errorf("input=%q: expected detection (case insensitive)", input)
		}
	}
}

func TestConfidentialDetector_MultipleMatches(t *testing.T) {
	d := newTestConfidentialDetector()
	findings := d.Detect("この極秘文書は社外秘であり、機密扱いです")
	if len(findings) < 3 {
		t.Errorf("expected >=3 findings, got %d", len(findings))
	}
}

func TestConfidentialDetector_CustomKeyword(t *testing.T) {
	d := NewConfidentialDetector(config.ConfidentialConfig{
		Enabled:  true,
		Keywords: map[string][]string{},
		Custom: []config.CustomKeyword{
			{Term: "Project Phoenix", Severity: "critical"},
		},
	})

	findings := d.Detect("Project Phoenix の進捗を教えて")
	if len(findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(findings))
	}
}
