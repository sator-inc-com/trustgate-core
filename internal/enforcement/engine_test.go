package enforcement

import (
	"testing"

	"github.com/trustgate/trustgate/internal/detector"
)

func TestMaskFindings(t *testing.T) {
	tests := []struct {
		input    string
		findings []detector.Finding
		want     string
	}{
		{
			"yamada@example.com に送って",
			[]detector.Finding{{Position: 0, Length: 18}},
			"****************** に送って",
		},
		{
			"連絡先は 090-1234-5678 です",
			[]detector.Finding{{Position: 5, Length: 13}},
			"連絡先は ************* です",
		},
		{
			"no findings here",
			nil,
			"no findings here",
		},
	}

	for _, tt := range tests {
		got := MaskFindings(tt.input, tt.findings, '*')
		if got != tt.want {
			t.Errorf("MaskFindings(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMaskString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ab", "**"},
		{"abc", "***"},
		{"abcd", "****"},
		{"yamada@example.com", "yam************com"},
	}

	for _, tt := range tests {
		got := MaskString(tt.input)
		if got != tt.want {
			t.Errorf("MaskString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
