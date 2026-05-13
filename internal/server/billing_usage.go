// gm-o9t8.4.2 — billing usage endpoint (Workstream B slice b).
//
// Surface (mounted on the shared /api/* block, behind apiAuth):
//
//	GET /api/v1/tenants/{tid}/usage — returns per-tenant usage counters
//
// Auth: same tenant-scoped check used by GET /api/v1/tenants/{tid} —
// the request's bearer-bound tenant must match the path tid. Cross-
// tenant reads return 403.
//
// The handler reads from the in-memory billing.Aggregator wired in via
// AttachUsageAggregator. When no aggregator is attached the endpoint
// returns 503 adaptor_not_configured.

package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/billing"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// AttachUsageAggregator wires the billing aggregator into the router.
// Nil disables the /usage endpoint (returns 503).
func (r *Router) AttachUsageAggregator(a *billing.Aggregator) {
	r.usageAggregator = a
}

// tenantUsage handles GET /api/v1/tenants/{tid}/usage.
func (r *Router) tenantUsage(w http.ResponseWriter, req *http.Request) {
	if r.usageAggregator == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"code":    "adaptor_not_configured",
				"message": "usage aggregator not attached",
			},
		})
		return
	}
	requested, err := tenant.ParseID(chi.URLParam(req, "tid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"code":    "invalid_tenant_id",
				"message": err.Error(),
			},
		})
		return
	}
	bearerTID, ok := tenant.FromContext(req.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"code": "no_tenant", "message": "unauthorized"},
		})
		return
	}
	if bearerTID != requested {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": map[string]string{
				"code":    "tenant_mismatch",
				"message": "bearer is not authorized for this tenant",
			},
		})
		return
	}
	snap := r.usageAggregator.Snapshot(string(requested))
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":     string(requested),
		"vm_minutes":    snap.VMMinutes,
		"egress_bytes":  snap.EgressBytes,
		"audit_log_gb":  snap.AuditLogGB,
	})
}
