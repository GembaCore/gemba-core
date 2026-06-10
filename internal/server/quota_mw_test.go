package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/quota"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// withTenantCtx is a tiny harness shim that stamps a tenant id on the
// request context — the production WithTenant middleware does the same
// thing after auth resolves the bearer.
func withTenantCtx(id tenant.ID, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := tenant.WithContext(r.Context(), id)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestQuotaMiddleware_AllowsWhenUnderBudget(t *testing.T) {
	r := &Router{}
	lim := quota.NewLimiter()
	lim.Set("t-aaaaaaaa", quota.Config{Rate: 100, Burst: 100})
	r.AttachQuotaLimiter(lim)

	called := 0
	h := withTenantCtx("t-aaaaaaaa", r.QuotaMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	})))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("expected pass-through, got code=%d called=%d", w.Code, called)
	}
}

func TestQuotaMiddleware_Denies429WithRetryAfter(t *testing.T) {
	r := &Router{}
	lim := quota.NewLimiter()
	lim.Set("t-bbbbbbbb", quota.Config{Rate: 1, Burst: 1})
	r.AttachQuotaLimiter(lim)

	called := 0
	h := withTenantCtx("t-bbbbbbbb", r.QuotaMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})))

	// 1st call drains the burst.
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil))
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w1.Code)
	}
	// 2nd call denies.
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil))
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w2.Code)
	}
	if got := w2.Header().Get("Retry-After"); got == "" {
		t.Fatal("expected Retry-After header")
	} else if n, err := strconv.Atoi(got); err != nil || n < 1 {
		t.Fatalf("expected positive integer Retry-After, got %q", got)
	}
	body, _ := io.ReadAll(w2.Body)
	if !strings.Contains(string(body), "rate_limited") {
		t.Fatalf("expected rate_limited code in body, got %s", string(body))
	}
	if called != 1 {
		t.Fatalf("inner handler should run exactly once, got %d", called)
	}
}

func TestQuotaMiddleware_NoLimiterIsPassthrough(t *testing.T) {
	r := &Router{}
	called := 0
	h := withTenantCtx("t-aaaaaaaa", r.QuotaMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})))
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d", i, w.Code)
		}
	}
	if called != 5 {
		t.Fatalf("expected 5 passthroughs, got %d", called)
	}
}

func TestQuotaMiddleware_NoTenantContextPassthrough(t *testing.T) {
	r := &Router{}
	lim := quota.NewLimiter()
	lim.SetDefaults(quota.Config{Rate: 0.0001, Burst: 0})
	r.AttachQuotaLimiter(lim)

	called := 0
	h := r.QuotaMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/beads", nil).
		WithContext(context.Background())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || called != 1 {
		t.Fatalf("expected passthrough without tenant ctx, got code=%d called=%d", w.Code, called)
	}
}
