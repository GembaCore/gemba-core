// gm-e7.12 — gt CLI capability probe.
//
// At adaptor construction time we shell `gt --version` and a handful of
// subcommand --help outputs to:
//
//   - Pin the gt CLI version into the manifest's Extension map (so the
//     SPA / /api/adaptors caller can see exactly what we're talking to).
//   - WARN — not crash — when the version is below a known floor.
//   - Detect missing subcommands / flags up-front (gm-e7.9 found that
//     `gt sling` does not yet have --json; the probe records this so
//     downstream code can branch without re-reading help every call).
//
// The probe is best-effort: a single failed probe invocation degrades a
// single capability bit, never the entire adaptor. The one fatal case is
// `gt --version` itself failing (binary missing / unrunnable) — that
// returns a tagged KindAdaptorDegraded from New() so gemba surfaces the
// diagnostic at startup instead of cratering on first dispatch.

package gt

import (
	"context"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// minimumGtVersion is the floor we emit a WARN below. Currently 1.0.0 —
// the brewed binary as of 2026-04-29. Bump as new gt features become
// hard requirements (e.g. when /api/sessions starts depending on a
// flag only present in 1.1+).
const minimumGtVersion = "1.0.0"

// probeContextTimeout caps how long any single probe sub-shell can run.
// The probe runs synchronously at New(); a hung gt would otherwise stall
// gemba serve startup.
var probeContextTimeout = 3 * time.Second

// gtCapabilities is the cached result of probeCapabilities. Surfaced
// through Describe()'s Extension map and consulted by sessions.go's
// recycle path (gm-e7.13) and any future flag-gated codepaths.
type gtCapabilities struct {
	// Version is the parsed version string from `gt --version`
	// (e.g. "1.0.0"). Empty when the version line could not be parsed.
	Version string
	// VersionRaw is the full unparsed line for diagnostics.
	VersionRaw string
	// VersionBelowFloor is true when Version < minimumGtVersion. We
	// log a WARN at probe time but never refuse to construct.
	VersionBelowFloor bool
	// SupportsSlingJSON reflects whether `gt sling --help` advertises
	// a `--json` flag. gm-e7.9 worked around the absence; this lets
	// future code branch without re-probing.
	SupportsSlingJSON bool
	// HasScheduler reflects whether the `gt scheduler` subcommand is
	// present. Surfaced in metadata only — no internal codepath
	// gates on it today.
	HasScheduler bool
	// HasHandoff gates the gm-e7.13 RecycleSession path. When false
	// RecycleSession returns KindUnsupported with a specific
	// diagnostic instead of shelling a non-existent subcommand.
	HasHandoff bool
}

// probeRunner is the shell shape the probe uses. Tests inject a stub
// to avoid spawning real gt.
type probeRunner func(ctx context.Context, args ...string) ([]byte, error)

// probeCapabilities executes the capability probe. Returns the populated
// gtCapabilities and a non-nil error only when `gt --version` itself
// fails (the rest are best-effort and degrade quietly into the struct).
//
// The returned error, when non-nil, is already a tagged *core.AdaptorError
// (KindAdaptorDegraded) so callers can surface it directly.
func probeCapabilities(ctx context.Context, run probeRunner, log *slog.Logger) (gtCapabilities, error) {
	if log == nil {
		log = slog.Default()
	}
	caps := gtCapabilities{}

	verCtx, cancel := context.WithTimeout(ctx, probeContextTimeout)
	defer cancel()
	verOut, err := run(verCtx, "--version")
	if err != nil {
		return caps, wrapGtError(err, "gt --version")
	}
	caps.VersionRaw = strings.TrimSpace(string(verOut))
	caps.Version = parseGtVersion(caps.VersionRaw)
	if caps.Version == "" {
		log.Warn("gt: capability probe could not parse version line",
			"raw", caps.VersionRaw)
	} else if compareSemver(caps.Version, minimumGtVersion) < 0 {
		caps.VersionBelowFloor = true
		log.Warn("gt: detected version below floor; gemba expects a newer gt for full feature parity",
			"detected", caps.Version,
			"floor", minimumGtVersion)
	}

	// Subcommand probes — each tolerated. A missing subcommand or a
	// non-zero exit code degrades the cap bit to false rather than
	// failing the whole probe.
	caps.SupportsSlingJSON = probeFlagPresent(ctx, run, "--json", "sling", "--help")
	caps.HasScheduler = probeSubcommandExists(ctx, run, "scheduler")
	caps.HasHandoff = probeSubcommandExists(ctx, run, "handoff")

	if !caps.HasHandoff {
		log.Warn("gt: handoff subcommand not detected; RecycleSession will degrade to KindUnsupported")
	}
	if !caps.HasScheduler {
		log.Warn("gt: scheduler subcommand not detected; surfaced in adaptor metadata")
	}
	if !caps.SupportsSlingJSON {
		// Not surprising as of 2026-04-29; keep at Info volume so the
		// log isn't noisy.
		log.Info("gt: sling --json flag not advertised; adaptor will parse plain output")
	}

	return caps, nil
}

// probeSubcommandExists reports whether `gt <name> --help` returns
// successfully. False on any error (the subcommand is missing or
// help itself failed — both cases mean we can't rely on it).
func probeSubcommandExists(ctx context.Context, run probeRunner, name string) bool {
	subCtx, cancel := context.WithTimeout(ctx, probeContextTimeout)
	defer cancel()
	_, err := run(subCtx, name, "--help")
	return err == nil
}

// probeFlagPresent reports whether the given flag substring appears in
// `gt <args...>` output. Used to detect optional flags in `--help`
// pages without parsing their full structure.
func probeFlagPresent(ctx context.Context, run probeRunner, flag string, args ...string) bool {
	subCtx, cancel := context.WithTimeout(ctx, probeContextTimeout)
	defer cancel()
	out, err := run(subCtx, args...)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), flag)
}

