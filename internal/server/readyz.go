package server

import (
	"net/http"
	"sync"
)

// DoltReadyChecker is the contract /api/readyz uses to ask whether the
// embedded dolt sql-server is reachable (gm-o9t8.1.2.3). Implemented by
// *supervisor.Supervisor; tests inject a stub.
type DoltReadyChecker interface {
	Ready() bool
}

// AttachDoltSupervisor wires a DoltReadyChecker into the router so the
// /api/readyz endpoint can reflect embedded-dolt health. Nil detaches.
//
// When no checker is attached, /api/readyz reports the server itself
// as ready and omits the dolt check from the response — that's the
// external-dolt and noop modes, where readyz can't make a meaningful
// claim about the upstream and shouldn't pretend to.
func (r *Router) AttachDoltSupervisor(s DoltReadyChecker) {
	r.doltMu.Lock()
	defer r.doltMu.Unlock()
	r.doltSupervisor = s
}

// doltReadyState holds the supervisor handle behind a mutex so attach
// races are well-defined. The Router struct embeds these fields; this
// file documents the contract in one place rather than burying it
// alongside 30 other Attach* helpers in router.go.
type doltReadyState struct {
	doltMu         sync.RWMutex
	doltSupervisor DoltReadyChecker
}

// readyz is the /api/readyz handler. Semantics:
//
//   - 200 with checks{dolt:"ok"} when a supervisor is attached and reports
//     Ready, or when no supervisor is attached at all (external-dolt mode).
//   - 503 with checks{dolt:"down"} when the supervisor is attached but
//     reports not-ready. Operators / orchestrators (k8s, the SPA's
//     "degraded" banner) use this to back off requests.
//
// readyz is distinct from /api/health — /health is a liveness probe
// that returns 200 as long as the Go process can serve HTTP, regardless
// of dependencies.
func (r *Router) readyz(w http.ResponseWriter, _ *http.Request) {
	r.doltMu.RLock()
	sup := r.doltSupervisor
	r.doltMu.RUnlock()

	resp := map[string]any{"status": "ok"}
	if sup == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if sup.Ready() {
		resp["checks"] = map[string]string{"dolt": "ok"}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp["status"] = "degraded"
	resp["checks"] = map[string]string{"dolt": "down"}
	writeJSON(w, http.StatusServiceUnavailable, resp)
}
