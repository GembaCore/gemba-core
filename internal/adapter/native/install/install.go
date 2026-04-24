// Package install is the per-agent install-strategy registry that
// drives gemba install-bridge and the native adaptor's eager install on
// StartSession (gm-native.18).
//
// Why a strategy pattern instead of inline-per-CLI-command: the bridge
// needs to land in every agent the operator runs, and each agent
// arranges its hook + skill artifacts differently. An Installer knows
// what files to scan for, how to merge vs create them, and which
// default files to copy when the worktree has none of its own. Adding
// a new agent type is a drop-in: implement Installer, Register in init.
//
// Every Installer must be SAFE-BY-DEFAULT: never clobber
// operator-authored bytes outside a gemba-owned sentinel block. Prefer
// scan-first, merge-second, default-third — so running the installer
// on a populated worktree is a supported no-op rather than a risky
// overwrite.

package install

import "context"

// Action describes one filesystem change (or non-change) an Installer
// intends to make. The Installer records an Action for every file
// it inspected, including "skipped because file existed and we never
// clobber operator content". Plural output is deliberate: operators
// reading `gemba install-bridge --dry-run` want a ledger, not a
// single-line "ok".
type Action struct {
	// Kind enumerates the outcome:
	//   "created"   — file did not exist, we wrote our default
	//   "merged"    — file existed, we added our sentinel-scoped block
	//   "skipped"   — file existed + sentinel present; idempotent no-op
	//   "preserved" — file existed, no sentinel; left untouched
	//                 (operator owns it; we never overwrite)
	Kind string
	// Path is absolute when possible; the Installer resolves against
	// the workspace Options.Dir so tests using t.TempDir() see sane
	// relative paths.
	Path string
	// Reason is a one-line human-readable explanation. Stable enough
	// that tests can assert on substrings.
	Reason string
}

// Report bundles the per-file ledger with a top-level summary line.
// Installer.Install always returns a Report even on error so callers
// can show the operator what was attempted before the failure.
type Report struct {
	Agent   string
	Actions []Action
}

// Options carries the install request. Fields are optional; zero-
// values are valid and mean "figure it out from defaults."
type Options struct {
	// Dir is the workspace the installer writes into. Required.
	Dir string
	// DryRun, when true, makes Install describe what it would do
	// without touching the filesystem. Every Action is still recorded.
	DryRun bool
}

// Installer is the per-agent strategy. One implementation per supported
// agent type (claude, shell_only, …); a Registry holds them all and
// resolves by Name.
type Installer interface {
	// Name is the registry key and matches the values an operator
	// types at the `--agent` flag. Stable, lowercase, no spaces.
	Name() string
	// Install performs the strategy against opts.Dir and returns a
	// Report describing what changed. Errors are reserved for
	// situations the strategy cannot continue from (bad perms,
	// unparseable existing JSON); "file already exists, we left it
	// alone" is an Action, not an error.
	Install(ctx context.Context, opts Options) (Report, error)
}
