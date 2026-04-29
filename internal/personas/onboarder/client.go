package onboarder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/skills/newproject"
)

// MissingClientDiagnostic is the operator-facing message the SPA
// renders on /new when the Onboarder fails to spawn because no chat
// client is configured. The wording is locked by the design doc; do
// not edit without amending docs/design/newproject.md §"Credential
// resolution".
const MissingClientDiagnostic = "No LLM client configured. Export ANTHROPIC_API_KEY in your environment, or set [llm] in ~/.gemba/config.toml, before starting a New project conversation."

// ErrNoLLMClient is the sentinel returned by [Spawn] (and the
// underlying [Resolve]) when no chat client is configured. The HTTP
// layer maps this onto a 503 with [MissingClientDiagnostic] in the
// body. The error string deliberately mirrors the diagnostic
// verbatim — HTTP handlers and the CLI both surface err.Error() to
// the operator unchanged, and the user-facing copy is a complete
// sentence ending in a period. The staticcheck lint exemption is
// intentional.
var ErrNoLLMClient = errors.New(MissingClientDiagnostic) //nolint:staticcheck // ST1005: user-facing diagnostic; surfaced verbatim by SPA + CLI

// Resolver hands back a [newproject.LLMClient] usable by the
// Onboarder. Tests inject fake resolvers; production code uses
// [DefaultResolver] which reads ~/.gemba/config.toml.
//
// Returning ErrNoLLMClient (or any error wrapping it) signals "no
// client configured" — [Spawn] surfaces that as a fail-spawn the
// SPA renders verbatim. Any other error is treated as an
// infrastructure failure (e.g. a malformed config.toml) and surfaced
// as a 500.
type Resolver func(ctx context.Context) (newproject.LLMClient, error)

// DefaultResolver builds a [Resolver] over the project's standard
// configuration layer. It reads ~/.gemba/config.toml's [llm] table
// (or the path pointed to by configPath when non-empty) and
// constructs a provider-specific [newproject.LLMClient] when the
// table is populated.
//
// Today only "anthropic" is wired; future providers add a switch
// arm here and an [LLMConfig] field as needed. An empty Provider in
// the config returns ErrNoLLMClient.
func DefaultResolver(configPath string) Resolver {
	return func(ctx context.Context) (newproject.LLMClient, error) {
		cfg, err := config.LoadUserConfig(configPath)
		if err != nil {
			return nil, fmt.Errorf("onboarder: load user config: %w", err)
		}
		return clientFromLLMConfig(cfg.LLM)
	}
}

// clientFromLLMConfig is the bare resolution logic, separated so
// tests can drive it without touching disk.
//
// Resolution order:
//
//  1. config.toml [llm].provider explicitly set → use that branch.
//  2. config.toml [llm].provider empty AND ANTHROPIC_API_KEY env var
//     set → implicit anthropic provider with the env-var key. This
//     lets an operator who already has ANTHROPIC_API_KEY exported
//     run `gemba serve` with no config.toml at all and still hit the
//     happy path. Credentials never touch disk.
//  3. Both empty → ErrNoLLMClient with the canonical diagnostic.
func clientFromLLMConfig(c config.LLMConfig) (newproject.LLMClient, error) {
	provider := strings.TrimSpace(strings.ToLower(c.Provider))
	switch provider {
	case "":
		// Implicit-anthropic via env var: the operator hasn't set up
		// config.toml [llm] but has exported ANTHROPIC_API_KEY in
		// their shell. Treat that as an opt-in to the anthropic
		// provider with sensible defaults — model and endpoint use
		// AnthropicClient's built-in defaults.
		if key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); key != "" {
			return NewAnthropicClient(AnthropicClientConfig{APIKey: key}), nil
		}
		return nil, ErrNoLLMClient
	case "anthropic":
		key := strings.TrimSpace(c.APIKey)
		if key == "" {
			// Soft fallback to the well-known env var the operator
			// almost certainly already has set if they're running a
			// Claude Code agent on the same machine. The empty-
			// provider branch above also reads this var so the
			// fallback covers both "explicit anthropic without
			// api_key" and "no provider at all" with the same env.
			key = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
		}
		if key == "" {
			return nil, ErrNoLLMClient
		}
		return NewAnthropicClient(AnthropicClientConfig{
			APIKey:   key,
			Model:    c.Model,
			Endpoint: c.Endpoint,
		}), nil
	default:
		return nil, fmt.Errorf("onboarder: unknown llm provider %q", c.Provider)
	}
}

