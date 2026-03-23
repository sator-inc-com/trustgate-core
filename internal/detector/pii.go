package detector

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/trustgate/trustgate/internal/config"
)

// versionContextPattern detects version-like prefixes before IP-like strings.
var versionContextPattern = regexp.MustCompile(`(?i)(version|ver\.?|v|バージョン|ヴァージョン)\s*$`)

// postalContextPattern validates postal code context (requires 〒 or Japanese address context).
var postalLabelPattern = regexp.MustCompile(`(?i)(〒|郵便番号|postal|zip)\s*:?\s*$`)

// addressContextPattern detects Japanese address patterns (都道府県+市区町村+番地).
var addressContextPattern = regexp.MustCompile(`(東京都|北海道|(?:京都|大阪)府|.{2,3}県)(.{1,8}[市区町村郡]).{0,20}(\d{1,4}[-ー−]\d{1,4}([-ー−]\d{1,4})?)`)

// dobContextPattern detects date of birth with context labels.
var dobContextPattern = regexp.MustCompile(`(?i)(生年月日|誕生日|birthday|date\s*of\s*birth|DOB|生まれ)\s*[:：]?\s*(\d{4}[/\-年]\d{1,2}[/\-月]\d{1,2}日?)`)

// bankAccountPattern detects bank account numbers with context.
var bankAccountContextPattern = regexp.MustCompile(`(?i)(口座番号|口座No|account\s*(number|no|#))\s*[:：]?\s*(\d{6,8})`)

type piiPattern struct {
	name     string
	re       *regexp.Regexp
	severity string
	category string
}

type PIIDetector struct {
	patterns []piiPattern
}

func NewPIIDetector(cfg config.PIIConfig) *PIIDetector {
	d := &PIIDetector{}

	builtins := []struct {
		key      string
		pattern  string
		severity string
		category string
	}{
		{"email", `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`, "high", "email"},
		{"phone", `0\d{1,4}-\d{1,4}-\d{3,4}`, "high", "phone"},
		{"mobile", `0[789]0[\s-]*\d{4}[\s-]*\d{4}`, "high", "mobile"},
		{"my_number", `\d{4}\s?\d{4}\s?\d{4}`, "critical", "my_number"},
		{"credit_card", `\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}`, "critical", "credit_card"},
		{"postal_code", `〒\d{3}-?\d{4}|(?:^|[^\d])\d{3}-\d{4}(?:[^\d]|$)`, "medium", "postal_code"},
		{"ip_address", `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`, "low", "ip_address"},
		// P1: 住所（都道府県+市区町村+番地）
		{"address", addressContextPattern.String(), "high", "address"},
		// P1: 生年月日（ラベル付きのみ検出、誤検知を抑制）
		{"date_of_birth", dobContextPattern.String(), "high", "date_of_birth"},
		// P1: 口座番号（ラベル付きのみ検出）
		{"bank_account", bankAccountContextPattern.String(), "high", "bank_account"},
	}

	for _, b := range builtins {
		enabled, exists := cfg.Patterns[b.key]
		if !exists || enabled {
			d.patterns = append(d.patterns, piiPattern{
				name:     b.key,
				re:       regexp.MustCompile(b.pattern),
				severity: b.severity,
				category: b.category,
			})
		}
	}

	for _, cp := range cfg.Custom {
		re, err := regexp.Compile(cp.Pattern)
		if err != nil {
			continue
		}
		d.patterns = append(d.patterns, piiPattern{
			name:     cp.Name,
			re:       re,
			severity: cp.Severity,
			category: cp.Name,
		})
	}

	return d
}

func (d *PIIDetector) Name() string { return "pii" }

func (d *PIIDetector) Detect(input string) []Finding {
	normalized := NormalizeText(input)
	var findings []Finding

	for _, p := range d.patterns {
		matches := p.re.FindAllStringIndex(normalized, -1)
		for _, m := range matches {
			matched := normalized[m[0]:m[1]]

			// Filter: IP address false positives (version numbers)
			if p.category == "ip_address" && isLikelyVersionNumber(normalized, matched, m[0]) {
				continue
			}

			// Filter: postal code false positives (SKU codes, arbitrary digit pairs)
			if p.category == "postal_code" && !isLikelyPostalCode(normalized, matched, m[0]) {
				continue
			}

			findings = append(findings, Finding{
				Detector:    "pii",
				Category:    p.category,
				Severity:    p.severity,
				Description: p.name + " detected",
				Matched:     matched,
				Position:    m[0],
				Length:      m[1] - m[0],
			})
		}
	}

	return findings
}

// isLikelyVersionNumber checks if an IP-like match is actually a version number.
func isLikelyVersionNumber(input, matched string, pos int) bool {
	// Check prefix for version-like context
	start := pos - 20
	if start < 0 {
		start = 0
	}
	prefix := input[start:pos]
	if versionContextPattern.MatchString(prefix) {
		return true
	}

	// Validate IP octets: each must be 0-255; version numbers often have small octets
	parts := strings.Split(matched, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n > 255 {
			return true // invalid octet = not a real IP
		}
	}
	return false
}

// isLikelyPostalCode checks if a postal-code-like match has supporting context.
func isLikelyPostalCode(input, matched string, pos int) bool {
	// If it starts with 〒, it's definitely a postal code
	if strings.HasPrefix(matched, "〒") {
		return true
	}

	// Check if preceded by postal-related context
	start := pos - 20
	if start < 0 {
		start = 0
	}
	prefix := input[start:pos]
	if postalLabelPattern.MatchString(prefix) {
		return true
	}

	// Japanese postal codes: 3 digits + hyphen + 4 digits (nnn-nnnn)
	// Without context, only accept if it matches the standard format with hyphen
	if len(matched) == 8 && matched[3] == '-' {
		// Could be postal code, accept it
		return true
	}

	// Reject: bare digit sequences without context (like SKU codes "1234-5678")
	// A 7-digit continuous number without context is ambiguous — reject
	if len(matched) == 7 && !strings.Contains(matched, "-") {
		return false
	}

	// If preceded by product/SKU-like labels, reject
	lowerPrefix := strings.ToLower(prefix)
	if strings.Contains(lowerPrefix, "sku") || strings.Contains(lowerPrefix, "id") ||
		strings.Contains(lowerPrefix, "code") || strings.Contains(lowerPrefix, "番号") {
		return false
	}

	return true
}
