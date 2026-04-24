package preamble

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/adapter/native/agents"
)

const sampleProfile = `# Interaction profile

Preamble text that should NOT appear in any section.

## dangerous

Never ask. Never stop.

## balanced

Stop for questions AND blockers.

Subsection:

### Formatting

Use the contract below.

## cautious

Surface every question. Stop only for blockers.

## Reference

Not a mode section — MUST NOT resolve.
`

func TestPickSection_selectsEachMode(t *testing.T) {
	cases := map[string]string{
		"dangerous": "Never ask. Never stop.",
		"balanced":  "Stop for questions AND blockers.",
		"cautious":  "Surface every question. Stop only for blockers.",
	}
	for mode, want := range cases {
		t.Run(mode, func(t *testing.T) {
			got := pickSection(sampleProfile, mode)
			if !strings.Contains(got, want) {
				t.Errorf("mode %q: body missing %q\nfull:\n%s", mode, want, got)
			}
		})
	}
}

func TestPickSection_balancedIncludesH3Subsection(t *testing.T) {
	// H3 headings inside a section must NOT terminate the section —
	// they're part of the body. Guards the balanced section's
	// "### Formatting" block from getting clipped.
	got := pickSection(sampleProfile, "balanced")
	if !strings.Contains(got, "### Formatting") {
		t.Errorf("balanced section lost its H3 subsection:\n%s", got)
	}
	if !strings.Contains(got, "Use the contract below.") {
		t.Errorf("balanced section lost its H3 body:\n%s", got)
	}
}

func TestPickSection_stopsAtNextH2(t *testing.T) {
	// The cautious section body must not leak into the Reference H2
	// that follows.
	got := pickSection(sampleProfile, "cautious")
	if strings.Contains(got, "Not a mode section") {
		t.Errorf("cautious section bled into Reference section:\n%s", got)
	}
}

func TestPickSection_unknownModeIsEmpty(t *testing.T) {
	if got := pickSection(sampleProfile, "bogus"); got != "" {
		t.Errorf("unknown mode returned non-empty:\n%s", got)
	}
}

func TestPickSection_blankModeIsEmpty(t *testing.T) {
	if got := pickSection(sampleProfile, ""); got != "" {
		t.Errorf("blank mode returned non-empty:\n%s", got)
	}
}

func TestPickSection_caseInsensitive(t *testing.T) {
	if got := pickSection(sampleProfile, "BALANCED"); !strings.Contains(got, "Stop for questions") {
		t.Errorf("case-insensitive match failed: %q", got)
	}
}

func TestSelectProfileSection_missingFileIsEmpty(t *testing.T) {
	if got := SelectProfileSection("/nonexistent.md", agents.InteractionBalanced); got != "" {
		t.Errorf("missing file should return empty; got %q", got)
	}
}

func TestSelectProfileSection_blankPathIsEmpty(t *testing.T) {
	if got := SelectProfileSection("", agents.InteractionBalanced); got != "" {
		t.Errorf("blank path should return empty; got %q", got)
	}
}

func TestSelectProfileSection_readsRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.md")
	if err := os.WriteFile(path, []byte(sampleProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	got := SelectProfileSection(path, agents.InteractionDangerous)
	if !strings.Contains(got, "Never ask. Never stop.") {
		t.Errorf("SelectProfileSection missing body:\n%s", got)
	}
}

func TestResolveProfilePath_agentOverrideAbs(t *testing.T) {
	if got := ResolveProfilePath("/repo", "/abs/override.md"); got != "/abs/override.md" {
		t.Errorf("abs override lost: %q", got)
	}
}

func TestResolveProfilePath_agentOverrideRelative(t *testing.T) {
	want := filepath.Join("/repo", "config/custom.md")
	if got := ResolveProfilePath("/repo", "config/custom.md"); got != want {
		t.Errorf("relative override: got %q want %q", got, want)
	}
}

func TestResolveProfilePath_defaultPresent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".gemba")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "interaction_profile.md")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveProfilePath(root, ""); got != p {
		t.Errorf("default path: got %q want %q", got, p)
	}
}

func TestResolveProfilePath_defaultMissing(t *testing.T) {
	root := t.TempDir()
	if got := ResolveProfilePath(root, ""); got != "" {
		t.Errorf("missing default should return empty; got %q", got)
	}
}
