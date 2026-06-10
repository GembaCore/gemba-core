package server

import (
	"net/url"
	"testing"
	"time"
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

// gm-g5xz.1 — created_since query param.
func TestParseWorkItemFilter_CreatedSince(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want time.Time
	}{
		{
			name: "rfc3339",
			raw:  "created_since=2026-04-30T10:00:00Z",
			want: time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC),
		},
		{
			name: "rfc3339nano",
			raw:  "created_since=2026-04-30T10:00:00.123456789Z",
			want: time.Date(2026, 4, 30, 10, 0, 0, 123456789, time.UTC),
		},
		{
			name: "yyyymmdd",
			raw:  "created_since=2026-04-30",
			want: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := url.ParseQuery(tc.raw)
			f, err := parseWorkItemFilter(q)
			if err != nil {
				t.Fatalf("parse err: %v", err)
			}
			if f.CreatedSince == nil {
				t.Fatalf("CreatedSince should be set")
			}
			if !f.CreatedSince.Equal(tc.want) {
				t.Errorf("got %v want %v", f.CreatedSince, tc.want)
			}
		})
	}
}

func TestParseWorkItemFilter_CreatedSinceMalformed(t *testing.T) {
	q, _ := url.ParseQuery("created_since=not-a-date")
	if _, err := parseWorkItemFilter(q); err == nil {
		t.Fatal("expected error on malformed created_since")
	}
}

func TestParseWorkItemFilter_CreatedSinceUnset(t *testing.T) {
	q := url.Values{}
	f, err := parseWorkItemFilter(q)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if f.CreatedSince != nil {
		t.Errorf("CreatedSince should default to nil; got %v", *f.CreatedSince)
	}
}
