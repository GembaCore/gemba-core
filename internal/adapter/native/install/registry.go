package install

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is a process-wide map of Installer-by-Name. Installers
// register themselves at init time; callers resolve via Get or iterate
// via Names for help output.
var (
	registryMu sync.RWMutex
	registry   = map[string]Installer{}
)

// Register adds an Installer to the global registry. Panics on
// duplicate name to catch registration races at startup — every
// installer ships with a unique key anyway, and silent overrides are
// the worst kind of "I could've sworn this worked yesterday" bug.
func Register(inst Installer) {
	if inst == nil {
		panic("install.Register: nil installer")
	}
	name := inst.Name()
	if name == "" {
		panic("install.Register: installer with empty Name()")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("install.Register: duplicate installer name %q", name))
	}
	registry[name] = inst
}

// Get resolves an Installer by Name. Returns an error with the
// available names listed so operator typos produce actionable output.
func Get(name string) (Installer, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	inst, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("install: unknown installer %q (known: %v)", name, namesLocked())
	}
	return inst, nil
}

// Names returns every registered Installer name, sorted. Useful for
// shell-completion and `--help` output.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return namesLocked()
}

func namesLocked() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Reset clears the registry. Intended for tests that want a
// deterministic starting state.
func Reset() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]Installer{}
}
