package tenant

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseID_Valid(t *testing.T) {
	cases := []string{
		"t-default",
		"t-abcd1234",
		"t-aaaaaaaa",
		"t-77777777",
		"t-abcdefgh",
		"t-0123abcd",
	}
	for _, in := range cases {
		got, err := ParseID(in)
		if err != nil {
			t.Errorf("ParseID(%q) unexpected err: %v", in, err)
			continue
		}
		if string(got) != in {
			t.Errorf("ParseID(%q) = %q, want %q", in, got, in)
		}
	}
}

func TestParseID_Invalid(t *testing.T) {
	cases := []struct {
		name, in string
	}{
		{"empty", ""},
		{"no prefix", "abcd1234"},
		{"wrong prefix", "x-abcd1234"},
		{"too short", "t-abc"},
		{"too long", "t-abcd12345"},
		{"uppercase", "t-ABCD1234"},
		{"hyphen in body", "t-abcd-234"},
		{"special char", "t-abcd!234"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseID(tc.in)
			if err == nil {
				t.Fatalf("ParseID(%q) expected error, got nil", tc.in)
			}
			if !errors.Is(err, ErrInvalidTenantID) {
				t.Errorf("ParseID(%q) err = %v, want ErrInvalidTenantID", tc.in, err)
			}
		})
	}
}

func TestParseWSID_ColonForm(t *testing.T) {
	w, err := ParseWSID("t-abcd:my-project")
	if err != nil {
		t.Fatalf("ParseWSID err: %v", err)
	}
	if string(w.Tenant) != "t-abcd" {
		t.Errorf("Tenant = %q, want t-abcd", w.Tenant)
	}
	if w.Slug != "my-project" {
		t.Errorf("Slug = %q, want my-project", w.Slug)
	}
}

func TestParseWSID_BareFormDefaultsToDefaultTenant(t *testing.T) {
	w, err := ParseWSID("my-project")
	if err != nil {
		t.Fatalf("ParseWSID err: %v", err)
	}
	if string(w.Tenant) != DefaultTenant.Prefix() {
		t.Errorf("Tenant = %q, want %q (DefaultTenant.Prefix)", w.Tenant, DefaultTenant.Prefix())
	}
	if w.Slug != "my-project" {
		t.Errorf("Slug = %q, want my-project", w.Slug)
	}
}

func TestParseWSID_Invalid(t *testing.T) {
	cases := []struct {
		name, in string
	}{
		{"empty", ""},
		{"empty slug", "t-abcd:"},
		{"bad prefix scheme", "x-abcd:slug"},
		{"prefix too short", "t:slug"},
		{"prefix body invalid", "t-AB!d:slug"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseWSID(tc.in)
			if err == nil {
				t.Fatalf("ParseWSID(%q) expected error", tc.in)
			}
			if !errors.Is(err, ErrInvalidWSID) {
				t.Errorf("err = %v, want ErrInvalidWSID", err)
			}
		})
	}
}

func TestWSID_RoundTrip(t *testing.T) {
	cases := []string{
		"t-abcd:my-project",
		"t-defa:legacy-slug",
		"t-77777:slug",
	}
	for _, in := range cases {
		w, err := ParseWSID(in)
		if err != nil {
			t.Fatalf("ParseWSID(%q): %v", in, err)
		}
		s := w.String()
		w2, err := ParseWSID(s)
		if err != nil {
			t.Fatalf("ParseWSID(%q): %v", s, err)
		}
		if w != w2 {
			t.Errorf("round-trip: %+v -> %q -> %+v", w, s, w2)
		}
		if s != in {
			t.Errorf("String() = %q, want %q", s, in)
		}
	}
}

func TestWSID_BareRoundTrip(t *testing.T) {
	// Bare form does not round-trip to bare — it canonicalizes to
	// the colon form rooted at the default tenant.
	w, err := ParseWSID("legacy")
	if err != nil {
		t.Fatalf("ParseWSID: %v", err)
	}
	if got, want := w.String(), "t-defa:legacy"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	w2, err := ParseWSID(w.String())
	if err != nil {
		t.Fatalf("ParseWSID re-parse: %v", err)
	}
	if w != w2 {
		t.Errorf("round-trip mismatch: %+v vs %+v", w, w2)
	}
}

func TestNewID_ValidAndUnique(t *testing.T) {
	seen := make(map[ID]bool)
	for i := 0; i < 100; i++ {
		id := NewID()
		if _, err := ParseID(string(id)); err != nil {
			t.Fatalf("NewID produced invalid id %q: %v", id, err)
		}
		if seen[id] {
			t.Fatalf("NewID produced duplicate id %q", id)
		}
		seen[id] = true
		if !strings.HasPrefix(string(id), "t-") {
			t.Errorf("NewID missing t- prefix: %q", id)
		}
		if len(id) != 10 {
			t.Errorf("NewID length = %d, want 10", len(id))
		}
	}
}

func TestPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"t-abcd1234", "t-abcd"},
		{"t-77777777", "t-7777"},
		{"t-default", "t-defa"},
		{"t-", "t-"},
	}
	for _, tc := range cases {
		if got := ID(tc.in).Prefix(); got != tc.want {
			t.Errorf("ID(%q).Prefix() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if _, ok := FromContext(ctx); ok {
		t.Fatal("FromContext on empty ctx should report ok=false")
	}
	ctx = WithContext(ctx, "t-abcd1234")
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("FromContext ok=false after WithContext")
	}
	if got != "t-abcd1234" {
		t.Errorf("FromContext = %q, want t-abcd1234", got)
	}
}

func TestFullTenant(t *testing.T) {
	w, _ := ParseWSID("t-abcd:proj")
	if got := w.FullTenant("t-abcd1234"); got != "t-abcd1234" {
		t.Errorf("FullTenant match: got %q, want t-abcd1234", got)
	}
	if got := w.FullTenant("t-zzzz9999"); got != "" {
		t.Errorf("FullTenant mismatch: got %q, want empty", got)
	}
}
