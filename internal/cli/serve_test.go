package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestServe_RejectsNonLoopbackWithoutAuth locks in the bind policy at the
// command layer: running `gemba serve --listen 0.0.0.0` without --auth must
// fail before the HTTP server is started. The detailed matrix lives in
// internal/config.TestValidateBindPolicy; this test guards the CLI wiring.
func TestServe_RejectsNonLoopbackWithoutAuth(t *testing.T) {
	cmd := newServeCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--listen", "0.0.0.0"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "non-loopback") {
		t.Errorf("error missing expected text: %v", err)
	}
}

// TestServe_ConfigFlagAccepted makes sure --config is a real flag so the
// gm-e1.2 output spec ([--config PATH]) doesn't silently regress.
func TestServe_ConfigFlagAccepted(t *testing.T) {
	cmd := newServeCmd()
	if cmd.Flags().Lookup("config") == nil {
		t.Fatal("--config flag missing from serve command")
	}
}

// TestServe_BeadsDirRejectsMissingPath pins the --beads-dir validation at
// the command layer (gm-dir): a non-existent path must fail before the
// HTTP server starts. The full validation matrix lives in
// internal/config.TestResolveBeadsDir; this test guards the CLI wiring
// (flag → ResolveBeadsDir → startup gate).
func TestServe_BeadsDirRejectsMissingPath(t *testing.T) {
	cmd := newServeCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--beads-dir", "/definitely/not/a/real/path/gm-dir"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "beads-dir") {
		t.Errorf("error missing expected text: %v", err)
	}
}
