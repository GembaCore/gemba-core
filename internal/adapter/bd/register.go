package bd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/internal/adapter/registry"
)

func init() {
	registry.Register(registry.Adaptor{
		Name:   "beads",
		Plane:  registry.WorkPlane,
		Detect: detect,
		Probe:  probe,
	})
}

var (
	probeDirMu sync.RWMutex
	probeDir   string
)

// SetProbeDir installs the Beads worktree directory the registry health
// probe should use. The registry hooks run without direct access to
// serve-time configuration, so container and daemon runs must hand the
// configured --project-dir to the package after the workplane boots.
// Passing an empty string restores the legacy cwd/ancestor lookup.
func SetProbeDir(dir string) {
	probeDirMu.Lock()
	probeDir = dir
	probeDirMu.Unlock()
}

// detect reports ok when the `bd` CLI is installed AND the current working
// tree (or configured probe directory, or an ancestor) contains a .beads/
// directory. Both halves matter: without the binary we can't issue queries,
// and without the store there are no beads to read.
func detect() registry.DetectResult {
	if _, err := exec.LookPath("bd"); err != nil {
		return registry.DetectResult{
			Reason: "bd CLI not on PATH; install Beads " +
				"(https://github.com/steveyegge/beads)",
		}
	}
	cwd, err := effectiveProbeDir()
	if err != nil {
		return registry.DetectResult{Reason: err.Error()}
	}
	for dir := cwd; ; {
		if info, err := os.Stat(filepath.Join(dir, ".beads")); err == nil && info.IsDir() {
			return registry.DetectResult{Ok: true}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return registry.DetectResult{
				Reason: "no .beads/ directory in cwd or any ancestor",
			}
		}
		dir = parent
	}
}

func effectiveProbeDir() (string, error) {
	probeDirMu.RLock()
	dir := probeDir
	probeDirMu.RUnlock()
	if strings.TrimSpace(dir) == "" {
		return os.Getwd()
	}
	return filepath.Abs(dir)
}

const probeTimeoutEnv = "GEMBA_BD_PROBE_TIMEOUT"

// probeTimeout bounds how long we'll wait for `bd` to answer. The Dolt
// server bd talks to is the most common failure mode (gm-b1 DoD: killing
// the daemon must surface a banner, not a hang), so a short cap is the
// point — a slow reply is functionally the same as no reply for the UI.
var probeTimeout = 2 * time.Second

func effectiveProbeTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(probeTimeoutEnv))
	if raw == "" {
		return probeTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return probeTimeout
	}
	return d
}

// probe: runtime health check used by /api/adaptors. Runs a cheap `bd`
// subcommand that requires the Dolt daemon to answer, with a short
// timeout. Failure modes this catches:
//   - bd binary removed from PATH (Detect would also catch this)
//   - .beads/ removed (Detect would also catch this)
//   - Dolt server on :3307 down / unreachable (Detect would MISS this)
//   - Dolt hung past the timeout (Detect would MISS this)
//
// The last two are the whole reason Probe exists.
func probe() registry.DetectResult {
	// Cheap precheck: if Detect says no, Probe can't possibly pass.
	// Skips the subprocess spawn entirely when the workspace is broken.
	if d := detect(); !d.Ok {
		return d
	}

	ctx, cancel := context.WithTimeout(context.Background(), effectiveProbeTimeout())
	defer cancel()

	// `bd list --limit 1 --json` forces a round-trip to Dolt but asks for
	// almost no data. A daemon outage surfaces here as a non-zero exit
	// within a few hundred ms; a hung server is killed by the context
	// deadline.
	dir, _ := effectiveProbeDir()
	out, err := runBdCommand(ctx, "bd", dir, "list", "--limit", "1", "--json")
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return registry.DetectResult{
				Reason: "bd probe timed out; Dolt may be hung (see `gt dolt status`)",
			}
		}
		trimmed := strings.TrimSpace(string(out))
		if trimmed == "" {
			trimmed = err.Error()
		}
		return registry.DetectResult{
			Reason: "bd probe failed: " + firstLine(trimmed),
		}
	}
	return registry.DetectResult{Ok: true}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
