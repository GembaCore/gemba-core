package walk_summary

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"Simple", "simple"},
		{"With Spaces", "with-spaces"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"Multiple   spaces", "multiple-spaces"},
		{"slash/path:test", "slash-path-test"},
		{"Unicode — em dash", "unicode-em-dash"},
		{"!!!only-punct!!!", "only-punct"},
		{"!@#$%^&*()", ""},
		{"trailing-hyphens---", "trailing-hyphens"},
		{"Dec 2026 retro: Q4 close-out", "dec-2026-retro-q4-close-out"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := slug(tc.in)
			if got != tc.want {
				t.Errorf("slug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSlug_LengthCap(t *testing.T) {
	in := strings.Repeat("a", 200)
	got := slug(in)
	if len(got) != slugMaxLen {
		t.Errorf("len = %d, want %d", len(got), slugMaxLen)
	}
}

// Words near the cap should not produce a trailing hyphen — the
// truncate path runs TrimRight after slicing.
func TestSlug_NoTrailingHyphenAfterTruncate(t *testing.T) {
	// 60-char cap; build "aaa-aaa-aaa-...-aaa-" so the truncated form
	// would end with a hyphen if TrimRight didn't fire.
	parts := strings.Repeat("aaa ", 30) // alternating words and spaces
	got := slug(parts)
	if strings.HasSuffix(got, "-") {
		t.Errorf("slug %q ends with hyphen", got)
	}
}
