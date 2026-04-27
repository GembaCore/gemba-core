package gastown_test

import (
	"context"
	"testing"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/shader/gastown"
)

func newShader(t *testing.T) *gastown.Shader {
	t.Helper()
	sh, err := gastown.New(gastown.Config{Rig: "gemba", RigAbbr: "gm"})
	if err != nil {
		t.Fatalf("gastown.New: %v", err)
	}
	return sh
}

// Round-trip: encode → decode == original. Pinned for every kind in
// the default prefix table — the contract that lets the SPA edit a
// title without losing the prefix on re-read.
func TestShader_RoundTrip(t *testing.T) {
	sh := newShader(t)
	cases := []struct {
		kind  string
		title string
		// encoded is the literal string we expect on the wire.
		encoded string
	}{
		{"bug", "/api/beads wire-shape mismatch", "BUG: /api/beads wire-shape mismatch"},
		{"decision", "Bead entity presentation", "DESIGN: Bead entity presentation"},
		{"epic", "First bead on screen", "[epic] First bead on screen"},
		{"task", "Quickstart doc", "Quickstart doc"},
		{"chore", "rename routes", "rename routes"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			native := core.WorkItem{ID: "gm-1", Kind: tc.kind, Title: tc.title}
			encoded, err := sh.EncodeForWrite(context.Background(), core.WriteCreate, native)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if encoded.Title != tc.encoded {
				t.Errorf("encoded title = %q, want %q", encoded.Title, tc.encoded)
			}
			decoded, err := sh.DecodeFromRead(context.Background(), encoded)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.Title != tc.title {
				t.Errorf("round-trip drift: %q → %q → %q", tc.title, encoded.Title, decoded.Title)
			}
		})
	}
}

// Encode is idempotent — re-encoding an already-encoded title doesn't
// stack prefixes ("BUG: BUG: …"). This matters when an external import
// brings beads that were already in encoded form.
func TestShader_Encode_Idempotent(t *testing.T) {
	sh := newShader(t)
	once, _ := sh.EncodeForWrite(context.Background(), core.WriteCreate,
		core.WorkItem{ID: "gm-1", Kind: "bug", Title: "/api/beads"})
	twice, _ := sh.EncodeForWrite(context.Background(), core.WriteCreate, once)
	if once.Title != twice.Title {
		t.Errorf("encode not idempotent: %q vs %q", once.Title, twice.Title)
	}
}

// Wisps bypass title transforms — they're non-canonical Gas Town
// scaffolding and don't follow the prefix convention.
func TestShader_WispsBypass(t *testing.T) {
	sh := newShader(t)
	wisp := core.WorkItem{ID: "gm-wisp-jag4", Kind: "bug", Title: "should not be prefixed"}
	encoded, _ := sh.EncodeForWrite(context.Background(), core.WriteCreate, wisp)
	if encoded.Title != wisp.Title {
		t.Errorf("wisp encode altered title: %q", encoded.Title)
	}
	decoded, _ := sh.DecodeFromRead(context.Background(), wisp)
	if decoded.Title != wisp.Title {
		t.Errorf("wisp decode altered title: %q", decoded.Title)
	}
}

// Unknown kinds (no entry in KindPrefixes) pass through unchanged
// rather than being dropped or erroring. Lets new kinds land
// server-side before the orchestrator config knows about them.
func TestShader_UnknownKindPassesThrough(t *testing.T) {
	sh := newShader(t)
	wi := core.WorkItem{ID: "gm-1", Kind: "molecule", Title: "raw"}
	encoded, _ := sh.EncodeForWrite(context.Background(), core.WriteCreate, wi)
	if encoded.Title != "raw" {
		t.Errorf("unknown kind altered: %q", encoded.Title)
	}
}

// Custom KindPrefixes override the inferred defaults — operators
// running a non-standard rig can pin their own conventions.
func TestShader_CustomPrefixes(t *testing.T) {
	sh, err := gastown.New(gastown.Config{
		KindPrefixes: map[string]string{"task": "TASK: "},
	})
	if err != nil {
		t.Fatalf("gastown.New: %v", err)
	}
	out, err := sh.EncodeForWrite(context.Background(), core.WriteCreate,
		core.WorkItem{ID: "gm-1", Kind: "task", Title: "x"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out.Title != "TASK: x" {
		t.Errorf("custom prefix not applied: %q", out.Title)
	}
}

// Decode tolerates titles that don't start with the expected prefix
// — operator-renamed items pass through unchanged.
func TestShader_Decode_NoPrefix_PassesThrough(t *testing.T) {
	sh := newShader(t)
	wi := core.WorkItem{ID: "gm-1", Kind: "bug", Title: "renamed without prefix"}
	out, _ := sh.DecodeFromRead(context.Background(), wi)
	if out.Title != wi.Title {
		t.Errorf("decode altered no-prefix title: %q", out.Title)
	}
}

func TestShader_Describe(t *testing.T) {
	sh := newShader(t)
	m := sh.Describe()
	if m.Name != "gastown" {
		t.Errorf("Name = %q, want gastown", m.Name)
	}
	found := false
	for _, f := range m.EncodedFields {
		if f == "title" {
			found = true
		}
	}
	if !found {
		t.Errorf("EncodedFields must include 'title'; got %+v", m.EncodedFields)
	}
}

// Bad title_format → New returns an error so a misconfigured config
// fails startup loudly rather than silently producing garbage.
func TestShader_BadTitleFormat_ErrorsAtConstruction(t *testing.T) {
	_, err := gastown.New(gastown.Config{TitleFormat: "{{.unclosed"})
	if err == nil {
		t.Fatal("expected error from malformed title_format")
	}
}
