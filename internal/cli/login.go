// gm-o9t8.1.1.2: `gemba login` and `gemba whoami`.
//
// login validates a personal access token against <server>/api/whoami
// and, on success, persists it to the per-user credentials file (see
// internal/auth/credstore). whoami reads the same file and confirms
// the credential is still valid by re-calling /api/whoami.
//
// gm-o9t8.1.1.4: whoami is wired through the typed Go client at
// internal/client. login intentionally keeps its ad-hoc HTTP path
// because it has to validate a token BEFORE there is anything in the
// credstore for the new typed client to read — a chicken-and-egg the
// credstore-default client can't resolve.
//
// PAT-only for v1. Full OAuth lands in M3.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/GembaCore/gemba-core/internal/auth/credstore"
	"github.com/GembaCore/gemba-core/internal/cli/serverconfig"
	gembaclient "github.com/GembaCore/gemba-core/internal/client"
)

// whoamiResponse mirrors internal/server/whoami.go's wire shape.
type whoamiResponse struct {
	Subject       string `json:"subject"`
	ServerVersion string `json:"server_version"`
}

// errorEnvelope mirrors the server's auth/error envelope (see
// internal/auth/token.go writeUnauthorized + internal/server/httperr).
type errorEnvelope struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// whoamiCaller is the (server, token) → identity probe the login and
// whoami commands share. Injected so tests can pass an httptest.Server
// transport without standing up the full router.
type whoamiCaller func(ctx context.Context, server, token string) (whoamiResponse, error)

// defaultWhoamiCaller is the production whoamiCaller. Issues
// GET <server>/api/whoami with a bearer token; errors with the server's
// envelope text on non-2xx.
func defaultWhoamiCaller(client *http.Client) whoamiCaller {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return func(ctx context.Context, server, token string) (whoamiResponse, error) {
		url := strings.TrimRight(server, "/") + "/api/whoami"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return whoamiResponse{}, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return whoamiResponse{}, fmt.Errorf("call %s: %w", url, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg := parseErrorEnvelope(body, resp.StatusCode)
			return whoamiResponse{}, errors.New(msg)
		}
		var out whoamiResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return whoamiResponse{}, fmt.Errorf("decode response: %w", err)
		}
		return out, nil
	}
}

// typedClientWhoamiCaller is the production whoami probe for `gemba
// whoami` — built on the typed Go client (gm-o9t8.1.1.4). The factory
// returns a whoamiCaller (server, token) → identity so the runWhoami
// signature stays unchanged and the existing test suite continues to
// inject an httptest-backed caller.
//
// credPath threads through to internal/client.WithCredentialsPath so
// the --credentials-path flag still routes everything (login + whoami)
// to the same on-disk store.
func typedClientWhoamiCaller(credPath string) whoamiCaller {
	return func(ctx context.Context, server, token string) (whoamiResponse, error) {
		opts := []gembaclient.Option{gembaclient.WithToken(token)}
		if credPath != "" {
			opts = append(opts, gembaclient.WithCredentialsPath(credPath))
		}
		c, err := gembaclient.New(server, opts...)
		if err != nil {
			return whoamiResponse{}, err
		}
		resp, err := c.Whoami(ctx)
		if err != nil {
			return whoamiResponse{}, err
		}
		out := whoamiResponse{}
		if resp.Subject != "" {
			out.Subject = resp.Subject
		}
		if resp.ServerVersion != "" {
			out.ServerVersion = resp.ServerVersion
		}
		return out, nil
	}
}

// parseErrorEnvelope renders a server error response into a single
// line suitable for stderr.
func parseErrorEnvelope(body []byte, status int) string {
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && (env.Error != "" || env.Code != "" || env.Message != "") {
		parts := []string{}
		if env.Code != "" {
			parts = append(parts, env.Code)
		} else if env.Error != "" {
			parts = append(parts, env.Error)
		}
		if env.Message != "" {
			parts = append(parts, env.Message)
		}
		return fmt.Sprintf("server returned %d: %s", status, strings.Join(parts, ": "))
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return fmt.Sprintf("server returned %d", status)
	}
	return fmt.Sprintf("server returned %d: %s", status, trimmed)
}

