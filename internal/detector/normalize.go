package detector

import (
	"strings"
	"unicode/utf8"
)

// NormalizeText preprocesses input text to improve detection accuracy
// against evasion techniques. Applied before pattern matching.
//
// Normalizations:
//   - Fullwidth ASCII (Ａ-Ｚ, ａ-ｚ, ０-９) → halfwidth
//   - Fullwidth symbols (！-～) → halfwidth
//   - Remove zero-width characters (ZWSP, ZWNJ, ZWJ, BOM)
//   - Remove soft hyphens
func NormalizeText(input string) string {
	var b strings.Builder
	b.Grow(len(input))

	for i := 0; i < len(input); {
		r, size := utf8.DecodeRuneInString(input[i:])

		switch {
		// Zero-width characters — remove entirely
		case r == '\u200B' || // Zero Width Space
			r == '\u200C' || // Zero Width Non-Joiner
			r == '\u200D' || // Zero Width Joiner
			r == '\uFEFF' || // BOM / Zero Width No-Break Space
			r == '\u00AD': // Soft Hyphen
			// skip

		// Fullwidth ASCII variants (U+FF01 ！ to U+FF5E ～) → halfwidth (U+0021 to U+007E)
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFF01 + 0x0021)

		// Fullwidth space → normal space
		case r == 0x3000:
			b.WriteByte(' ')

		default:
			b.WriteRune(r)
		}

		i += size
	}

	return b.String()
}