// versionLineRE matches the leading semver in `gt version 1.0.0` or
// any common variant ("gt v1.0.0", "1.0.0\n…"). Pre-release and build
// metadata are tolerated but trimmed for compareSemver.
var versionLineRE = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// parseGtVersion extracts the semver portion from `gt --version` output.
// Returns "" when no semver-shaped substring is present.
func parseGtVersion(raw string) string {
	m := versionLineRE.FindStringSubmatch(raw)
	if len(m) != 4 {
		return ""
	}
	return m[1] + "." + m[2] + "." + m[3]
}

// compareSemver returns -1 / 0 / +1 the same way strings.Compare does,
// for the major.minor.patch portions of two semver strings. Handles
// "" gracefully (treated as "0.0.0"). Pre-release and build suffixes
// are ignored.
func compareSemver(a, b string) int {
	aMaj, aMin, aPat := splitSemver(a)
	bMaj, bMin, bPat := splitSemver(b)
	switch {
	case aMaj != bMaj:
		return cmpInt(aMaj, bMaj)
	case aMin != bMin:
		return cmpInt(aMin, bMin)
	default:
		return cmpInt(aPat, bPat)
	}
}

func splitSemver(s string) (int, int, int) {
	if s == "" {
		return 0, 0, 0
	}
	m := versionLineRE.FindStringSubmatch(s)
	if len(m) != 4 {
		return 0, 0, 0
	}
	return atoiOr0(m[1]), atoiOr0(m[2]), atoiOr0(m[3])
}

func atoiOr0(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// liveProbeRunner is the default probe shell — it spawns a real `gt`
// binary on PATH. Reused by NewOrchestrationPlane so production and
// tests share the same probe codepath.
func liveProbeRunner(path string) probeRunner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		// Use Output (not CombinedOutput) so probe captures stdout
		// only; --version writes there. Errors carry stderr via the
		// *exec.ExitError wrapping wrapGtError unwraps.
		return cmd.Output()
	}
}