// newLoginCmd builds the `gemba login` subcommand.
func newLoginCmd() *cobra.Command {
	var (
		server         string
		token          string
		tokenStdin     bool
		credPath       string
		useGitHub      bool
		githubClientID string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate the CLI to a gemba server",
		Long: `Authenticates the CLI to a gemba server and persists the resulting
credential to ~/.config/gemba/credentials.json (file mode 0600).

Two flows:

  --github            GitHub OAuth device flow (gm-o9t8.3.5.2). The CLI
                      prints a user code + verification URL, polls
                      GitHub for the access token, then exchanges it
                      with the gemba server for a gemba bearer.
                      Client id resolution: --github-client-id flag >
                      GEMBA_OAUTH_GITHUB_CLIENT_ID env.

  (default) PAT       Validate a personal access token against
                      <server>/api/whoami. Token sources, in priority
                      order:
                        --token <pat>      flag value
                        --token-stdin      read a single line from stdin
                        interactive prompt from /dev/tty with masking
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := serverconfig.Resolve(server)
			if err != nil {
				return err
			}
			path := credPath
			if path == "" {
				p, err := credstore.DefaultPath()
				if err != nil {
					return err
				}
				path = p
			}
			if useGitHub {
				clientID := strings.TrimSpace(githubClientID)
				if clientID == "" {
					clientID = strings.TrimSpace(os.Getenv("GEMBA_OAUTH_GITHUB_CLIENT_ID"))
				}
				cfg := githubDeviceFlowConfig{
					ClientID:    clientID,
					GembaServer: resolved,
				}
				return runGitHubLogin(cmd.Context(), cmd.OutOrStdout(),
					cmd.ErrOrStderr(), cfg, path)
			}
			tok, err := resolveLoginToken(cmd.InOrStdin(), cmd.OutOrStdout(), token, tokenStdin)
			if err != nil {
				return err
			}
			caller := defaultWhoamiCaller(nil)
			return runLogin(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(),
				resolved, tok, path, caller)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gemba server base URL (e.g. http://localhost:7666)")
	cmd.Flags().StringVar(&token, "token", "", "personal access token (avoid on shared shells)")
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "read the token from stdin")
	cmd.Flags().StringVar(&credPath, "credentials-path", "",
		"override the credentials file path (default: $XDG_CONFIG_HOME/gemba/credentials.json)")
	cmd.Flags().BoolVar(&useGitHub, "github", false,
		"authenticate via GitHub OAuth device flow (gm-o9t8.3.5.2)")
	cmd.Flags().StringVar(&githubClientID, "github-client-id", "",
		"GitHub OAuth app client id (default: $GEMBA_OAUTH_GITHUB_CLIENT_ID)")
	return cmd
}

// resolveLoginToken picks the token off --token, --token-stdin, or an
// interactive masked prompt — in that order.
func resolveLoginToken(in io.Reader, out io.Writer, flagTok string, fromStdin bool) (string, error) {
	if flagTok != "" {
		return strings.TrimSpace(flagTok), nil
	}
	if fromStdin {
		b, err := io.ReadAll(in)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		// Take the first non-empty line — token-stdin is a single
		// credential per call.
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				return line, nil
			}
		}
		return "", errors.New("--token-stdin produced an empty token")
	}
	return promptForTokenMasked(out)
}

// promptForTokenMasked reads a token from /dev/tty with echo disabled.
// Falls back to plain stdin read if /dev/tty isn't available.
func promptForTokenMasked(out io.Writer) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("open /dev/tty: %w (use --token or --token-stdin)", err)
	}
	defer tty.Close()
	_, _ = fmt.Fprint(out, "Enter personal access token: ")
	b, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", errors.New("empty token")
	}
	return tok, nil
}

// runLogin validates token against server then persists it to the
// credstore at path. Injected caller makes it testable.
func runLogin(ctx context.Context, out, stderr io.Writer, server, token, path string, caller whoamiCaller) error {
	resp, err := caller(ctx, server, token)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return fmt.Errorf("login: token rejected by %s", server)
	}
	store, err := credstore.Load(path)
	if err != nil {
		return fmt.Errorf("login: load credentials: %w", err)
	}
	cred := credstore.Credential{
		Token:    token,
		Subject:  resp.Subject,
		StoredAt: time.Now().UTC(),
	}
	if err := store.Put(server, cred); err != nil {
		return fmt.Errorf("login: persist credentials: %w", err)
	}
	fmt.Fprintf(out, "Logged in to %s as %s.\n", server, resp.Subject)
	return nil
}

// newWhoamiCmd builds the `gemba whoami` subcommand.
func newWhoamiCmd() *cobra.Command {
	var (
		server   string
		asJSON   bool
		credPath string
	)
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the identity the CLI is logged in as",
		Long: `Reads the persisted credential for <server> (or the only stored
server when --server is omitted) and calls <server>/api/whoami to
confirm the credential is still valid.

When --server is omitted and the credentials file holds exactly one
entry, that entry is used. With zero entries the command prints
"not logged in"; with multiple entries it asks the operator to pass
--server explicitly.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := credPath
			if path == "" {
				p, err := credstore.DefaultPath()
				if err != nil {
					return err
				}
				path = p
			}
			// Run the URL resolver first; if any explicit source (flag,
			// env, or config.toml) is set, that wins over the credstore's
			// "single stored server" auto-pick. With no explicit source
			// we hand "" to runWhoami so the credstore fallback still
			// works (a fresh login + whoami round-trip without ever
			// touching --server should keep working).
			detail, err := serverconfig.ResolveDetail(server)
			if err != nil {
				return err
			}
			chosen := ""
			if detail.Source != serverconfig.SourceDefault {
				chosen = detail.URL
			}
			caller := typedClientWhoamiCaller(path)
			return runWhoami(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(),
				chosen, path, asJSON, caller)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gemba server URL (defaults to the only stored server)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a human-readable line")
	cmd.Flags().StringVar(&credPath, "credentials-path", "",
		"override the credentials file path (default: $XDG_CONFIG_HOME/gemba/credentials.json)")
	return cmd
}

// whoamiJSON is the --json output shape.
type whoamiJSON struct {
	Server        string `json:"server"`
	Subject       string `json:"subject"`
	Authenticated bool   `json:"authenticated"`
}

// runWhoami is the testable entry point for `gemba whoami`.
func runWhoami(ctx context.Context, out, stderr io.Writer, server, path string, asJSON bool, caller whoamiCaller) error {
	store, err := credstore.Load(path)
	if err != nil {
		return fmt.Errorf("whoami: load credentials: %w", err)
	}
	chosen := server
	if chosen == "" {
		servers := store.ServerList()
		switch len(servers) {
		case 0:
			return printNotLoggedIn(out, asJSON, "")
		case 1:
			chosen = servers[0]
		default:
			return fmt.Errorf("whoami: multiple servers stored; pass --server (one of: %s)",
				strings.Join(servers, ", "))
		}
	}
	cred, ok := store.Get(chosen)
	if !ok {
		return printNotLoggedIn(out, asJSON, chosen)
	}
	resp, err := caller(ctx, chosen, cred.Token)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return fmt.Errorf("whoami: credential rejected by %s", chosen)
	}
	if asJSON {
		return json.NewEncoder(out).Encode(whoamiJSON{
			Server:        chosen,
			Subject:       resp.Subject,
			Authenticated: true,
		})
	}
	fmt.Fprintf(out, "%s @ %s\n", resp.Subject, chosen)
	return nil
}

func printNotLoggedIn(out io.Writer, asJSON bool, server string) error {
	if asJSON {
		return json.NewEncoder(out).Encode(whoamiJSON{
			Server:        server,
			Authenticated: false,
		})
	}
	fmt.Fprintln(out, "not logged in")
	return nil
}
