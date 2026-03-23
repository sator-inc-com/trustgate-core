package detector

import (
	"regexp"
	"strings"

	"github.com/trustgate/trustgate/internal/config"
)

type injectionPattern struct {
	name       string
	re         *regexp.Regexp
	severity   string
	category   string
	confidence float64 // base confidence for this pattern (0.0-1.0)
}

type InjectionDetector struct {
	patterns []injectionPattern
}

func NewInjectionDetector(cfg config.InjectionConfig) *InjectionDetector {
	d := &InjectionDetector{}

	hasLang := func(lang string) bool {
		for _, l := range cfg.Language {
			if l == lang {
				return true
			}
		}
		return len(cfg.Language) == 0 // if no languages specified, enable all
	}

	if hasLang("en") {
		enPatterns := []struct {
			name       string
			pattern    string
			severity   string
			category   string
			confidence float64
		}{
			// High confidence: direct, unambiguous attack patterns
			{"ignore_instructions", `(?i)ignore\s+(all\s+)?(previous|above|prior|earlier)\s+(instructions|rules|constraints|guidelines)`, "critical", "instruction_override", 0.95},
			{"ignore_safety", `(?i)ignore\s+(all\s+)?safety\s+(rules|guidelines|filters|policies)`, "critical", "instruction_override", 0.95},
			{"forget_instructions", `(?i)(forget|disregard|override|discard)\s+(everything|all|every)?\s*(above|previous|prior|earlier|before)\b`, "critical", "instruction_override", 0.90},
			{"system_prompt_reveal", `(?i)(show|reveal|display|output|print|tell\s+me)\s+.{0,30}(system\s+prompt|instructions|rules|hidden\s+prompt|initial\s+config)`, "critical", "system_prompt_reveal", 0.90},
			{"jailbreak", `(?i)(jailbreak|DAN|do\s+anything\s+now|no\s+restrictions|unrestricted\s+mode)`, "critical", "jailbreak", 0.85},
			// Medium confidence: could be legitimate in some contexts
			{"role_change", `(?i)(you\s+are\s+now|act\s+as|pretend\s+to\s+be|roleplay\s+as|you\s+must\s+act)`, "high", "role_change", 0.70},
			// Low confidence: often benign, needs LLM verification
			{"encoding_evasion", `(?i)(base64\s+(encode|decode|encod)|hex\s+encod|rot13|unicode\s+escape)`, "medium", "encoding_evasion", 0.50},
		}
		for _, p := range enPatterns {
			d.patterns = append(d.patterns, injectionPattern{
				name:       p.name,
				re:         regexp.MustCompile(p.pattern),
				severity:   p.severity,
				category:   p.category,
				confidence: p.confidence,
			})
		}
	}

	if hasLang("ja") {
		jaPatterns := []struct {
			name       string
			pattern    string
			severity   string
			category   string
			confidence float64
		}{
			{"ja_ignore_instructions", `(前の|上記の|これまでの|すべての)(指示|命令|ルール|制約).{0,10}(無視|忘れ|従わな|取り消)`, "critical", "instruction_override", 0.95},
			{"ja_system_prompt_reveal", `(システムプロンプト|設定内容|指示内容|隠された指示|隠し設定).{0,10}(見せ|教え|表示|出力|開示)`, "critical", "system_prompt_reveal", 0.90},
			{"ja_role_change", `(あなたは|お前は).{0,20}(として振る舞|になりきっ|のふりをし|として行動)`, "high", "role_change", 0.70},
		}
		for _, p := range jaPatterns {
			d.patterns = append(d.patterns, injectionPattern{
				name:       p.name,
				re:         regexp.MustCompile(p.pattern),
				severity:   p.severity,
				category:   p.category,
				confidence: p.confidence,
			})
		}
	}

	for _, cp := range cfg.Custom {
		re, err := regexp.Compile(cp.Pattern)
		if err != nil {
			continue
		}
		sev := cp.Severity
		if sev == "" {
			sev = "high"
		}
		d.patterns = append(d.patterns, injectionPattern{
			name:       cp.Name,
			re:         re,
			severity:   sev,
			category:   cp.Name,
			confidence: 0.80, // custom patterns get moderate confidence
		})
	}

	return d
}

func (d *InjectionDetector) Name() string { return "injection" }

// educationalPatterns matches contexts where attack keywords appear in
// legitimate security education, defense discussions, or explanatory text.
var educationalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(how\s+to\s+(protect|prevent|defend|guard|mitigate)|what\s+(is|are)\s+(common\s+)?(prompt\s+injection|jailbreak|DAN))`),
	regexp.MustCompile(`(?i)(explain|describe|definition\s+of|what\s+is)\s+.{0,30}(jailbreak|DAN|prompt\s+injection)`),
	regexp.MustCompile(`(?i)(protect|prevent|defend|detect)\s+against\s+.{0,30}(injection|jailbreak)`),
	regexp.MustCompile(`(?i)(対策|防御|防止|検出|検知|守る|ベストプラクティス|仕組み).{0,15}(プロンプトインジェクション|ジェイルブレイク|DAN|攻撃)`),
	regexp.MustCompile(`(?i)(プロンプトインジェクション|ジェイルブレイク|DAN|攻撃).{0,15}(対策|防御|防止|検出|検知|守る|ベストプラクティス|仕組み)`),
}

func isEducationalContext(input string) bool {
	for _, re := range educationalPatterns {
		if re.MatchString(input) {
			return true
		}
	}
	return false
}

func (d *InjectionDetector) Detect(input string) []Finding {
	// Apply text normalization to defeat evasion techniques
	normalized := NormalizeText(input)

	// Check if this is an educational/defensive context
	educational := isEducationalContext(normalized)

	var findings []Finding

	for _, p := range d.patterns {
		matches := p.re.FindAllStringIndex(normalized, -1)
		for _, m := range matches {
			matched := normalized[m[0]:m[1]]

			// Skip findings in educational context
			if educational {
				continue
			}

			findings = append(findings, Finding{
				Detector:    "injection",
				Category:    p.category,
				Severity:    p.severity,
				Confidence:  p.confidence,
				Description: p.name + " detected",
				Matched:     matched,
				Position:    m[0],
				Length:      m[1] - m[0],
			})
		}
	}

	// Check for separator-based injection (long text with many separators)
	if len(normalized) > 200 {
		separators := []string{"###", "---", "===", "<<<", ">>>"}
		sepCount := 0
		for _, sep := range separators {
			sepCount += strings.Count(normalized, sep)
		}
		if sepCount >= 3 {
			findings = append(findings, Finding{
				Detector:    "injection",
				Category:    "separator_injection",
				Severity:    "medium",
				Confidence:  0.40, // low confidence — prime candidate for LLM escalation
				Description: "multiple separators in long text",
				Matched:     "(separator pattern)",
				Position:    0,
				Length:      0,
			})
		}
	}

	return findings
}
