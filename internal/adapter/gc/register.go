package gc

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
)

func init() {
	registry.Register(registry.Adaptor{
		Name:   "gascity",
		Plane:  registry.OrchestrationPlane,
		Detect: detect,
	})
}

// detect: Gas City is present when the `gc` binary is on PATH AND a
// workspace is reachable from cwd. A workspace is identified by a .gc/
// directory or a city.toml file. The `gc` name is also shipped by
// graphviz; we tolerate the collision here because doctor also runs the
// workspace check, so graphviz-only systems will still fail to pair.
func detect() registry.DetectResult {
	if _, err := exec.LookPath("gc"); err != nil {
		return registry.DetectResult{
			Reason: "gc CLI not on PATH; Gas City is alpha — install at your own risk",
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return registry.DetectResult{Reason: err.Error()}
	}
	for dir := cwd; ; {
		if isDir(filepath.Join(dir, ".gc")) || isFile(filepath.Join(dir, "city.toml")) {
			return registry.DetectResult{Ok: true}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return registry.DetectResult{
				Reason: "no .gc/ or city.toml in cwd or any ancestor",
			}
		}
		dir = parent
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
