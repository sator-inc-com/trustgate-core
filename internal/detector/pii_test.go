package detector

import (
	"testing"

	"github.com/trustgate/trustgate/internal/config"
)

func newTestPIIDetector() *PIIDetector {
	return NewPIIDetector(config.PIIConfig{
		Enabled: true,
		Patterns: map[string]bool{
			"email":         true,
			"phone":         true,
			"mobile":        true,
			"my_number":     true,
			"credit_card":   true,
			"postal_code":   true,
			"ip_address":    true,
			"address":       true,
			"date_of_birth": true,
			"bank_account":  true,
		},
	})
}

func TestPIIDetector_Email(t *testing.T) {
	d := newTestPIIDetector()

	tests := []struct {
		input string
		want  int
	}{
		{"yamada@example.com", 1},
		{"連絡先は user@company.co.jp です", 1},
		{"a@b.cc", 1},
		{"メールなし", 0},
		{"@だけ", 0},
		{"user@@example.com", 0},
	}

	for _, tt := range tests {
		findings := d.Detect(tt.input)
		emails := filterByCategory(findings, "email")
		if len(emails) != tt.want {
			t.Errorf("input=%q: got %d email findings, want %d", tt.input, len(emails), tt.want)
		}
	}
}

func TestPIIDetector_Phone(t *testing.T) {
	d := newTestPIIDetector()

	tests := []struct {
		input string
		want  int
		desc  string
	}{
		{"03-1234-5678", 1, "Tokyo landline"},
		{"090-1234-5678", 1, "Mobile"},
		{"080-1234-5678", 1, "Mobile 080"},
		{"070-1234-5678", 1, "Mobile 070"},
		{"0120-123-456", 1, "Free dial"},
		{"電話なし", 0, "No phone"},
	}

	for _, tt := range tests {
		findings := d.Detect(tt.input)
		phones := filterByCategory(findings, "phone")
		mobiles := filterByCategory(findings, "mobile")
		total := len(phones) + len(mobiles)
		if total < tt.want {
			t.Errorf("%s: input=%q: got %d phone findings, want >=%d", tt.desc, tt.input, total, tt.want)
		}
	}
}

func TestPIIDetector_CreditCard(t *testing.T) {
	d := newTestPIIDetector()

	tests := []struct {
		input string
		want  int
	}{
		{"4111-1111-1111-1111", 1},
		{"4111 1111 1111 1111", 1},
		{"4111111111111111", 1},
		{"1234", 0},
	}

	for _, tt := range tests {
		findings := d.Detect(tt.input)
		cards := filterByCategory(findings, "credit_card")
		if len(cards) != tt.want {
			t.Errorf("input=%q: got %d credit_card findings, want %d", tt.input, len(cards), tt.want)
		}
	}
}

func TestPIIDetector_MyNumber(t *testing.T) {
	d := newTestPIIDetector()

	tests := []struct {
		input string
		want  int
	}{
		{"1234 5678 9012", 1},
		{"123456789012", 1},
		{"123", 0},
	}

	for _, tt := range tests {
		findings := d.Detect(tt.input)
		mynum := filterByCategory(findings, "my_number")
		if len(mynum) < tt.want {
			t.Errorf("input=%q: got %d my_number findings, want >=%d", tt.input, len(mynum), tt.want)
		}
	}
}

func TestPIIDetector_CustomPattern(t *testing.T) {
	d := NewPIIDetector(config.PIIConfig{
		Enabled:  true,
		Patterns: map[string]bool{},
		Custom: []config.CustomPattern{
			{Name: "employee_id", Pattern: `EMP-\d{6}`, Severity: "high"},
		},
	})

	findings := d.Detect("社員番号は EMP-123456 です")
	if len(findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(findings))
		return
	}
	if findings[0].Category != "employee_id" {
		t.Errorf("expected category employee_id, got %s", findings[0].Category)
	}
}

func TestPIIDetector_Address(t *testing.T) {
	d := newTestPIIDetector()

	tests := []struct {
		input string
		want  int
		desc  string
	}{
		{"東京都渋谷区神南1-23-4", 1, "Tokyo address with chome"},
		{"大阪府大阪市北区梅田2-5-10", 1, "Osaka address"},
		{"北海道札幌市中央区北1条西2-3", 1, "Hokkaido address"},
		{"京都府京都市左京区下鴨3-15", 1, "Kyoto address"},
		{"神奈川県横浜市中区本町6-50-1", 1, "Kanagawa address"},
		{"住所は未定です", 0, "No address"},
		{"東京都の天気", 0, "Prefecture mention without address"},
	}

	for _, tt := range tests {
		findings := d.Detect(tt.input)
		addrs := filterByCategory(findings, "address")
		if len(addrs) != tt.want {
			t.Errorf("%s: input=%q: got %d address findings, want %d", tt.desc, tt.input, len(addrs), tt.want)
		}
	}
}

func TestPIIDetector_DateOfBirth(t *testing.T) {
	d := newTestPIIDetector()

	tests := []struct {
		input string
		want  int
		desc  string
	}{
		{"生年月日: 1990/05/15", 1, "DOB with slash"},
		{"生年月日：1990年5月15日", 1, "DOB with Japanese format"},
		{"誕生日: 2000-12-25", 1, "Birthday with hyphen"},
		{"date of birth: 1985-03-10", 1, "English DOB"},
		{"DOB: 1995/01/01", 1, "DOB abbreviation"},
		{"2026年3月22日の会議", 0, "Regular date without DOB context"},
		{"納期は2026/04/01です", 0, "Deadline date"},
	}

	for _, tt := range tests {
		findings := d.Detect(tt.input)
		dobs := filterByCategory(findings, "date_of_birth")
		if len(dobs) != tt.want {
			t.Errorf("%s: input=%q: got %d date_of_birth findings, want %d", tt.desc, tt.input, len(dobs), tt.want)
		}
	}
}

func TestPIIDetector_BankAccount(t *testing.T) {
	d := newTestPIIDetector()

	tests := []struct {
		input string
		want  int
		desc  string
	}{
		{"口座番号: 1234567", 1, "JP bank account 7 digits"},
		{"口座番号：12345678", 1, "JP bank account 8 digits"},
		{"口座No: 7654321", 1, "Account No format"},
		{"account number: 12345678", 1, "English account number"},
		{"注文番号: 1234567", 0, "Order number (not bank)"},
		{"ID: 12345678", 0, "Generic ID"},
	}

	for _, tt := range tests {
		findings := d.Detect(tt.input)
		accounts := filterByCategory(findings, "bank_account")
		if len(accounts) != tt.want {
			t.Errorf("%s: input=%q: got %d bank_account findings, want %d", tt.desc, tt.input, len(accounts), tt.want)
		}
	}
}

func TestPIIDetector_NoFalsePositives(t *testing.T) {
	d := newTestPIIDetector()

	benign := []string{
		"今日の天気を教えてください",
		"売上レポートの作成方法",
		"バージョン1.2.3について",
		"2026年3月21日の会議",
	}

	for _, input := range benign {
		findings := d.Detect(input)
		if len(findings) > 0 {
			t.Errorf("false positive on %q: got %d findings: %v", input, len(findings), findings[0].Category)
		}
	}
}

func filterByCategory(findings []Finding, category string) []Finding {
	var result []Finding
	for _, f := range findings {
		if f.Category == category {
			result = append(result, f)
		}
	}
	return result
}
