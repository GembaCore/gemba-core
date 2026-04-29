package server

import (
	"net/url"
	"testing"
)

// gm-e12.22.1 — query-string parsing for the new IncludeTemplates +
// IncludeWisps filter flags.

func TestParseWorkItemFilter_IncludeTemplates(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"include_templates=true", true},
		{"include_templates=1", true},
		{"include_templates=yes", true},
		{"include_templates=on", true},
		{"include_templates=TRUE", true},
		{"include_templates=false", false},
		{"include_templates=0", false},
		{"include_templates=", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			q, _ := url.ParseQuery(tc.raw)
			f, err := parseWorkItemFilter(q)
			if err != nil {
				t.Fatalf("parse err: %v", err)
			}
			if f.IncludeTemplates != tc.want {
				t.Errorf("IncludeTemplates: got %v want %v", f.IncludeTemplates, tc.want)
			}
		})
	}
}

func TestParseWorkItemFilter_IncludeWisps(t *testing.T) {
	q, _ := url.ParseQuery("include_wisps=true")
	f, err := parseWorkItemFilter(q)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if !f.IncludeWisps {
		t.Error("IncludeWisps should be true")
	}
}

func TestParseWorkItemFilter_BothFlagsIndependent(t *testing.T) {
	q, _ := url.ParseQuery("include_templates=true&include_wisps=true")
	f, err := parseWorkItemFilter(q)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if !f.IncludeTemplates || !f.IncludeWisps {
		t.Errorf("both flags should be true; got templates=%v wisps=%v",
			f.IncludeTemplates, f.IncludeWisps)
	}
}

func TestParseWorkItemFilter_DefaultBothFalse(t *testing.T) {
	// "no filter" wire shape — neither flag set on the URL.
	q := url.Values{}
	f, err := parseWorkItemFilter(q)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if f.IncludeTemplates || f.IncludeWisps {
		t.Errorf("defaults should be false; got templates=%v wisps=%v",
			f.IncludeTemplates, f.IncludeWisps)
	}
}
