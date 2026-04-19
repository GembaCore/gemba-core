package bd

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
)

func init() {
	registry.Register(registry.Adaptor{
		Name:   "beads",
		Plane:  registry.WorkPlane,
		Detect: detect,
	})
}

// detect reports ok when the `bd` CLI is installed AND the current working
// tree (or an ancestor) contains a .beads/ directory. Both halves matter:
// without the binary we can't issue queries, and without the store there
// are no beads to read.
func detect() registry.DetectResult {
	if _, err := exec.LookPath("bd"); err != nil {
		return registry.DetectResult{
			Reason: "bd CLI not on PATH; install Beads " +
				"(https://github.com/steveyegge/beads)",
		}
	}
	cwd, err := os.Getwd()
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
