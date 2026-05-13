// gm-o9t8.4.1 — tests for the GitHub OrgGate (Workstream B slice b).

package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ghServer spins up an httptest.Server that mimics the three GitHub
// endpoints the OrgGate consults. The "tok-good" token returns the
// configured payloads; any other token answers 401 across all routes.
func ghServer(t *testing.T, user authUser, orgs []ghOrg, teams []ghTeam) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer tok-good"
	}
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(user)
	})
	mux.HandleFunc("/user/orgs", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(orgs)
	})
	mux.HandleFunc("/user/teams", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(teams)
	})
	return httptest.NewServer(mux)
}

func TestOrgGate_AllowsUserInOrg(t *testing.T) {
	srv := ghServer(t,
		authUser{Login: "alice", ID: 7},
		[]ghOrg{{Login: "GembaCore"}, {Login: "other"}},
		nil,
	)
	defer srv.Close()
	g := (OrgGate{AllowedOrgs: []string{"gembacore"}}).WithAPIBase(srv.URL)
	login, err := g.Allow(context.Background(), "tok-good")
	if err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
	if login != "alice" {
		t.Fatalf("want login=alice, got %q", login)
	}
}

func TestOrgGate_DeniesNotInOrg(t *testing.T) {
	srv := ghServer(t,
		authUser{Login: "mallory", ID: 9},
		[]ghOrg{{Login: "evil-corp"}},
		nil,
	)
	defer srv.Close()
	g := (OrgGate{AllowedOrgs: []string{"gembacore"}}).WithAPIBase(srv.URL)
	_, err := g.Allow(context.Background(), "tok-good")
	if !errors.Is(err, ErrNotInAllowed) {
		t.Fatalf("want ErrNotInAllowed, got %v", err)
	}
}

func TestOrgGate_AllowsUserInTeam(t *testing.T) {
	teams := []ghTeam{{Slug: "platform"}}
	teams[0].Organization.Login = "GembaCore"
	srv := ghServer(t,
		authUser{Login: "bob", ID: 11},
		nil,
		teams,
	)
	defer srv.Close()
	g := (OrgGate{AllowedTeams: []string{"gembacore/platform"}}).WithAPIBase(srv.URL)
	login, err := g.Allow(context.Background(), "tok-good")
	if err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
	if login != "bob" {
		t.Fatalf("want login=bob, got %q", login)
	}
}

func TestOrgGate_DeniesNotInTeam(t *testing.T) {
	teams := []ghTeam{{Slug: "ops"}}
	teams[0].Organization.Login = "OtherOrg"
	srv := ghServer(t,
		authUser{Login: "carol", ID: 13},
		nil,
		teams,
	)
	defer srv.Close()
	g := (OrgGate{AllowedTeams: []string{"gembacore/platform"}}).WithAPIBase(srv.URL)
	_, err := g.Allow(context.Background(), "tok-good")
	if !errors.Is(err, ErrNotInAllowed) {
		t.Fatalf("want ErrNotInAllowed, got %v", err)
	}
}

func TestOrgGate_RejectsBadToken(t *testing.T) {
	srv := ghServer(t, authUser{Login: "x"}, nil, nil)
	defer srv.Close()
	g := (OrgGate{AllowedOrgs: []string{"gembacore"}}).WithAPIBase(srv.URL)
	_, err := g.Allow(context.Background(), "tok-bad")
	if !errors.Is(err, ErrGitHubTokenRejected) {
		t.Fatalf("want ErrGitHubTokenRejected, got %v", err)
	}
}

func TestOrgGate_OpenAllowsAnyUser(t *testing.T) {
	srv := ghServer(t, authUser{Login: "anyone", ID: 1}, nil, nil)
	defer srv.Close()
	g := (OrgGate{}).WithAPIBase(srv.URL)
	login, err := g.Allow(context.Background(), "tok-good")
	if err != nil {
		t.Fatalf("open gate should allow, got %v", err)
	}
	if login != "anyone" {
		t.Fatalf("want login=anyone, got %q", login)
	}
}

func TestOrgGate_EmptyToken(t *testing.T) {
	g := OrgGate{}
	_, err := g.Allow(context.Background(), "")
	if !errors.Is(err, ErrGitHubTokenRejected) {
		t.Fatalf("want ErrGitHubTokenRejected, got %v", err)
	}
}

func TestOrgGateStore_GetSet(t *testing.T) {
	s := NewOrgGateStore()
	if got := s.Get("t-abc"); !got.IsOpen() {
		t.Fatalf("zero-value store should return open gate")
	}
	s.Set("t-abc", OrgGate{AllowedOrgs: []string{"gembacore"}})
	got := s.Get("t-abc")
	if got.IsOpen() {
		t.Fatalf("stored gate should not be open")
	}
	if len(got.AllowedOrgs) != 1 || got.AllowedOrgs[0] != "gembacore" {
		t.Fatalf("unexpected gate %+v", got)
	}
}
