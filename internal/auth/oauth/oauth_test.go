package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNew_ConfigMatrix exercises the validation table for New (gm-o9t8.3.5.1).
func TestNew_ConfigMatrix(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{"empty", Config{}, ErrUnconfigured},
		{"id_only", Config{GitHubClientID: "Iv1.abc"}, ErrPartialConfig},
		{"secret_only", Config{GitHubClientSecret: "shh"}, ErrPartialConfig},
		{"both", Config{GitHubClientID: "Iv1.abc", GitHubClientSecret: "shh"}, nil},
		{"both_with_base", Config{GitHubClientID: "Iv1.abc", GitHubClientSecret: "shh", CallbackBaseURL: "https://gemba.example.com"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(tc.cfg)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err: got %v, want %v", err, tc.wantErr)
				}
				if got != nil {
					t.Fatalf("expected nil handle on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got == nil {
				t.Fatalf("expected non-nil handle")
			}
			if got.ClientID() != tc.cfg.GitHubClientID {
				t.Fatalf("ClientID: got %q want %q", got.ClientID(), tc.cfg.GitHubClientID)
			}
		})
	}
}

// TestCallbackURL covers both an empty CallbackBaseURL (returns just
// CallbackPath) and a populated one (joined with no trailing slash).
func TestCallbackURL(t *testing.T) {
	o, err := New(Config{GitHubClientID: "id", GitHubClientSecret: "sec"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := o.CallbackURL(); got != CallbackPath {
		t.Fatalf("empty base: got %q want %q", got, CallbackPath)
	}
	o2, _ := New(Config{GitHubClientID: "id", GitHubClientSecret: "sec", CallbackBaseURL: "https://gemba.example.com/"})
	want := "https://gemba.example.com" + CallbackPath
	if got := o2.CallbackURL(); got != want {
		t.Fatalf("with base: got %q want %q", got, want)
	}
}

// TestDisabledHandler verifies the 503 envelope shape — CLIs depend on
// the stable error.code so they can distinguish "server doesn't speak
// OAuth" from auth failures.
func TestDisabledHandler(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, CallbackPath, nil)
	DisabledHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rr.Code)
	}
	var body struct {
		Error map[string]string `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error["code"] != "oauth_not_configured" {
		t.Fatalf("code: got %q want oauth_not_configured", body.Error["code"])
	}
}

// TestFetchUserAt exercises the test seam against an httptest.Server,
// which is how gm-o9t8.3.5.2 mocks GitHub end-to-end.
func TestFetchUserAt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    int64(424242),
			"login": "octocat",
		})
	}))
	defer srv.Close()
	o, _ := New(Config{GitHubClientID: "id", GitHubClientSecret: "sec"})
	u, err := o.FetchUserAt(context.Background(), srv.URL, "gho_xxx")
	if err != nil {
		t.Fatalf("FetchUserAt: %v", err)
	}
	if u.ID != 424242 || u.Login != "octocat" {
		t.Fatalf("user: %+v", u)
	}
}

// TestFetchUserAt_Rejected confirms a non-2xx upstream surfaces as an
// error rather than a zero-value user.
func TestFetchUserAt_Rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()
	o, _ := New(Config{GitHubClientID: "id", GitHubClientSecret: "sec"})
	if _, err := o.FetchUserAt(context.Background(), srv.URL, "bad"); err == nil {
		t.Fatalf("expected error on 401")
	}
}
