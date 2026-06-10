package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpecNew_Phase2_CreatesDocAndLockfile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	cmd := newSpecCmd()
	cmd.SetArgs([]string{"new", "demo-phase2"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	specPath := filepath.Join(dir, "docs", "specs", today+"-demo-phase2.md")
	lockPath := filepath.Join(dir, "docs", "specs", "."+today+"-demo-phase2.spec.lock.json")

	body, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	s := string(body)
	if !strings.HasPrefix(s, "---\n") {
		t.Errorf("missing frontmatter fence: %q", s[:min(60, len(s))])
	}
	if !strings.Contains(s, "spec_id: SP-"+today+"-demo-phase2") {
		t.Errorf("spec_id missing: %s", s)
	}
	if !strings.Contains(s, "nonce:") {
		t.Errorf("nonce missing")
	}
	if !strings.Contains(s, "## Goals") || !strings.Contains(s, "### S-01") {
		t.Errorf("template skeleton missing")
	}

	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	var lock map[string]any
	if err := json.Unmarshal(lockBytes, &lock); err != nil {
		t.Fatalf("lock json: %v", err)
	}
	if lock["nonce"] == nil || lock["nonce"] == "" {
		t.Errorf("lock nonce missing")
	}
	if mappings, _ := lock["mappings"].([]any); len(mappings) != 0 {
		t.Errorf("expected empty mappings, got %v", mappings)
	}
}

func TestSpecNew_Phase2_IdempotentWithoutForce(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	cmd := newSpecCmd()
	cmd.SetArgs([]string{"new", "dup"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	specPath := filepath.Join(dir, "docs", "specs", today+"-dup.md")
	firstBody, _ := os.ReadFile(specPath)

	// Second run without --force leaves the file untouched.
	cmd2 := newSpecCmd()
	cmd2.SetArgs([]string{"new", "dup"})
	out := &bytes.Buffer{}
	cmd2.SetOut(out)
	cmd2.SetErr(&bytes.Buffer{})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("idempotent re-run should not error: %v", err)
	}
	if !strings.Contains(out.String(), "exists") {
		t.Errorf("expected 'exists' notice, got: %s", out.String())
	}
	secondBody, _ := os.ReadFile(specPath)
	if string(firstBody) != string(secondBody) {
		t.Errorf("file changed on idempotent re-run")
	}

	// With --force, second run overwrites (new nonce).
	cmd3 := newSpecCmd()
	cmd3.SetArgs([]string{"new", "dup", "--force"})
	cmd3.SetOut(&bytes.Buffer{})
	cmd3.SetErr(&bytes.Buffer{})
	if err := cmd3.Execute(); err != nil {
		t.Fatalf("--force should overwrite: %v", err)
	}
	thirdBody, _ := os.ReadFile(specPath)
	if string(firstBody) == string(thirdBody) {
		t.Errorf("--force should regenerate nonce/file contents")
	}
}

func TestWritePhase2Spec_DirectAPI(t *testing.T) {
	dir := t.TempDir()
	fixed := time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC)
	specPath, lockPath, err := writePhase2Spec(dir, "alpha", fixed, false, "dev@example.com")
	if err != nil {
		t.Fatalf("writePhase2Spec: %v", err)
	}
	if !strings.HasSuffix(specPath, "2026-05-13-alpha.md") {
		t.Errorf("specPath: %s", specPath)
	}
	if !strings.HasSuffix(lockPath, ".2026-05-13-alpha.spec.lock.json") {
		t.Errorf("lockPath: %s", lockPath)
	}
	body, _ := os.ReadFile(specPath)
	if !strings.Contains(string(body), "owner: dev@example.com") {
		t.Errorf("owner not substituted: %s", body)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
