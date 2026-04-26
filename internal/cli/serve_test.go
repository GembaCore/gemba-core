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
	cmd := newServeCmd(BuildInfo{})
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
	cmd := newServeCmd(BuildInfo{})
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
	cmd := newServeCmd(BuildInfo{})
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

// TestServe_DoltURLFlagAccepted locks in --dolt-url's presence on the
// serve command (gm-0fd). The parse/connect matrix lives in
// internal/adapter/dolt; this test just guards the flag surface.
func TestServe_DoltURLFlagAccepted(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	if cmd.Flags().Lookup("dolt-url") == nil {
		t.Fatal("--dolt-url flag missing from serve command")
	}
}

// TestServe_OrchestrationDefaultsToNative pins the flag default. A
// fresh `gemba serve` MUST register the native orchestration plane
// so /coach + /api/operational-context return data without the
// operator hunting for an --orchestration flag. Pass --orchestration=
// or =none to opt out.
func TestServe_OrchestrationDefaultsToNative(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	f := cmd.Flags().Lookup("orchestration")
	if f == nil {
		t.Fatal("--orchestration flag missing from serve command")
	}
	if got := f.DefValue; got != "native" {
		t.Errorf("orchestration default = %q, want %q", got, "native")
	}
}

// TestServe_RejectsBothBeadsDirAndDoltURL pins the mutex between the
// two workplane selectors at the CLI layer. The rejection must
// happen before ResolveBeadsDir / NewWorkPlane fires so the operator
// gets a single, actionable error.
func TestServe_RejectsBothBeadsDirAndDoltURL(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--beads-dir", "/tmp",
		"--dolt-url", "mysql://root@127.0.0.1:3307/gemba",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error must mention mutual exclusion; got %q", err.Error())
	}
}

// TestServe_RejectsNeitherBeadsDirNorDoltURL pins the "must pick one"
// half of the WorkPlane selector contract at the CLI layer. Running
// `gemba serve` with no adaptor flag has no WorkPlane to expose, so
// the rejection must happen before the listener opens and must point
// the operator at both flag names.
func TestServe_RejectsNeitherBeadsDirNorDoltURL(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "no WorkPlane selected") {
		t.Errorf("error must name the condition; got %q", err.Error())
	}
	for _, want := range []string{"--beads-dir", "--dolt-url"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q; got %q", want, err.Error())
		}
	}
}
