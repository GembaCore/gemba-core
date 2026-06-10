// gm-o9t8.3.5.5 — `gemba auth tokens list|revoke|rotate`.
//
// Three subcommands that talk to the gemba server's
// /api/v1/auth/tokens surface. The shared session glue lives in the
// helper authTokensClient which:
//
//   - resolves the server URL (--server flag → serverconfig.Resolve)
//   - loads the credstore credential for that server
//   - injects the bearer + nonce headers the server requires
//
// Rotation specifically: after the POST succeeds we MUST persist the
// new bearer back to the credstore — otherwise the very next call
// from this CLI session would fail with 401. We do that before
// printing the success line; a credstore write failure rolls the
// command into an error so the user is never left with a stale
// credential file silently shadowing a working server-side row.

package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/auth/credstore"
	"github.com/GembaCore/gemba-core/internal/cli/serverconfig"
)

// authTokensFlags captures the shared flags every subcommand exposes.
type authTokensFlags struct {
	server   string
	credPath string
}

// authTokenInfo mirrors server.TokenInfo on the wire.
type authTokenInfo struct {
	ID          string     `json:"id"`
	DeviceLabel string     `json:"device_label"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

type authTokensListResponse struct {
	Tokens []authTokenInfo `json:"tokens"`
}

type authTokensRotateResponse struct {
	NewBearer   string `json:"new_bearer"`
	TokenID     string `json:"token_id"`
	DeviceLabel string `json:"device_label"`
}

// newAuthTokensCmd builds the `gemba auth tokens` subtree.
func newAuthTokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "List, revoke, and rotate OAuth-minted bearer tokens",
	}
	cmd.AddCommand(newAuthTokensListCmd())
	cmd.AddCommand(newAuthTokensRevokeCmd())
	cmd.AddCommand(newAuthTokensRotateCmd())
	return cmd
}

func bindTokensFlags(cmd *cobra.Command, f *authTokensFlags) {
	cmd.Flags().StringVar(&f.server, "server", "", "gemba server base URL (defaults to the stored single server)")
	cmd.Flags().StringVar(&f.credPath, "credentials-path", "",
		"override the credentials file path (default: $XDG_CONFIG_HOME/gemba/credentials.json)")
}

func newAuthTokensListCmd() *cobra.Command {
	var f authTokensFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your bearer tokens (non-revoked) for the current server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthTokensList(cmd.Context(), cmd.OutOrStdout(), nil, f)
		},
	}
	bindTokensFlags(cmd, &f)
	return cmd
}

func newAuthTokensRevokeCmd() *cobra.Command {
	var f authTokensFlags
	cmd := &cobra.Command{
		Use:   "revoke <token_id>",
		Short: "Revoke a bearer token by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthTokensRevoke(cmd.Context(), cmd.OutOrStdout(), nil, f, args[0])
		},
	}
	bindTokensFlags(cmd, &f)
	return cmd
}

func newAuthTokensRotateCmd() *cobra.Command {
	var f authTokensFlags
	cmd := &cobra.Command{
		Use:   "rotate <token_id>",
		Short: "Rotate a bearer token; updates the local credstore with the new value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthTokensRotate(cmd.Context(), cmd.OutOrStdout(), nil, f, args[0])
		},
	}
	bindTokensFlags(cmd, &f)
	return cmd
}

// resolveTokensSession bundles server, credPath, credential. Centralizes
// the "which server am I talking to + what's my bearer" lookup.
type tokensSession struct {
	server   string
	credPath string
	cred     credstore.Credential
}

func resolveTokensSession(f authTokensFlags) (tokensSession, error) {
	path := f.credPath
	if path == "" {
		p, err := credstore.DefaultPath()
		if err != nil {
			return tokensSession{}, err
		}
		path = p
	}
	store, err := credstore.Load(path)
	if err != nil {
		return tokensSession{}, fmt.Errorf("load credentials: %w", err)
	}
	chosen := strings.TrimSpace(f.server)
	if chosen == "" {
		resolved, err := serverconfig.ResolveDetail("")
		if err == nil && resolved.Source != serverconfig.SourceDefault {
			chosen = resolved.URL
		}
	} else {
		resolved, err := serverconfig.Resolve(chosen)
		if err != nil {
			return tokensSession{}, err
		}
		chosen = resolved
	}
	if chosen == "" {
		servers := store.ServerList()
		switch len(servers) {
		case 0:
			return tokensSession{}, errors.New("no stored servers; run `gemba login` first")
		case 1:
			chosen = servers[0]
		default:
			return tokensSession{}, fmt.Errorf("multiple stored servers; pass --server (one of: %s)",
				strings.Join(servers, ", "))
		}
	}
	cred, ok := store.Get(chosen)
	if !ok {
		return tokensSession{}, fmt.Errorf("no stored credential for %s; run `gemba login` first", chosen)
	}
	return tokensSession{server: chosen, credPath: path, cred: cred}, nil
}

// authTokensHTTPClient is the override seam tests use to point at an
// httptest.Server. Zero value falls back to http.DefaultClient.
var authTokensHTTPClient *http.Client

func tokensHTTPClient(override *http.Client) *http.Client {
	if override != nil {
		return override
	}
	if authTokensHTTPClient != nil {
		return authTokensHTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// newNonce returns a 16-byte hex string for the X-GEMBA-Confirm
// idempotency header.
func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func runAuthTokensList(ctx context.Context, out io.Writer, httpc *http.Client, f authTokensFlags) error {
	sess, err := resolveTokensSession(f)
	if err != nil {
		return err
	}
	url := strings.TrimRight(sess.server, "/") + "/api/v1/auth/tokens"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+sess.cred.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := tokensHTTPClient(httpc).Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(parseErrorEnvelope(body, resp.StatusCode))
	}
	var dec authTokensListResponse
	if err := json.Unmarshal(body, &dec); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if len(dec.Tokens) == 0 {
		fmt.Fprintln(out, "no tokens")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TOKEN_ID\tDEVICE\tCREATED\tLAST_USED")
	for _, t := range dec.Tokens {
		last := "-"
		if t.LastUsedAt != nil {
			last = t.LastUsedAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			t.ID, t.DeviceLabel, t.CreatedAt.UTC().Format(time.RFC3339), last)
	}
	return tw.Flush()
}

func runAuthTokensRevoke(ctx context.Context, out io.Writer, httpc *http.Client, f authTokensFlags, tokenID string) error {
	sess, err := resolveTokensSession(f)
	if err != nil {
		return err
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return errors.New("token_id required")
	}
	nonce, err := newNonce()
	if err != nil {
		return err
	}
	url := strings.TrimRight(sess.server, "/") + "/api/v1/auth/tokens/" + tokenID
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+sess.cred.Token)
	req.Header.Set("X-GEMBA-Confirm", nonce)
	resp, err := tokensHTTPClient(httpc).Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNoContent {
		fmt.Fprintf(out, "revoked token %s\n", tokenID)
		return nil
	}
	return errors.New(parseErrorEnvelope(body, resp.StatusCode))
}

func runAuthTokensRotate(ctx context.Context, out io.Writer, httpc *http.Client, f authTokensFlags, tokenID string) error {
	sess, err := resolveTokensSession(f)
	if err != nil {
		return err
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return errors.New("token_id required")
	}
	nonce, err := newNonce()
	if err != nil {
		return err
	}
	url := strings.TrimRight(sess.server, "/") + "/api/v1/auth/tokens/" + tokenID + "/rotate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+sess.cred.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GEMBA-Confirm", nonce)
	resp, err := tokensHTTPClient(httpc).Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(parseErrorEnvelope(body, resp.StatusCode))
	}
	var dec authTokensRotateResponse
	if err := json.Unmarshal(body, &dec); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if dec.NewBearer == "" {
		return errors.New("rotate: server returned empty bearer")
	}
	// Persist the new bearer back into the credstore so the next CLI
	// call doesn't 401 against the now-revoked prior bearer.
	store, err := credstore.Load(sess.credPath)
	if err != nil {
		return fmt.Errorf("rotate: load credentials: %w", err)
	}
	updated := credstore.Credential{
		Token:    dec.NewBearer,
		Subject:  sess.cred.Subject,
		StoredAt: time.Now().UTC(),
	}
	if err := store.Put(sess.server, updated); err != nil {
		return fmt.Errorf("rotate: persist credentials: %w", err)
	}
	fmt.Fprintf(out, "rotated token: new id %s (device %s); credstore updated\n",
		dec.TokenID, dec.DeviceLabel)
	return nil
}
