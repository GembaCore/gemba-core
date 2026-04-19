package gt

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
)

func init() {
	registry.Register(registry.Adaptor{
		Name:   "gastown",
		Plane:  registry.OrchestrationPlane,
		Detect: detect,
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
