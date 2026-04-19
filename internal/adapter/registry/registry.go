// Package registry holds the process-wide list of adaptors gemba can use.
//
// Each adaptor package registers itself via init(); the doctor command and
// the server read the list to decide which WorkPlane + OrchestrationPlane
// pair satisfies the current workspace.
package registry

import (
	"sort"
	"sync"
)

// Plane identifies which half of the two-plane architecture an adaptor
// lives on. Gemba requires exactly one of each at runtime.
type Plane string

const (
	WorkPlane          Plane = "work"
	OrchestrationPlane Plane = "orchestration"
)

// DetectResult is what an adaptor reports when asked whether the current
// workspace satisfies it. Ok=false with a human-readable Reason means
// "this adaptor is registered but the cwd can't use it."
type DetectResult struct {
	Ok     bool
	Reason string
}

// Adaptor is the minimum surface doctor needs. The full runtime interface
// (queries, mutations, capabilities) lands with later epics; this skeleton
// only captures identity + detection.
type Adaptor struct {
	Name   string
	Plane  Plane
	Detect func() DetectResult
}

var (
	mu       sync.RWMutex
	adaptors []Adaptor
)

// Register adds an adaptor to the process-wide list. Safe for concurrent
// use but expected to be called from package init() functions.
func Register(a Adaptor) {
	mu.Lock()
	defer mu.Unlock()
	adaptors = append(adaptors, a)
}

// List returns a copy of registered adaptors sorted by plane then name so
// doctor output is deterministic across runs.
func List() []Adaptor {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Adaptor, len(adaptors))
	copy(out, adaptors)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plane != out[j].Plane {
			return out[i].Plane < out[j].Plane
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Reset clears the registry. Intended for tests.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	adaptors = nil
}
