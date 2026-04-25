package server

import (
	"net/http"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	"github.com/MikeBengtson/gemba/internal/transport"
)

// adaptorsHealth is the /api/adaptors handler. The SPA polls this every
// few seconds (see web/src/components/AdaptorBanner.tsx) to decide
// whether to surface the "adaptor degraded" banner. gm-b1.
//
// Response shape is stable:
//
//	{
//	  "adaptors": [
//	    {"name": "beads", "plane": "work", "healthy": true},
//	    {"name": "gastown", "plane": "orchestration", "healthy": false,
//	     "reason": "bd probe timed out; Dolt may be hung"}
//	  ]
//	}
//
// Only adaptors actively bound to a plane via host.Register* are
// returned — the registry as a whole carries every adaptor that
// self-registered in init() (used by `gemba doctor` for discovery),
// but the banner cares about runtime, not discovery. Without this
// filter the banner would scold users about uninstalled optional
// orchestrators or the sibling work adaptor they didn't pick.
//
// The endpoint itself MUST never fail — a degraded backend is a banner,
// not a 500.
//
// gm-root.7: reads from the HealthBus cache when the bus has been
// started (the common production path); falls through to a live probe
// otherwise so unit tests that skip StartHealthBus still get the
// expected wire shape.
func (r *Router) adaptorsHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"instance_id": r.instanceID,
		"adaptors":    r.snapshotAdaptorStatuses(),
	})
}

// snapshotAdaptorStatuses returns the current cached status if the bus
// is running; otherwise runs the bound-adaptor probe directly. The bus
// itself also falls back to source() when its cache is empty, so this
// helper could just be r.healthBus.Snapshot() — the explicit nil check
// exists because some test harnesses construct Router with a zeroed
// struct bypassing NewRouter.
func (r *Router) snapshotAdaptorStatuses() []registry.AdaptorStatus {
	if r.healthBus != nil {
		return r.healthBus.Snapshot()
	}
	return r.boundAdaptorStatuses()
}

// boundAdaptorStatuses returns the registry-probed status for the
// adaptor names actually bound on the API host. When the host is nil
// (test setups that pre-date host wiring), falls through to the full
// registry to keep the legacy behaviour.
func (r *Router) boundAdaptorStatuses() []registry.AdaptorStatus {
	all := registry.Status()
	if r.host == nil {
		return all
	}
	allowed := map[string]registry.Plane{}
	for _, reg := range r.host.Registrations() {
		switch reg.Plane {
		case transport.PlaneWork:
			allowed[reg.AdaptorName] = registry.WorkPlane
		case transport.PlaneOrchestration:
			allowed[reg.AdaptorName] = registry.OrchestrationPlane
		}
	}
	if len(allowed) == 0 {
		// Nothing bound yet — keep returning the whole registry so an
		// operator who hits /api/adaptors during the bind window still
		// sees something useful (e.g., "beads-dolt: not configured").
		return all
	}
	out := make([]registry.AdaptorStatus, 0, len(allowed))
	for _, s := range all {
		if want, ok := allowed[s.Name]; ok && want == s.Plane {
			out = append(out, s)
		}
	}
	return out
}

// requireHealthyAdaptor is the mutation gate (gm-b1 output #3). Wrap any
// handler that writes to an adaptor with requireHealthyAdaptor(plane) so
// a degraded backend returns a structured 503 instead of hanging the
// request for 30s and then failing opaquely.
//
// Usage (for future mutation routes):
//
//	api.With(requireHealthyAdaptor(registry.WorkPlane)).
//	    Post("/beads", createBead)
//
// Response envelope on rejection:
//
//	503 {"error": "adaptor_degraded", "plane": "work", "adaptor": "beads",
//	     "reason": "bd probe timed out; Dolt may be hung"}
//
// Callers get enough detail to surface an actionable error in the UI
// without guessing at the cause.
func requireHealthyAdaptor(plane registry.Plane) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			status, ok := registry.PlaneHealth(plane)
			if !ok {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{
					"error":  "adaptor_not_configured",
					"plane":  plane,
					"reason": "no adaptor registered for this plane",
				})
				return
			}
			if !status.Healthy {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{
					"error":   "adaptor_degraded",
					"plane":   plane,
					"adaptor": status.Name,
					"reason":  status.Reason,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
