package walk_summary

import (
	"strings"
	"unicode"
)

// slugMaxLen is the cap on the slug portion of the filename. 60 keeps
// the filename comfortably under the 255-byte filesystem limit even
// when prefixed with the date and combined with a long extension; it
// also keeps `ls docs/walks/` readable on a narrow terminal.
const slugMaxLen = 60

// slug normalizes a free-form label into a filesystem-safe stem:
// lowercase ASCII alphanumerics joined by single hyphens, no leading
// or trailing hyphen, capped at slugMaxLen. Any rune that's not a
// letter or digit collapses to a hyphen; consecutive hyphens collapse
// to one. Returns "" when the input contains no alphanumerics — the
// caller is expected to fall back to the walk id in that case.
func slug(in string) string {
	if in == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(in))
	prevHyphen := true // suppress leading hyphen
	for _, r := range in {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > slugMaxLen {
		out = strings.TrimRight(out[:slugMaxLen], "-")
	}
	return out
}
