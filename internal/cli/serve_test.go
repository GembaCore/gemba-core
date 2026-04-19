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
