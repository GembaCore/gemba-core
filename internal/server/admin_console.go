// gm-o9t8.4.3 — operator console endpoints (Workstream B slice b).
//
// Surface (mounted on the shared /api block, behind apiAuth + admin):
//
//	GET  /api/v1/admin/tenants          — list every tenant (paginated)
//	GET  /api/v1/admin/sessions         — list active agent sessions
//	                                      across all tenants
//	POST /api/v1/admin/audit/verify     — replay the HMAC/Ed25519 chain
//	                                      over the audit log
//
// Auth: the routes mount behind a dedicated requireAdmin middleware
// that checks GEMBA_ADMIN_EMAILS (or a router-attached email list);
// the standard apiAuth bearer/cookie gate runs first. When the admin
// email list is empty admin auth is OPEN — single-user installs and
// loopback dev rigs work without extra config. Production deployments
// MUST set GEMBA_ADMIN_EMAILS.
//
// The session listing pulls from an injected lister so the operator
// console can be wired against either the OrchestrationPlane (today)
// or the multi-plane fleet aggregator (future). Tests substitute a
// trivial in-memory list.

package server

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/server/audit"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// adminEmailHeader is the request header the v1 stub reads to identify
// the operator. cmd/gemba serve runs behind a reverse proxy that
// stamps the verified email here after OAuth; loopback deployments
// can set it manually for testing.
const adminEmailHeader = "X-GEMBA-Admin-Email"

// AdminSession is the wire shape for the operator console's session
// inventory. Mirrors core.Session but kept narrow so the console UI
// gets a stable contract independent of the agent-plane internals.
type AdminSession struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Workspace string    `json:"workspace,omitempty"`
	AgentID   string    `json:"agent_id,omitempty"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at,omitempty"`
}

// AdminSessionLister is the contract the admin sessions endpoint
// depends on. Production wiring builds one over the OrchestrationPlane;
// tests inject a static list.
type AdminSessionLister func(ctx context.Context) ([]AdminSession, error)

// AttachAdminConsole wires the admin-console seams. Any nil argument
// leaves the prior value in place so callers can wire pieces
// incrementally.
func (r *Router) AttachAdminConsole(
	emails []string,
	sessionLister AdminSessionLister,
	auditPubKey ed25519.PublicKey,
	auditRecords func(ctx context.Context) ([]audit.Record, error),
) {
	if emails != nil {
		r.adminEmails = emails
	}
	if sessionLister != nil {
		r.adminSessionLister = sessionLister
	}
	if auditPubKey != nil {
		r.adminAuditPubKey = auditPubKey
	}
	if auditRecords != nil {
		r.adminAuditRecords = auditRecords
	}
}

// requireAdmin is the middleware that gates /api/v1/admin/* routes.
// The check runs after the shared apiAuth bearer/cookie gate. When the
// admin allow-list is empty the middleware is permissive — single-user
// loopback installs need this to not lock themselves out.
func (r *Router) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		emails := r.adminEmails
		if len(emails) == 0 {
			// fall back to the env so production can configure without
			// touching code wiring.
			if v := strings.TrimSpace(os.Getenv("GEMBA_ADMIN_EMAILS")); v != "" {
				emails = splitAndTrim(v, ",")
			}
		}
		if len(emails) == 0 {
			next.ServeHTTP(w, req)
			return
		}
		got := strings.ToLower(strings.TrimSpace(req.Header.Get(adminEmailHeader)))
		if got == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": map[string]string{
					"code":    "admin_required",
					"message": "missing X-GEMBA-Admin-Email header",
				},
			})
			return
		}
		for _, want := range emails {
			if strings.EqualFold(strings.TrimSpace(want), got) {
				next.ServeHTTP(w, req)
				return
			}
		}
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": map[string]string{
				"code":    "admin_forbidden",
				"message": "email is not on the admin allow-list",
			},
		})
	})
}

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// adminListTenants handles GET /api/v1/admin/tenants. Mirrors the
// existing per-bearer listTenants but always lists across the whole
// tenancy table — no tenant-scoping. Pagination follows the same
// ?limit / ?after envelope.
func (r *Router) adminListTenants(w http.ResponseWriter, req *http.Request) {
	if r.tenantStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"code": "adaptor_not_configured", "message": "tenant store not attached"},
		})
		return
	}
	q := req.URL.Query()
	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"code": "bad_request", "message": "limit must be non-negative integer"},
			})
			return
		}
		limit = n
	}
	var after tenant.ID
	if v := q.Get("after"); v != "" {
		id, err := tenant.ParseID(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"code": "bad_request", "message": "after must be a valid tenant id"},
			})
			return
		}
		after = id
	}
	tenants, err := r.tenantStore.List(req.Context(), tenant.ListOptions{Limit: limit, After: after})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "store_error", "message": err.Error()},
		})
		return
	}
	rows := make([]tenantResponse, 0, len(tenants))
	for _, t := range tenants {
		rows = append(rows, marshalTenant(t))
	}
	resp := map[string]any{"tenants": rows}
	if limit > 0 && len(tenants) == limit {
		resp["next_after"] = string(tenants[len(tenants)-1].ID)
	}
	writeJSON(w, http.StatusOK, resp)
}

// adminListSessions handles GET /api/v1/admin/sessions. Delegates to
// the injected AdminSessionLister; when none is wired returns an
// empty list (mirrors the listSessions behaviour for "no plane").
func (r *Router) adminListSessions(w http.ResponseWriter, req *http.Request) {
	if r.adminSessionLister == nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []AdminSession{}})
		return
	}
	sessions, err := r.adminSessionLister(req.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "session_list_failed", "message": err.Error()},
		})
		return
	}
	if sessions == nil {
		sessions = []AdminSession{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// adminAuditVerify handles POST /api/v1/admin/audit/verify. Pulls
// every audit record via the wired loader and runs audit.Verify
// against the configured public key. Returns the seq of the first
// broken record on failure; 200 OK with {ok:true, count:N} on success.
func (r *Router) adminAuditVerify(w http.ResponseWriter, req *http.Request) {
	if len(r.adminAuditPubKey) == 0 || r.adminAuditRecords == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"code":    "adaptor_not_configured",
				"message": "audit verify dependencies not attached",
			},
		})
		return
	}
	records, err := r.adminAuditRecords(req.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "audit_read_failed", "message": err.Error()},
		})
		return
	}
	if err := audit.Verify(records, ed25519.PublicKey(r.adminAuditPubKey)); err != nil {
		// audit.Verify embeds the offending seq in its error message;
		// also try to recover the int when present.
		broken := firstBrokenSeq(records, ed25519.PublicKey(r.adminAuditPubKey))
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":          false,
			"count":       len(records),
			"error":       err.Error(),
			"broken_seq":  broken,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"count": len(records),
	})
}

// firstBrokenSeq runs Verify one record at a time to surface the seq
// of the first failing entry as an int. Returns 0 when the chain
// passes (Verify already returned nil) or when no records are present.
func firstBrokenSeq(records []audit.Record, pub ed25519.PublicKey) int64 {
	for i := 1; i <= len(records); i++ {
		if err := audit.Verify(records[:i], pub); err != nil {
			return records[i-1].Seq
		}
	}
	return 0
}
