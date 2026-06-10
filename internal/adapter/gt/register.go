package gt

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/adapter/registry"
)

func init() {
	registry.Register(registry.Adaptor{
		Name:   "gastown",
		Plane:  registry.OrchestrationPlane,
		Detect: detect,
		Probe:  probe,
	})
}

// detect: Gas Town is present when `gt` is on PATH AND an HQ is reachable
// from cwd. An HQ is identified by a .gt/ directory or a rigs/ directory
// (the two layouts gt itself accepts).
func detect() registry.DetectResult {
	if _, err := exec.LookPath("gt"); err != nil {
		return registry.DetectResult{
			Reason: "gt CLI not on PATH; install Gas Town 1.0",
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return registry.DetectResult{Reason: err.Error()}
	}
	for dir := cwd; ; {
		if isDir(filepath.Join(dir, ".gt")) || isDir(filepath.Join(dir, "rigs")) {
			return registry.DetectResult{Ok: true}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return registry.DetectResult{
				Reason: "no .gt/ or rigs/ directory in cwd or any ancestor",
			}
		}
		dir = parent
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// probeTimeout bounds how long we'll wait for `gt` to answer. Gas Town
// reads/writes beads through the shared Dolt server; the DoD in gm-b1
// explicitly calls out detecting Gas Town degradation, so the probe must
// catch Dolt hangs too.
var probeTimeout = 2 * time.Second

// probe: runtime health check used by /api/adaptors. `gt dolt status`
// is the documented way to check Gas Town's data-plane health (see the
// town CLAUDE.md "Dolt Server — Operational Awareness" section), so it's
// the right lever here. A failure means either gt itself is broken or
// the Dolt daemon is unreachable — both are "degraded" from the UI's
// perspective.
func probe() registry.DetectResult {
	if d := detect(); !d.Ok {
		return d
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gt", "dolt", "status")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return registry.DetectResult{
				Reason: "gt dolt status timed out; Dolt may be hung",
			}
		}
		trimmed := strings.TrimSpace(string(out))
		if trimmed == "" {
			trimmed = err.Error()
		}
		return registry.DetectResult{
			Reason: "gt probe failed: " + firstLine(trimmed),
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