// AnthropicClientConfig configures the bundled in-process Anthropic
// chat client. This is the only HTTP-backed implementation of
// [newproject.LLMClient] this package ships; the surface is narrow
// because [newproject.LLMClient] only requires a single
// system+user-prompt -> string call.
type AnthropicClientConfig struct {
	APIKey string
	// Model defaults to claude-3-5-sonnet-latest when empty. Locked
	// to the lowest-cost capable model at write time; operators with
	// stronger budgets override via config.toml.
	Model string
	// Endpoint defaults to https://api.anthropic.com/v1/messages
	// when empty. Tests inject a httptest.Server URL.
	Endpoint string
	// HTTPClient defaults to a net/http.Client with a 60s timeout.
	// Tests inject a fake client.
	HTTPClient *http.Client
	// MaxTokens caps the model's reply length. Defaults to 4096 —
	// large enough to fit a full plan-tree JSON envelope plus the
	// conversational reply.
	MaxTokens int
}

// AnthropicClient is a minimal Messages-API client implementing
// [newproject.LLMClient]. Lives in this package because it is the
// Onboarder persona's responsibility to resolve a chat client; if
// other personas need one in the future, lift this into a shared
// internal/llm package and re-import here.
type AnthropicClient struct {
	cfg AnthropicClientConfig
}

// NewAnthropicClient builds the client. Defaults applied:
//   - Model = "claude-3-5-sonnet-latest"
//   - Endpoint = "https://api.anthropic.com/v1/messages"
//   - HTTPClient = &http.Client{Timeout: 60 * time.Second}
//   - MaxTokens = 4096
func NewAnthropicClient(cfg AnthropicClientConfig) *AnthropicClient {
	if cfg.Model == "" {
		cfg.Model = "claude-3-5-sonnet-latest"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://api.anthropic.com/v1/messages"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	return &AnthropicClient{cfg: cfg}
}

// anthropicMessage is one turn in the Messages API request body.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicRequest is the wire shape for POST /v1/messages. We only
// use the system + single-user-message variant; the skill's
// conversation history lives inside the user prompt's rendered state
// JSON, not in a multi-turn message list.
type anthropicRequest struct {
	Model     string             `json:"model"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
	MaxTokens int                `json:"max_tokens"`
}

// anthropicResponse is the slice of the Messages-API response we
// care about — a list of content blocks the SDK joins. We
// concatenate text blocks; tool_use blocks (which the skill never
// asks for) are ignored.
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete implements [newproject.LLMClient] against the Anthropic
// Messages API.
func (c *AnthropicClient) Complete(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(anthropicRequest{
		Model:     c.cfg.Model,
		System:    system,
		Messages:  []anthropicMessage{{Role: "user", Content: user}},
		MaxTokens: c.cfg.MaxTokens,
	})
	if err != nil {
		return "", fmt.Errorf("onboarder/anthropic: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("onboarder/anthropic: build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("onboarder/anthropic: post: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("onboarder/anthropic: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Surface the upstream error type if we can decode it; the
		// raw body is the fallback so a non-JSON error page still
		// carries diagnostic value into the operator's logs.
		var parsed anthropicResponse
		if jerr := json.Unmarshal(raw, &parsed); jerr == nil && parsed.Error != nil {
			return "", fmt.Errorf("onboarder/anthropic: %d %s: %s",
				resp.StatusCode, parsed.Error.Type, parsed.Error.Message)
		}
		return "", fmt.Errorf("onboarder/anthropic: %d: %s",
			resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed anthropicResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("onboarder/anthropic: decode response: %w", err)
	}
	var sb strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	out := sb.String()
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("onboarder/anthropic: empty completion")
	}
	return out, nil
}
