package parser

import (
	"errors"
	"strings"
	"testing"
)

const wellFormed = `---
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

Use Go.

## Milestones

### M-01 Foundations

Body of M-01.

## Epics

### E-01 Parser

Body of E-01.

## Stories

### S-01 Define schema

Status: open
Parent: E-01
Priority: P1

Some body text.

AC:
- thing one
- thing two

### S-02 Lockfile io

Status: open
Parent: E-01
- AC: atomic write
- AC: rejects missing nonce
`

func TestParse_WellFormed(t *testing.T) {
	spec, err := Parse([]byte(wellFormed))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if spec.Frontmatter.SpecID != "SP-001" {
		t.Errorf("spec_id = %q", spec.Frontmatter.SpecID)
	}
	if spec.Frontmatter.Nonce == "" {
		t.Errorf("nonce empty")
	}
	if !strings.Contains(spec.Goals, "Ship the foundation") {
		t.Errorf("goals: %q", spec.Goals)
	}
	if len(spec.Milestones) != 1 || spec.Milestones[0].Anchor != "M-01" {
		t.Errorf("milestones: %+v", spec.Milestones)
	}
	if len(spec.Epics) != 1 || spec.Epics[0].Anchor != "E-01" {
		t.Errorf("epics: %+v", spec.Epics)
	}
	if len(spec.Stories) != 2 {
		t.Fatalf("want 2 stories, got %d", len(spec.Stories))
	}
	s1 := spec.Stories[0]
	if s1.Anchor != "S-01" || s1.Status != "open" || s1.Parent != "E-01" || s1.Priority != "P1" {
		t.Errorf("S-01 fields: %+v", s1)
	}
	if len(s1.AC) != 2 || s1.AC[0] != "thing one" || s1.AC[1] != "thing two" {
		t.Errorf("S-01 AC: %+v", s1.AC)
	}
	s2 := spec.Stories[1]
	if len(s2.AC) != 2 || s2.AC[0] != "atomic write" {
		t.Errorf("S-02 AC: %+v", s2.AC)
	}
}

func TestParse_MissingFrontmatter(t *testing.T) {
	_, err := Parse([]byte("## Goals\n\nno frontmatter here\n"))
	if !errors.Is(err, ErrMissingFrontmatter) {
		t.Fatalf("want ErrMissingFrontmatter, got %v", err)
	}
}

func TestParse_MissingNonce(t *testing.T) {
	src := `---
spec_id: SP-X
title: t
---

## Goals
`
	_, err := Parse([]byte(src))
	if !errors.Is(err, ErrMissingNonce) {
		t.Fatalf("want ErrMissingNonce, got %v", err)
	}
}

func TestParse_DuplicateAnchor(t *testing.T) {
	src := `---
spec_id: SP-X
nonce: n
---

## Stories

### S-01 First

Status: open
Parent: E-01

### S-01 Second

Status: open
Parent: E-01
`
	_, err := Parse([]byte(src))
	if !errors.Is(err, ErrDuplicateAnchor) {
		t.Fatalf("want ErrDuplicateAnchor, got %v", err)
	}
}

func TestParse_MalformedAC(t *testing.T) {
	src := `---
spec_id: SP-X
nonce: n
---

## Stories

### S-01 Bad

Status: open
Parent: E-01

AC:
-
`
	_, err := Parse([]byte(src))
	if !errors.Is(err, ErrMalformedAC) {
		t.Fatalf("want ErrMalformedAC, got %v", err)
	}
}

func TestParse_MalformedAC_EmptyBlock(t *testing.T) {
	src := `---
spec_id: SP-X
nonce: n
---

## Stories

### S-01 Bad

Status: open
Parent: E-01

AC:
`
	_, err := Parse([]byte(src))
	if !errors.Is(err, ErrMalformedAC) {
		t.Fatalf("want ErrMalformedAC, got %v", err)
	}
}
