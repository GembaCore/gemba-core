// gm-o9t8.4.1 — GitHub OAuth org/team gating (Workstream B slice b).
//
// OrgGate is the per-tenant policy that decides whether a GitHub user
// (identified by a valid OAuth access token) is allowed to log in to a
// particular gemba tenant. Two membership predicates are evaluated
// against api.github.com:
//
//   - AllowedOrgs: the user must be a member of at least one of the
//     listed organizations (matched against GET /user/orgs).
//   - AllowedTeams: the user must be a member of at least one of the
//     listed teams; entries are "org/team" pairs matched against
//     GET /user/teams.
//
// Both lists are inclusive — a user qualifies if EITHER list yields a
// hit (i.e. union semantics). An empty OrgGate (no orgs AND no teams)
// is the open-tenant default and accepts any authenticated GitHub user.
//
// Token rejection (any 4xx response from GitHub) is surfaced as
// ErrGitHubTokenRejected so handlers can map cleanly onto the existing
// "github_token_rejected" wire envelope.

package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrGitHubTokenRejected is returned by OrgGate.Allow when GitHub
// itself answers 401/403 for the supplied access token. Callers that
// need to distinguish "bad token" from "denied by policy" use
// errors.Is.
var ErrGitHubTokenRejected = errors.New("auth: github token rejected")

// ErrNotInAllowed is returned by OrgGate.Allow when the supplied user
// authenticates fine against GitHub but doesn't satisfy any of the
// configured org/team predicates.
var ErrNotInAllowed = errors.New("auth: not in allowed orgs/teams")

// OrgGate is a tenant-scoped allow-list policy. The zero value is the
// "no restrictions" gate — every GitHub user with a valid token is
// permitted (subject to future role checks).
type OrgGate struct {
	// AllowedOrgs lists GitHub organization logins. A user matches if
	// they appear in GET /user/orgs for any login (case-insensitive).
	AllowedOrgs []string

	// AllowedTeams lists "org/team" pairs. A user matches if their
	// GET /user/teams response includes a team whose owning org +
	// slug match (case-insensitive).
	AllowedTeams []string

	// httpClient is the test seam; nil falls back to a 15s default.
	httpClient *http.Client

	// apiBase overrides https://api.github.com (for httptest). The
	// production caller leaves this empty.
	apiBase string
}

// IsOpen reports whether the gate has no restrictions configured. An
// open gate accepts any valid GitHub token without consulting orgs /
// teams.
func (g *OrgGate) IsOpen() bool {
	if g == nil {
		return true
	}
	return len(g.AllowedOrgs) == 0 && len(g.AllowedTeams) == 0
}

// WithHTTPClient returns a copy of g using the supplied HTTP client.
// Tests use this to inject an httptest.Server-bound RoundTripper.
func (g OrgGate) WithHTTPClient(c *http.Client) OrgGate {
	g.httpClient = c
	return g
}

// WithAPIBase returns a copy of g whose GitHub API calls hit baseURL
// rather than https://api.github.com. Tests point this at an
// httptest.Server; production code never sets it.
func (g OrgGate) WithAPIBase(baseURL string) OrgGate {
	g.apiBase = strings.TrimRight(baseURL, "/")
	return g
}

// Allow validates the supplied GitHub OAuth access token against the
// gate. On success the GitHub login is returned. On policy denial the
// error wraps ErrNotInAllowed; on a rejected token it wraps
// ErrGitHubTokenRejected.
func (g *OrgGate) Allow(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("%w: empty token", ErrGitHubTokenRejected)
	}
	client := g.client()
	base := g.base()

	user, err := fetchAuthUser(ctx, client, base, token)
	if err != nil {
		return "", err
	}

	if g.IsOpen() {
		return user.Login, nil
	}

	if len(g.AllowedOrgs) > 0 {
		orgs, err := fetchUserOrgs(ctx, client, base, token)
		if err != nil {
			return "", err
		}
		if matchesAny(orgs, g.AllowedOrgs) {
			return user.Login, nil
		}
	}

	if len(g.AllowedTeams) > 0 {
		teams, err := fetchUserTeams(ctx, client, base, token)
		if err != nil {
			return "", err
		}
		if matchesTeam(teams, g.AllowedTeams) {
			return user.Login, nil
		}
	}

	return "", fmt.Errorf("%w: user=%s", ErrNotInAllowed, user.Login)
}

