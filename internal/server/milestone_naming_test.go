// Tests for milestone auto-numbering (gm-lw6h).

package server

import "testing"

func TestExtractMilestoneNumber(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"M1", 1},
		{"M1 Beta release", 1},
		{"M10 Q3 push", 10},
		{"M100", 100},
		{"  M2 leading space", 2},
		{"Beta release", 0},
		{"", 0},
		{"M Beta", 0},
		{"m1 lowercase", 0},
		{"MX1 not digits", 0},
	}
	for _, tc := range cases {
		if got := extractMilestoneNumber(tc.in); got != tc.want {
			t.Errorf("extractMilestoneNumber(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNextMilestoneNumber(t *testing.T) {
	cases := []struct {
		name   string
		titles []string
		want   int
	}{
		{"empty", nil, 1},
		{"only M1", []string{"M1 Beta"}, 2},
		{"max wins", []string{"M3 Late", "M1 Early", "M2 Mid"}, 4},
		{"gap not reused", []string{"M1", "M5"}, 6},
		{"non-prefixed ignored", []string{"Random epic", "M2 Active"}, 3},
		{"all non-prefixed", []string{"Beta", "Q3"}, 1},
		{"large numbers", []string{"M999 huge"}, 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextMilestoneNumber(tc.titles); got != tc.want {
				t.Errorf("nextMilestoneNumber(%v) = %d, want %d",
					tc.titles, got, tc.want)
			}
		})
	}
}

func TestApplyMilestonePrefix(t *testing.T) {
	cases := []struct {
		name  string
		title string
		n     int
		want  string
	}{
		{"empty title gets prefix only", "", 1, "M1"},
		{"plain title", "Beta release", 1, "M1 Beta release"},
		{"unpadded across orders", "Q3 push", 10, "M10 Q3 push"},
		{"already prefixed left alone", "M5 existing", 9, "M5 existing"},
		{"already prefixed M1 left alone", "M1", 9, "M1"},
		{"whitespace title trimmed", "  beta  ", 2, "M2 beta"},
		{"lowercase m not a prefix", "m1 sneaky", 3, "M3 m1 sneaky"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyMilestonePrefix(tc.title, tc.n)
			if got != tc.want {
				t.Errorf("applyMilestonePrefix(%q, %d) = %q, want %q",
					tc.title, tc.n, got, tc.want)
			}
		})
	}
}
