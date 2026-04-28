// startup_gates.go holds the two early-startup probes that run before
// any HTTP listener is bound (gm-root.17.4):
//
//  1. probeBd: verifies the bd CLI is installed. Missing → print install
//     instructions to stderr and return a non-nil error so the caller can
//     exit non-zero. This gate runs before any other initialization so the
//     operator gets an actionable message without waiting for port binding.
//
//  2. coldStartRedirect: determines whether the SPA root should redirect
//     to /new. Returns true when bd is present but no project exists under
//     the configured default_dir.

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/MikeBengtson/gemba/internal/config"
)

// bdInstallInstructions is the message printed to stderr when bd is not found.
// Link and command are chosen to match the canonical install surface for bd.
const bdInstallInstructions = `
============================================================
  Gemba requires the bd (Beads) CLI but it was not found on
  PATH.

  Install bd:

    brew install beads-cli          # macOS / Linux via Homebrew
    pipx install beads-cli          # Python-managed install

  Full install docs:
    https://github.com/MikeBengtson/beads/blob/main/docs/install.md

  Once installed, re-run: gemba serve
============================================================
`

// probeBd probes for the bd CLI by running `bd --version`.
// If bd is not on PATH or the probe fails, it writes install
// instructions to stderr and returns a non-nil error. On success
// it returns nil and the caller may proceed.
//
// stderr is a parameter so tests can capture the output without
// polluting os.Stderr.
func probeBd(stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	path, err := exec.LookPath("bd")
	if err != nil {
		fmt.Fprint(stderr, bdInstallInstructions)
		return fmt.Errorf("bd CLI not found on PATH: install bd before running gemba serve")
	}
	// Run `bd --version` to confirm the binary is functional.
	// We use a simple exec.Command without a context because this is
	// a pre-startup gate — if bd hangs here something is deeply wrong
	// and the operator should Ctrl-C. A sub-second timeout would add
	// complexity without meaningful protection.
	cmd := exec.Command(path, "--version") //nolint:gosec // path is from LookPath
	if err := cmd.Run(); err != nil {
		fmt.Fprint(stderr, bdInstallInstructions)
		return fmt.Errorf("bd --version failed (%w): install bd before running gemba serve", err)
	}
	return nil
}

// coldStartRedirect decides whether the SPA root should redirect to /new.
// It loads the user config (respecting the ConfigPath override in cfg),
// resolves the default_dir, and checks whether any project exists there.
//
// Returns (true, nil)  — redirect; bd present, no project found.
// Returns (false, nil) — no redirect; at least one project found.
// A non-nil error indicates a config I/O problem that is non-fatal:
// callers log it and treat the result as false (no redirect) so a bad
// config file does not block serve startup.
func coldStartRedirect(cfg config.ServeConfig) (bool, error) {
	ucfg, err := config.LoadUserConfig(cfg.ConfigPath)
	if err != nil {
		return false, fmt.Errorf("load user config: %w", err)
	}
	defaultDir, err := config.ResolveDefaultDir(ucfg)
	if err != nil {
		return false, fmt.Errorf("resolve default_dir: %w", err)
	}
	hasProject, err := config.HasProjectUnder(defaultDir)
	if err != nil {
		// Non-fatal: a permissions error on the projects dir should not
		// prevent serve from starting. Log but don't redirect.
		return false, fmt.Errorf("scan default_dir %q: %w", defaultDir, err)
	}
	return !hasProject, nil
}
