package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/quota"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// quotaTestStack composes [WithTenant -> WithQuota -> handler]. The
// upstream auth shim is omitted here because WithTenant's resolver
// stamps the tenant directly.
func quotaTestStack(t *testing.T, opts QuotaOptions, resolveTo tenant.ID) http.Handler {
	t.Helper()
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mw := WithQuota(opts)
	return WithTenant(func(string) (tenant.ID, bool) {
		if resolveTo == "" {
			return "", false
		}
		return resolveTo, true
	})(mw(final))
}

func staticTier(tier quota.Tier) TierResolver {
	return func(context.Context, tenant.ID) (quota.Tier, error) {
		return tier, nil
	}
}

func TestWithQuota_AllowsUnderBudget(t *testing.T) {
	store := quota.NewBucketStore()
	h := quotaTestStack(t, QuotaOptions{
		Store:    store,
		Counters: quota.NewMemCounters(),
		Tier:     staticTier(quota.TierEnterprise), // very high RPS
	}, "t-aaaaaaaa")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil).
		WithContext(context.Background())
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 allow, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestWithQuota_Denies429WithRetryAfter(t *testing.T) {
	store := quota.NewBucketStore()
	h := quotaTestStack(t, QuotaOptions{
		Store:    store,
		Counters: quota.NewMemCounters(),
		Tier:     staticTier(quota.TierFree), // RPS=2 Burst=5 — drain quickly
	}, "t-bbbbbbbb")
	// Drain the burst (5 tokens) then expect the 6th call to 429.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil)
		req.Header.Set("Authorization", "Bearer x")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("burst call %d: expected 204, got %d", i, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil)
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Fatal("missing Retry-After header")
	} else if n, err := strconv.Atoi(got); err != nil || n < 1 {
		t.Fatalf("bad Retry-After %q", got)
	}
	if !strings.Contains(w.Body.String(), "rate_limited") {
		t.Errorf("body should mention rate_limited: %s", w.Body.String())
	}
}

func TestWithQuota_BypassesWhenNoTenantContext(t *testing.T) {
	// Build the middleware directly (without WithTenant) so the
	// request lacks a tenant on context — should bypass.
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mw := WithQuota(QuotaOptions{
		Store:    quota.NewBucketStore(),
		Counters: quota.NewMemCounters(),
		Tier:     staticTier(quota.TierFree),
	})(final)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected bypass to 204, got %d", w.Code)
	}
}

func TestWithQuota_ConcurrentVMGate(t *testing.T) {
	store := quota.NewBucketStore()
	counters := quota.NewMemCounters()
	// Free tier allows 1 concurrent VM. Pre-seed 1 to force the gate.
	counters.SetConcurrentVMs("t-cccccccc", 1)
	h := quotaTestStack(t, QuotaOptions{
		Store:          store,
		Counters:       counters,
		Tier:           staticTier(quota.TierFree),
		VMCreateRoutes: []string{"/api/v1/vms"},
	}, "t-cccccccc")

	// Non-allowlisted path — still passes through bucket but no VM gate.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("non-VM route: expected 204, got %d body=%s", w.Code, w.Body.String())
	}

	// Allowlisted path — VM ceiling kicks in (current=1 >= max=1).
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/vms", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("VM route over ceiling: expected 429, got %d body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "vm_limit") {
		t.Errorf("body should mention vm_limit: %s", w2.Body.String())
	}
}

func TestWithQuota_WorkspaceCeiling(t *testing.T) {
	store := quota.NewBucketStore()
	counters := quota.NewMemCounters()
	counters.SetWorkspaces("t-dddddddd", 1) // free-tier cap = 1
	h := quotaTestStack(t, QuotaOptions{
		Store:                 store,
		Counters:              counters,
		Tier:                  staticTier(quota.TierFree),
		WorkspaceCreateRoutes: []string{"/api/v1/workspaces"},
	}, "t-dddddddd")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 workspace_limit, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "workspace_limit") {
		t.Errorf("body should mention workspace_limit: %s", w.Body.String())
	}
}

// recordingAuditor captures rate-limit events for the audit-emission test.
type recordingAuditor struct {
	calls []string
}

func (r *recordingAuditor) QuotaRateLimit(_ context.Context, tid tenant.ID, route string, retrySec int) {
	r.calls = append(r.calls, string(tid)+":"+route+":"+strconv.Itoa(retrySec))
}

func TestWithQuota_AuditOn429(t *testing.T) {
	store := quota.NewBucketStore()
	aud := &recordingAuditor{}
	h := quotaTestStack(t, QuotaOptions{
		Store:    store,
		Counters: quota.NewMemCounters(),
		Tier:     staticTier(quota.TierFree),
		Auditor:  aud,
	}, "t-eeeeeeee")
	// Drain burst (5 for free tier).
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if len(aud.calls) != 1 {
		t.Fatalf("expected 1 audit emission, got %d (%v)", len(aud.calls), aud.calls)
	}
	if !strings.HasPrefix(aud.calls[0], "t-eeeeeeee:/api/v1/beads:") {
		t.Errorf("audit call shape wrong: %q", aud.calls[0])
	}
}
