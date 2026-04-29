// startup_gates.go holds the early-startup probes that run before
// any HTTP listener is bound (gm-root.17.4, gm-root.19):
//
//  1. probeBd: verifies the bd CLI is installed. Missing → print install
//     instructions to stderr and return a non-nil error so the caller can
//     exit non-zero. This gate runs before any other initialization so the
//     operator gets an actionable message without waiting for port binding.
//
//  2. coldStartRedirect: determines whether the SPA root should redirect
//     to /new. Returns true when bd is present but no project exists under
//     the configured default_dir.
//
//  3. applyBeadsURLDefault: resolves the Beads server URL from config /
//     built-in default when neither --dolt-url nor --beads-dir was set
//     explicitly. Populates cfg.DoltURL so downstream validation and
//     adaptor construction can proceed without special-casing the
//     "no flags" scenario (gm-root.19).

package cli

import (
	"fmt"
	"io"
	"log/slog"
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
		_, _ = fmt.Fprint(stderr, bdInstallInstructions)
		return fmt.Errorf("bd CLI not found on PATH: install bd before running gemba serve")
	}
	// Run `bd --version` to confirm the binary is functional.
	// We use a simple exec.Command without a context because this is
	// a pre-startup gate — if bd hangs here something is deeply wrong
	// and the operator should Ctrl-C. A sub-second timeout would add
	// complexity without meaningful protection.
	cmd := exec.Command(path, "--version") //nolint:gosec // path is from LookPath
	if err := cmd.Run(); err != nil {
		_, _ = fmt.Fprint(stderr, bdInstallInstructions)
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

// applyBeadsURLDefault resolves the Beads server URL when the operator
// has not supplied explicit WorkPlane flags (gm-root.19). Resolution
// order:
//
//  1. --dolt-url already set in cfg — no-op; CLI wins.
//  2. --beads-dir already set in cfg — no-op; bd CLI path wins.
//  3. --noop already set — no-op; in-memory plane wins.
//  4. [beads].url in ~/.gemba/config.toml — populate cfg.DoltURL.
//  5. Built-in default (config.DefaultBeadsURL) — populate cfg.DoltURL.
//
// When the URL comes from config or the built-in default (cases 4/5),
// a diagnostic is logged so the operator knows which URL is being used
// and can correct it if wrong. The actual reachability probe happens
// downstream inside dolt.NewWorkPlane (which pings on connect); we do
// not open a connection here.
//
// If the config file is present but malformed, the error is returned so
// the operator sees it before any port binding happens.
func applyBeadsURLDefault(cfg *config.ServeConfig) error {
	// Cases 1–3: an explicit flag was given; nothing to do beyond
	// recording that the source is "cli". --noop has no Beads URL at
	// all — leave BeadsURLSource empty so the cold-start gate treats
	// it as a non-default-bound run.
	if cfg.DoltURL != "" || cfg.BeadsDir != "" {
		cfg.BeadsURLSource = "cli"
		return nil
	}
	if cfg.Noop {
		// Noop has no Beads URL; the cold-start gate keys off
		// BeadsURLSource=="default" so leaving this empty is correct.
		return nil
	}

	ucfg, err := config.LoadUserConfig(cfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("load user config for beads URL resolution: %w", err)
	}

	resolved := config.ResolveBeadsURL("", ucfg)
	cfg.DoltURL = resolved

	source := "built-in default"
	cfg.BeadsURLSource = "default"
	if ucfg.Beads.URL != "" {
		source = "~/.gemba/config.toml [beads].url"
		cfg.BeadsURLSource = "config"
	}
	slog.Info("beads URL resolved from "+source,
		"url", config.RedactBeadsURL(resolved),
		"hint", "pass --dolt-url or set [beads].url in ~/.gemba/config.toml to override")
	return nil
}

// coldStartShouldSkipBind decides whether the WorkPlane registration
// must be skipped to protect a fresh operator from silently binding a
// stray local Dolt "gemba" database (gm-ygwe). The answer is yes when
// BOTH:
//
//  1. the resolved Beads URL came from the built-in default (i.e.
//     applyBeadsURLDefault recorded BeadsURLSource=="default"), AND
//  2. no project marker (.gemba/workspace.toml) exists under the
//     configured [projects].default_dir.
//
// An explicit --dolt-url, an explicit --beads-dir, an explicit --noop,
// or a [beads].url operator override in ~/.gemba/config.toml all bypass
// this gate — the operator has stated their intent and we honor it.
//
// Errors loading the config or scanning the projects dir are returned
// so the caller can log them; on error this returns (false, err) — i.e.
// "do NOT skip the bind" — which preserves prior behaviour when a
// bad config file would otherwise block startup.
func coldStartShouldSkipBind(cfg config.ServeConfig) (bool, error) {
	if cfg.BeadsURLSource != "default" {
		return false, nil
	}
	ucfg, err := config.LoadUserConfig(cfg.ConfigPath)
	if err != nil {
		return false, fmt.Errorf("load user config: %w", err)
	}
	defaultDir, err := config.ResolveDefaultDir(ucfg)
	if err != nil {
		return false, fmt.Errorf("resolve default_dir: %w", err)
	}
	projects, err := config.ListProjectsUnder(defaultDir)
	if err != nil {
		return false, fmt.Errorf("scan default_dir %q: %w", defaultDir, err)
	}
	return len(projects) == 0, nil
}
