package promote

import (
	"context"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/spec/parser"
)

const baseSpec = `---
spec_id: SP-001
title: Demo Spec
version: "0.1"
created: 2026-05-13T00:00:00Z
owner: dev@example.com
decision_parent: gm-o9t8
constitution_path: .gemba/constitution.md
nonce: 11111111-2222-3333-4444-555555555555
---

## Goals

Ship the foundation.

## Decisions

### D-01 Use Go
Author: alice@example.com
Date: 2026-05-01

We chose Go.

## Milestones

### M-01 Foundations

Body of M-01.
`

type fakeLister struct {
	mails []Candidate
	label string
}

func (f *fakeLister) List(ctx context.Context, label string) ([]Candidate, error) {
	f.label = label
	return f.mails, nil
}

func TestDiscover_FiltersBySlug(t *testing.T) {
	f := &fakeLister{
		mails: []Candidate{
			{MailID: "m1", Title: "Promote auth model", Author: "a@x", Timestamp: "2026-05-10T10:00:00Z", Body: "Use JWT."},
		},
	}
	cs, err := Discover(context.Background(), "demo-spec", f)
	if err != nil {
		t.Fatal(err)
	}
	if f.label != "spec-decision:demo-spec" {
		t.Errorf("label = %q", f.label)
	}
	if len(cs) != 1 || cs[0].SpecSlug != "demo-spec" {
		t.Errorf("candidates = %+v", cs)
	}
}

func TestNextDecisionNumber(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "01"},
		{"### D-01 foo\n", "02"},
		{"### D-01 foo\n### D-09 bar\n", "10"},
		{"### D-99 foo\n", "100"},
		{"random\n### D-03 baz\nbody\n### D-07 zog\n", "08"},
	}
	for _, c := range cases {
		got := NextDecisionNumber(c.in)
		if got != c.want {
			t.Errorf("NextDecisionNumber(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPropose_RoundTrip(t *testing.T) {
	spec, err := parser.Parse([]byte(baseSpec))
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	c := Candidate{
		MailID:    "m-42",
		SpecSlug:  "demo",
		Title:     "Adopt Cobra for CLI",
		Body:      "We will use Cobra because it is widely adopted.\nAlt: stdlib flag.",
		Author:    "bob@example.com",
		Timestamp: "2026-05-12T14:30:00Z",
	}
	diff, err := Propose("specs/demo/spec.md", []byte(baseSpec), spec, c)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if !strings.Contains(diff, "--- a/specs/demo/spec.md") {
		t.Errorf("diff missing a/ header:\n%s", diff)
	}
	if !strings.Contains(diff, "+### D-02 Adopt Cobra for CLI") {
		t.Errorf("diff missing new entry header:\n%s", diff)
	}
	if !strings.Contains(diff, "+Date: 2026-05-12") {
		t.Errorf("diff missing date:\n%s", diff)
	}

	// Apply the diff and re-parse.
	updated, err := applyDiffForTest(baseSpec, diff)
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, diff)
	}
	parsed, err := parser.Parse([]byte(updated))
	if err != nil {
		t.Fatalf("re-parse updated spec failed: %v\n--- updated:\n%s", err, updated)
	}
	if !strings.Contains(parsed.Decisions, "D-02 Adopt Cobra for CLI") {
		t.Errorf("updated Decisions section missing new entry: %q", parsed.Decisions)
	}
	if !strings.Contains(parsed.Decisions, "D-01 Use Go") {
		t.Errorf("original D-01 missing after apply: %q", parsed.Decisions)
	}
	// Milestones must remain intact.
	if len(parsed.Milestones) != 1 || parsed.Milestones[0].Anchor != "M-01" {
		t.Errorf("milestones disturbed: %+v", parsed.Milestones)
	}
}

func TestPropose_NoDecisionsSection(t *testing.T) {
	src := `---
spec_id: SP-X
nonce: n1
---

## Goals

Ship it.
`
	spec, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := Candidate{MailID: "m1", Title: "First decision", Author: "a@x", Timestamp: "2026-05-13T00:00:00Z", Body: "Pick X."}
	diff, err := Propose("spec.md", []byte(src), spec, c)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := applyDiffForTest(src, diff)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(updated, "## Decisions") {
		t.Errorf("missing new Decisions section:\n%s", updated)
	}
	if !strings.Contains(updated, "### D-01 First decision") {
		t.Errorf("missing D-01 entry:\n%s", updated)
	}
	if _, err := parser.Parse([]byte(updated)); err != nil {
		t.Fatalf("re-parse: %v\n%s", err, updated)
	}
}

func TestFormatEntry_TimestampParsing(t *testing.T) {
	out := FormatEntry("03", "Title", "x@y", "2026-05-12T14:30:00Z", "body")
	if !strings.Contains(out, "Date: 2026-05-12") {
		t.Errorf("expected date conversion, got: %s", out)
	}
	out2 := FormatEntry("03", "Title", "x@y", "not-a-date", "body")
	if !strings.Contains(out2, "Date: not-a-date") {
		t.Errorf("expected passthrough, got: %s", out2)
	}
}
