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
//
// Detect is a startup-time workspace check (is the CLI installed? is the
// .beads/ directory present?). It answers "can this adaptor ever run
// here?" and is invoked once by `gemba doctor` / server startup.
//
// Probe is a runtime health check ("can this adaptor answer a query right
// now?") used by the /api/adaptors endpoint to detect mid-session
// degradation — e.g., the bd Dolt daemon being killed. Probe implementations
// MUST bound themselves (short timeout, cheap query) so a degraded backend
// cannot stall the health surface. When Probe is nil the registry falls
// back to Detect, which gives a weaker signal but never blocks.
type Adaptor struct {
	Name   string
	Plane  Plane
	Detect func() DetectResult
	Probe  func() DetectResult
}

// AdaptorStatus is the JSON-serializable shape returned by /api/adaptors
// and consumed by the SPA banner. Healthy=false with a Reason is what
// the banner surfaces to the user. See gm-b1.
type AdaptorStatus struct {
	Name    string `json:"name"`
	Plane   Plane  `json:"plane"`
	Healthy bool   `json:"healthy"`
	Reason  string `json:"reason,omitempty"`
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

// Status runs each registered adaptor's Probe (or Detect as a fallback)
// and returns the current runtime health. Order matches List().
//
// Probe is preferred over Detect because Detect only answers "could this
// adaptor ever work here?" — it doesn't catch mid-session degradation like
// the bd Dolt daemon dying. Adaptors that want the banner to react to
// runtime failures MUST set Probe.
func Status() []AdaptorStatus {
	list := List()
	out := make([]AdaptorStatus, 0, len(list))
	for _, a := range list {
		probe := a.Probe
		if probe == nil {
			probe = a.Detect
		}
		s := AdaptorStatus{Name: a.Name, Plane: a.Plane}
		if probe == nil {
			s.Reason = "no probe configured"
			out = append(out, s)
			continue
		}
		res := probe()
		s.Healthy = res.Ok
		s.Reason = res.Reason
		out = append(out, s)
	}
	return out
}

// PlaneHealth reports the health of the first registered adaptor on the
// given plane. Gemba runs exactly one adaptor per plane, so "first" is
// unambiguous in production. Returns ok=false when no adaptor is
// registered for that plane. Used by the mutation middleware to reject
// writes against a degraded backend with a clear 503.
func PlaneHealth(plane Plane) (AdaptorStatus, bool) {
	for _, s := range Status() {
		if s.Plane == plane {
			return s, true
		}
	}
	return AdaptorStatus{}, false
}