func (g *OrgGate) client() *http.Client {
	if g != nil && g.httpClient != nil {
		return g.httpClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (g *OrgGate) base() string {
	if g != nil && g.apiBase != "" {
		return g.apiBase
	}
	return "https://api.github.com"
}

// authUser captures the minimum subset of /user we need.
type authUser struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

type ghOrg struct {
	Login string `json:"login"`
}

type ghTeam struct {
	Slug         string `json:"slug"`
	Organization struct {
		Login string `json:"login"`
	} `json:"organization"`
}

func fetchAuthUser(ctx context.Context, client *http.Client, base, token string) (authUser, error) {
	var u authUser
	if err := ghGet(ctx, client, base+"/user", token, &u); err != nil {
		return authUser{}, err
	}
	if u.Login == "" {
		return authUser{}, fmt.Errorf("%w: github /user missing login", ErrGitHubTokenRejected)
	}
	return u, nil
}

func fetchUserOrgs(ctx context.Context, client *http.Client, base, token string) ([]ghOrg, error) {
	var orgs []ghOrg
	if err := ghGet(ctx, client, base+"/user/orgs", token, &orgs); err != nil {
		return nil, err
	}
	return orgs, nil
}

func fetchUserTeams(ctx context.Context, client *http.Client, base, token string) ([]ghTeam, error) {
	var teams []ghTeam
	if err := ghGet(ctx, client, base+"/user/teams", token, &teams); err != nil {
		return nil, err
	}
	return teams, nil
}

// ghGet performs a JSON GET against the GitHub API surface. 4xx
// responses are surfaced as ErrGitHubTokenRejected so callers can
// distinguish them from transport / decode errors.
func ghGet(ctx context.Context, client *http.Client, url, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("auth: build github request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("auth: github request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: %s %d %s", ErrGitHubTokenRejected, url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("auth: github %s: %d %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("auth: decode github response: %w", err)
	}
	return nil
}

func matchesAny(orgs []ghOrg, allowed []string) bool {
	for _, want := range allowed {
		want = strings.ToLower(strings.TrimSpace(want))
		if want == "" {
			continue
		}
		for _, o := range orgs {
			if strings.EqualFold(o.Login, want) {
				return true
			}
		}
	}
	return false
}

func matchesTeam(teams []ghTeam, allowed []string) bool {
	for _, want := range allowed {
		want = strings.ToLower(strings.TrimSpace(want))
		slash := strings.IndexByte(want, '/')
		if slash <= 0 || slash == len(want)-1 {
			continue
		}
		wantOrg, wantSlug := want[:slash], want[slash+1:]
		for _, t := range teams {
			if strings.EqualFold(t.Organization.Login, wantOrg) && strings.EqualFold(t.Slug, wantSlug) {
				return true
			}
		}
	}
	return false
}

// OrgGateStore is the per-tenant OrgGate registry. The OAuth login
// handler resolves the bearer-bound tenant id and calls Get to apply
// the right policy. The store is goroutine-safe and zero-value usable.
//
// The persistence story (Dolt-backed config rows) lands with the
// broader tenant-config refactor; this in-memory store mirrors the
// SQLStore + MemStore split tenant.Store uses today.
type OrgGateStore struct {
	mu     sync.RWMutex
	gates  map[string]OrgGate
}

// NewOrgGateStore returns an empty in-memory store.
func NewOrgGateStore() *OrgGateStore {
	return &OrgGateStore{gates: map[string]OrgGate{}}
}

// Set records the gate for tenantID. Passing the zero-value OrgGate
// clears any prior restriction (== open).
func (s *OrgGateStore) Set(tenantID string, g OrgGate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gates == nil {
		s.gates = map[string]OrgGate{}
	}
	s.gates[tenantID] = g
}

// Get returns the gate for tenantID. Unknown tenants return the open
// zero value, matching the "no policy ⇒ permissive" default.
func (s *OrgGateStore) Get(tenantID string) OrgGate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gates[tenantID]
}
