// client.go: the hand-written wrapper that gives callers a friendly
// constructor on top of the generated low-level client at gen/.
//
// Responsibilities:
//   - Resolve a credential from internal/auth/credstore by server URL
//     (overridable with WithToken).
//   - Attach `Authorization: Bearer <token>` to every outbound request.
//   - Attach `X-GEMBA-Confirm: <uuid>` to every state-mutating request
//     (POST / PATCH / PUT / DELETE) — required by the Govern surface
//     (gm-o9t8.6 / .8 / .9 / .10 / .12).
//   - Set a sensible User-Agent so server logs identify the CLI build.
//   - Map the server's JSON error envelope to typed sentinels — see
//     errors.go.
//
// gm-o9t8.1.1.4

package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/GembaCore/gemba-core/internal/auth/credstore"
	"github.com/GembaCore/gemba-core/internal/client/gen"
)

// DefaultUserAgent is sent on every request unless overridden with
// WithUserAgent. The version segment is bumped by the build pipeline.
const DefaultUserAgent = "gemba-cli/dev"

// confirmHeader is the nonce header the Govern endpoints check.
const confirmHeader = "X-GEMBA-Confirm"

// Client is the gemba typed HTTP client. The embedded
// *gen.ClientWithResponses gives callers direct access to every
// generated operation (GetWhoamiWithResponse, ListSpecsWithResponse,
// ReconcileSpecWithResponse, …). Wrapper-level methods provide
// envelope-mapped, typed convenience shortcuts.
type Client struct {
	*gen.ClientWithResponses

	raw       *gen.Client
	server    string
	token     string
	userAgent string
	httpDoer  *http.Client
}

// Option configures New. Options are applied in order; later options
// win for fields they overlap on.
type Option func(*options)

type options struct {
	token     string
	userAgent string
	http      *http.Client
	credPath  string
}

// WithToken overrides the token the wrapper would otherwise read from
// the credstore. Useful when a caller already holds a PAT (e.g. the
// `gemba login` command, which has no credstore entry yet).
func WithToken(tok string) Option {
	return func(o *options) { o.token = tok }
}

// WithHTTPClient swaps the underlying *http.Client. Default has a
// 30 s timeout; pass a configured client for proxy / mTLS scenarios.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) { o.http = c }
}

// WithUserAgent overrides the default User-Agent string.
func WithUserAgent(ua string) Option {
	return func(o *options) { o.userAgent = ua }
}

// WithCredentialsPath overrides the credstore file path. Mostly useful
// in tests; production callers should rely on the credstore default.
func WithCredentialsPath(path string) Option {
	return func(o *options) { o.credPath = path }
}

// New constructs a Client bound to server. The credstore is consulted
// for a stored token unless WithToken supplies one explicitly.
//
// Returns an error when:
//   - server is empty;
//   - the credstore can't be loaded;
//   - no credential exists for server AND WithToken was not supplied.
func New(server string, opts ...Option) (*Client, error) {
	if server == "" {
		return nil, errors.New("client: server URL is required")
	}
	o := options{userAgent: DefaultUserAgent}
	for _, opt := range opts {
		opt(&o)
	}
	tok := o.token
	if tok == "" {
		path := o.credPath
		if path == "" {
			p, err := credstore.DefaultPath()
			if err != nil {
				return nil, fmt.Errorf("client: resolve credstore path: %w", err)
			}
			path = p
		}
		store, err := credstore.Load(path)
		if err != nil {
			return nil, fmt.Errorf("client: load credstore: %w", err)
		}
		cred, ok := store.Get(server)
		if !ok {
			return nil, fmt.Errorf("client: no credential stored for %s (run `gemba login --server %s`)", server, server)
		}
		tok = cred.Token
	}
	httpDoer := o.http
	if httpDoer == nil {
		httpDoer = &http.Client{Timeout: 30 * time.Second}
	}
	c := &Client{
		server:    server,
		token:     tok,
		userAgent: o.userAgent,
		httpDoer:  httpDoer,
	}
	raw, err := gen.NewClient(server,
		gen.WithHTTPClient(c),
		gen.WithRequestEditorFn(c.addAuth),
		gen.WithRequestEditorFn(c.addConfirm),
		gen.WithRequestEditorFn(c.addUserAgent),
	)
	if err != nil {
		return nil, fmt.Errorf("client: construct generated client: %w", err)
	}
	c.raw = raw
	c.ClientWithResponses = &gen.ClientWithResponses{ClientInterface: raw}
	return c, nil
}

// Do satisfies gen.HttpRequestDoer so we can delegate to the real
// *http.Client while keeping a hook point for future request-level
// instrumentation (metrics, retries, …).
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.httpDoer.Do(req)
}

// addAuth is the bearer-token request editor.
func (c *Client) addAuth(_ context.Context, req *http.Request) error {
	if c.token == "" {
		return nil
	}
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return nil
}

// addUserAgent stamps the User-Agent unless the caller already set one.
func (c *Client) addUserAgent(_ context.Context, req *http.Request) error {
	if req.Header.Get("User-Agent") == "" && c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	return nil
}

// addConfirm injects a fresh UUID v4 X-GEMBA-Confirm header on every
// mutating verb. GET / HEAD / OPTIONS are skipped so cache headers and
// CDN behavior stay predictable. Callers that need to send a specific
// confirm token (e.g. replay protection in a retry) can set the header
// themselves before issuing the call — addConfirm only fills it in if
// empty.
func (c *Client) addConfirm(_ context.Context, req *http.Request) error {
	switch req.Method {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		if req.Header.Get(confirmHeader) == "" {
			req.Header.Set(confirmHeader, uuid.NewString())
		}
	}
	return nil
}

// Whoami is a convenience that calls the generated GetWhoami operation
// and returns the typed WhoamiResponse on success, or a mapped error
// (see errors.go) on failure.
func (c *Client) Whoami(ctx context.Context) (*gen.WhoamiResponse, error) {
	resp, err := c.GetWhoamiWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("whoami: %w", err)
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	return nil, mapServerErr(resp.StatusCode(), resp.Body)
}

// mapServerErr wraps mapErr so callers that have a parsed *…HTTP
// response (the WithResponse variants expose Body + StatusCode but not
// a *http.Response) can still get a typed error. mapErr only inspects
// resp.StatusCode, so we synthesize a minimal http.Response.
func mapServerErr(status int, body []byte) error {
	return mapErr(&http.Response{StatusCode: status}, body)
}
