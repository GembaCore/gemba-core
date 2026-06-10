package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/tenant"
)

// fakeAuth is a tiny stand-in for the real bearer auth middleware:
// it accepts any bearer "token-<tenant-id>" and rejects empty
// Authorization. It does NOT touch the request context — WithTenant
// is what stamps the tenant. Tests use this to compose [auth, tenant,
// require-wsid-match] without dragging the real auth package in.
func fakeAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenToTenant is a TenantResolver that maps "token-<id>" → <id>.
// Empty bearer → DefaultTenant (cookie-auth path equivalent).
func tokenToTenant(bearer string) (tenant.ID, bool) {
	if bearer == "" {
		return tenant.DefaultTenant, true
	}
	const prefix = "token-"
	if len(bearer) <= len(prefix) || bearer[:len(prefix)] != prefix {
		return "", false
	}
	id, err := tenant.ParseID(bearer[len(prefix):])
	if err != nil {
		return "", false
	}
	return id, true
}

func buildRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(fakeAuth)
	r.Use(WithTenant(tokenToTenant))
	r.Route("/api/v1/workspaces/{wsid}", func(r chi.Router) {
		r.Use(RequireWSIDTenantMatch)
		r.Get("/status", func(w http.ResponseWriter, req *http.Request) {
			tid, _ := tenant.FromContext(req.Context())
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(string(tid)))
		})
	})
	return r
}

func do(t *testing.T, h http.Handler, method, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestWSID_SameTenant_Succeeds(t *testing.T) {
	rr := do(t, buildRouter(), "GET", "/api/v1/workspaces/t-abcd:proj/status", "token-t-abcd1234")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "t-abcd1234" {
		t.Errorf("tenant in ctx = %q", rr.Body.String())
	}
}

func TestWSID_CrossTenant_403(t *testing.T) {
	rr := do(t, buildRouter(), "GET", "/api/v1/workspaces/t-xxxx:proj/status", "token-t-abcd1234")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("got %d %s, want 403", rr.Code, rr.Body.String())
	}
}

func TestWSID_BareForm_SingleUser_Succeeds(t *testing.T) {
	// Single-user resolver always answers DefaultTenant; a bare
	// wsid also resolves to DefaultTenant. Both prefixes match.
	r := chi.NewRouter()
	r.Use(fakeAuth)
	r.Use(WithTenant(SingleUserResolver()))
	r.Route("/api/v1/workspaces/{wsid}", func(r chi.Router) {
		r.Use(RequireWSIDTenantMatch)
		r.Get("/status", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})
	rr := do(t, r, "GET", "/api/v1/workspaces/legacy-proj/status", "any-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (bare wsid + single-user should pass)", rr.Code)
	}
}

func TestWSID_NoAuth_401(t *testing.T) {
	rr := do(t, buildRouter(), "GET", "/api/v1/workspaces/t-abcd:proj/status", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
}

func TestWSID_MalformedWSID_400(t *testing.T) {
	rr := do(t, buildRouter(), "GET", "/api/v1/workspaces/x-bad:proj/status", "token-t-abcd1234")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestWithTenant_ResolverReject_401(t *testing.T) {
	r := chi.NewRouter()
	r.Use(fakeAuth)
	r.Use(WithTenant(func(string) (tenant.ID, bool) { return "", false }))
	r.Get("/x", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(200) })
	rr := do(t, r, "GET", "/x", "anything")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 (resolver rejected)", rr.Code)
	}
}

func TestWithTenant_NilResolver_FallsBackToDefault(t *testing.T) {
	r := chi.NewRouter()
	r.Use(fakeAuth)
	r.Use(WithTenant(nil))
	r.Get("/x", func(w http.ResponseWriter, req *http.Request) {
		tid, ok := tenant.FromContext(req.Context())
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(tid))
	})
	rr := do(t, r, "GET", "/x", "tok")
	if rr.Code != http.StatusOK || rr.Body.String() != string(tenant.DefaultTenant) {
		t.Fatalf("got %d %q, want 200 %q", rr.Code, rr.Body.String(), tenant.DefaultTenant)
	}
}

func TestRequireWSIDTenantMatch_NoTenantInCtx_401(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1/workspaces/{wsid}", func(r chi.Router) {
		r.Use(RequireWSIDTenantMatch)
		r.Get("/x", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(200) })
	})
	req := httptest.NewRequest("GET", "/api/v1/workspaces/t-abcd:proj/x", nil)
	req = req.WithContext(context.Background())
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
}
