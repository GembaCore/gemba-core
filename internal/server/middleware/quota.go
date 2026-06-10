// gm-o9t8.4.2.1 — tier-aware quota + rate-limit middleware.
//
// WithQuota composes after WithTenant in the chain. For each request it:
//
//  1. Reads the bearer-bound tenant id from the request context.
//     If absent, the middleware is a no-op — upstream auth has already
//     decided whether to issue a 401.
//  2. Resolves the tenant's Tier (via TierResolver). Unknown tenants
//     map to TierFree so the path is fail-safe.
//  3. Looks up the per-tenant token bucket (rps/burst from Limits) and
//     consumes one token. On deny: emit 429 + Retry-After and (if an
//     Auditor is wired) record a `quota.rate.limit` audit event.
//  4. For route-allowlisted creation routes (VM create, workspace
//     create) additionally enforces the tier's MaxConcurrentVMs /
//     MaxWorkspaces ceilings via the Counters projection.
//
// Read-only and unauthenticated routes (health, OAuth callback) are
// expected to opt out via the chi Route boundary — the middleware is
// only mounted on the authenticated /api subtree.
package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/GembaCore/gemba-core/internal/quota"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// TierResolver maps a tenant id to its current subscription tier. The
// production wiring backs this with internal/tenant.Store (cached);
// tests inject a static map.
type TierResolver func(ctx context.Context, id tenant.ID) (quota.Tier, error)

// QuotaAuditor is the narrow audit hook used by the middleware. Kept
// local (not the full server/audit.Auditor interface) so this package
// stays out of the server import cycle.
type QuotaAuditor interface {
	QuotaRateLimit(ctx context.Context, tid tenant.ID, route string, retrySec int)
}

// QuotaOptions configures WithQuota.
type QuotaOptions struct {
	// Store is the per-tenant token-bucket store. Required.
	Store *quota.BucketStore
	// Counters is the read-only usage projection used to gate the
	// VM/workspace creation routes. Pass NewMemCounters() to disable.
	// Required.
	Counters quota.Counters
	// Tier resolves the tenant's subscription level. When nil, every
	// tenant is treated as TierFree.
	Tier TierResolver
	// VMCreateRoutes is the allowlist of route paths (exact prefix
	// match) that trigger the MaxConcurrentVMs + MonthlyVMMinutes
	// ceiling check. Use prefix matching so chi param-bearing paths
	// (e.g. "/api/v1/workspaces/{wsid}/vms") match without the
	// middleware needing to know about chi.
	VMCreateRoutes []string
	// WorkspaceCreateRoutes is the analogous allowlist for the
	// MaxWorkspaces ceiling.
	WorkspaceCreateRoutes []string
	// Auditor receives quota.rate.limit / quota.ceiling events. Nil
	// disables audit emission.
	Auditor QuotaAuditor
}

// WithQuota returns middleware that enforces the tier-aware quota +
// rate-limit envelope described in the package doc.
//
// The returned handler panics if opts.Store or opts.Counters is nil —
// these are programming errors at wiring time, not runtime conditions.
func WithQuota(opts QuotaOptions) func(http.Handler) http.Handler {
	if opts.Store == nil {
		panic("middleware.WithQuota: Store is required")
	}
	if opts.Counters == nil {
		panic("middleware.WithQuota: Counters is required")
	}
	if opts.Tier == nil {
		opts.Tier = func(context.Context, tenant.ID) (quota.Tier, error) {
			return quota.TierFree, nil
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tid, ok := tenant.FromContext(r.Context())
			if !ok {
				// No tenant on the context — upstream auth handles
				// the 401. Bypass enforcement so unauthenticated
				// paths (health, OAuth callback) work without
				// route-level opt-out.
				next.ServeHTTP(w, r)
				return
			}
			tier, err := opts.Tier(r.Context(), tid)
			if err != nil || tier == "" {
				// Fail-safe: an unknown tenant or resolver error
				// drops to TierFree. The middleware is a guard,
				// not a directory — it should not 500 because the
				// directory is briefly unavailable.
				tier = quota.TierFree
			}
			limits := quota.LimitsForTier(tier)
			b := opts.Store.Get(tid, limits.RPS, limits.Burst)
			if !b.Allow() {
				secs := int(b.RetryAfter().Seconds())
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				if opts.Auditor != nil {
					opts.Auditor.QuotaRateLimit(r.Context(), tid, r.URL.Path, secs)
				}
				writeQuotaError(w, http.StatusTooManyRequests, "rate_limited",
					"per-tenant quota exhausted; retry after "+strconv.Itoa(secs)+"s")
				return
			}
			// Concurrent-VM ceiling on the VM-create allowlist.
			if matchesAny(r.URL.Path, opts.VMCreateRoutes) {
				if limits.MaxConcurrentVMs > 0 && opts.Counters.ConcurrentVMs(string(tid)) >= limits.MaxConcurrentVMs {
					writeQuotaError(w, http.StatusTooManyRequests, "vm_limit",
						"tier "+string(tier)+" allows at most "+strconv.Itoa(limits.MaxConcurrentVMs)+" concurrent VMs")
					return
				}
				if limits.MonthlyVMMinutes > 0 && opts.Counters.MonthlyVMMinutes(string(tid)) >= limits.MonthlyVMMinutes {
					writeQuotaError(w, http.StatusPaymentRequired, "monthly_minutes_exhausted",
						"tier "+string(tier)+" monthly VM-minute budget exhausted")
					return
				}
			}
			// Workspace-count ceiling on the workspace-create allowlist.
			if matchesAny(r.URL.Path, opts.WorkspaceCreateRoutes) {
				if limits.MaxWorkspaces > 0 && opts.Counters.Workspaces(string(tid)) >= limits.MaxWorkspaces {
					writeQuotaError(w, http.StatusTooManyRequests, "workspace_limit",
						"tier "+string(tier)+" allows at most "+strconv.Itoa(limits.MaxWorkspaces)+" workspaces")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// matchesAny reports whether path begins with any of the prefixes in
// allow. An empty allow list returns false (no enforcement).
func matchesAny(path string, allow []string) bool {
	for _, p := range allow {
		if p == "" {
			continue
		}
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// writeQuotaError emits the canonical JSON envelope. Kept local rather
// than depending on internal/server/httperr because the middleware
// package sits below /server in the import graph.
func writeQuotaError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Hand-rolled to avoid importing encoding/json plus the wider
	// httperr surface; the schema mirrors httperr.Write.
	_, _ = w.Write([]byte(`{"error":"` + code + `","code":"` + code + `","message":"` + jsonEscape(msg) + `"}`))
}

// jsonEscape escapes the minimum set of chars that may appear in our
// error messages (quote + backslash + newline). Anything more exotic
// gets caught by upstream callers before it reaches the middleware.
func jsonEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
