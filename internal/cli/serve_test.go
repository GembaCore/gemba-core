package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
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

// TestServe_ProjectDirRejectsMissingPath pins the --project-dir validation at
// the command layer (gm-dir): a non-existent path must fail before the
// HTTP server starts. The full validation matrix lives in
// internal/config.TestResolveBeadsDir; this test guards the CLI wiring
// (flag → ResolveBeadsDir → startup gate).
func TestServe_ProjectDirRejectsMissingPath(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--project-dir", "/definitely/not/a/real/path/gm-dir"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "project-dir") {
		t.Errorf("error missing expected text: %v", err)
	}
}

func TestServe_BeadsDirRemainsDeprecatedAlias(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	if cmd.Flags().Lookup("project-dir") == nil {
		t.Fatal("--project-dir flag missing from serve command")
	}
	f := cmd.Flags().Lookup("beads-dir")
	if f == nil {
		t.Fatal("--beads-dir compatibility alias missing from serve command")
	}
	if f.Deprecated == "" {
		t.Fatal("--beads-dir should be marked deprecated")
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

func TestServe_BeadsOnlyFlagsAccepted(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	for _, name := range []string{"beads-only", "beads-read-only", "beads-url", "beads-history", "restart"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("--%s flag missing from serve command", name)
		}
	}
}

func TestServeEnvDefaults_BeadsOnly(t *testing.T) {
	t.Setenv("GEMBA_MODE", "beads_only")
	t.Setenv("GEMBA_BEADS_URL", "mysql://root@127.0.0.1:3307/gemba")
	t.Setenv("GEMBA_BEADS_ONLY_MANIFEST", "/tmp/gemba-history.jsonl")
	t.Setenv("GEMBA_BEADS_READ_ONLY", "true")

	cfg := config.ServeConfig{}
	applyServeEnvDefaults(&cfg)
	normalizeServeMode(&cfg)

	if !cfg.BeadsOnly {
		t.Fatal("GEMBA_MODE=beads_only did not enable BeadsOnly")
	}
	if !cfg.BeadsReadOnly {
		t.Fatal("GEMBA_BEADS_READ_ONLY=true did not enable BeadsReadOnly")
	}
	if cfg.DoltURL != "mysql://root@127.0.0.1:3307/gemba" {
		t.Fatalf("DoltURL = %q", cfg.DoltURL)
	}
	if cfg.BeadsOnlyManifestPath != "/tmp/gemba-history.jsonl" {
		t.Fatalf("BeadsOnlyManifestPath = %q", cfg.BeadsOnlyManifestPath)
	}
	if shouldProbeBd(cfg) {
		t.Fatal("beads-only URL mode should not require bd")
	}
}

func TestBeadsOnlyProjectDirIsValidLocalMode(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(project, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.ServeConfig{ProjectDir: project, BeadsOnly: true}
	if err := normalizeProjectDirFlags(&cfg, true, false); err != nil {
		t.Fatalf("normalizeProjectDirFlags: %v", err)
	}
	normalizeServeMode(&cfg)
	if err := cfg.ValidateWorkPlaneFlags(); err != nil {
		t.Fatalf("ValidateWorkPlaneFlags: %v", err)
	}
	resolved, err := cfg.ResolveBeadsDir()
	if err != nil {
		t.Fatalf("ResolveBeadsDir: %v", err)
	}
	cfg.BeadsDir = resolved
	cfg.ProjectDir = resolved
	if !shouldProbeBd(cfg) {
		t.Fatal("beads-only project-dir mode should use the local bd adaptor")
	}
	if got := cfg.BeadsSource().Kind; got != "project-dir" {
		t.Fatalf("BeadsSource kind = %q, want project-dir", got)
	}
}

func TestNormalizeServeMode_ReadOnlyImpliesBeadsOnly(t *testing.T) {
	cfg := config.ServeConfig{BeadsReadOnly: true}
	normalizeServeMode(&cfg)
	if !cfg.BeadsOnly {
		t.Fatal("BeadsReadOnly must imply BeadsOnly")
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

// TestServe_RejectsBothProjectDirAndDoltURL pins the mutex between the
// two workplane selectors at the CLI layer. The rejection must
// happen before ResolveBeadsDir / NewWorkPlane fires so the operator
// gets a single, actionable error.
func TestServe_RejectsBothProjectDirAndDoltURL(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--project-dir", "/tmp",
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

// TestServe_TLSFlagsAccepted locks in the surface gm-e5.3 ships:
// --tls-cert, --tls-key, and --tls-self-signed must all be present on
// the serve command. The cert generation matrix lives in
// internal/auth.TestGenerateSelfSignedCert; this test just guards the
// flag wiring so an accidental rename doesn't silently strand the
// feature.
func TestServe_TLSFlagsAccepted(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	for _, name := range []string{"tls-cert", "tls-key", "tls-self-signed"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("--%s flag missing from serve command", name)
		}
	}
}

// TestServe_RejectsTLSCertWithoutKey pins the "cert and key inseparable"
// half of the TLS validator at the CLI layer. The rejection must
// happen before the listener opens so the operator sees the error
// before any partial setup.
func TestServe_RejectsTLSCertWithoutKey(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--tls-cert", "/tmp/c.pem", "--project-dir", "/tmp"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "must be set together") {
		t.Errorf("error missing expected text: %v", err)
	}
}

// TestServe_RejectsTLSSelfSignedWithCertKey pins the mutex between
// the operator-supplied path and the self-signed path. Both cannot be
// active simultaneously; the validator rejects before any cert work.
func TestServe_RejectsTLSSelfSignedWithCertKey(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--tls-self-signed",
		"--tls-cert", "/tmp/c.pem",
		"--tls-key", "/tmp/k.pem",
		"--project-dir", "/tmp",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error missing expected text: %v", err)
	}
}

// TestServe_NoopFlagStillAccepted is a non-regression check: --noop still
// works with no other adaptor flags after gm-root.19.
func TestServe_NoopFlagStillAccepted(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	if cmd.Flags().Lookup("noop") == nil {
		t.Fatal("--noop flag missing from serve command")
	}
}

// TestServe_NoopFlagAccepted pins the --noop flag's presence on the serve
// command (gm-e3.7). The reference adaptors themselves are exercised by
// the conformance harness; this test just guards the flag surface.
func TestServe_NoopFlagAccepted(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	if cmd.Flags().Lookup("noop") == nil {
		t.Fatal("--noop flag missing from serve command")
	}
}

// TestServe_NoopRejectsProjectDir pins the mutex between --noop and the
// real WorkPlane selectors. The noop adaptor is itself a complete
// WorkPlane — pairing it with --project-dir would force the server to
// pick one and silently ignore the other, so we reject up front.
func TestServe_NoopRejectsProjectDir(t *testing.T) {
	cmd := newServeCmd(BuildInfo{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--noop", "--project-dir", "/tmp"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "--noop is mutually exclusive") {
		t.Errorf("error must mention --noop mutex; got %q", err.Error())
	}
}

// TestPoolSizingDocExists is the gm-s47n.12 §3.3 "Documentation
// requirement" tripwire. The pool sizing doc must reference both
// MaxParallel and pool.size or operators won't find the
// cross-reference when the clamp surprises them.
func TestPoolSizingDocExists(t *testing.T) {
	body, err := os.ReadFile("../../docs/deployment/pool-sizing.md")
	if err != nil {
		t.Fatalf("docs/deployment/pool-sizing.md missing: %v", err)
	}
	s := string(body)
	for _, want := range []string{"MaxParallel", "pool.size", "reserved_for_manual"} {
		if !strings.Contains(s, want) {
			t.Errorf("pool-sizing.md missing reference to %q", want)
		}
	}
}

// TestPhase0ZeroDelta_NoPoolConfigYieldsNoDaemons confirms the
// gm-s47n.12 §11.1 contract: a server with no pool config
// constructs no daemons and behaves identically to today's main.
// The serve test suite is what the architect cares about for
// regression — this test is the explicit zero-delta tripwire.
func TestPhase0ZeroDelta_NoPoolConfigYieldsNoDaemons(t *testing.T) {
	cfg := config.ServeConfig{}
	resolved, _, err := loadAndResolvePools(cfg)
	if err != nil {
		t.Fatalf("zero-delta load: %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("Phase 0 zero-delta requires no daemons; got %d resolved pools", len(resolved))
	}
}
