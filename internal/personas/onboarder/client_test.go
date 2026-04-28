package onboarder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
)

// TestClientFromLLMConfig_NoProvider_NoEnv: empty provider AND no
// ANTHROPIC_API_KEY env var → no-client diagnostic. This is the
// primary "operator hasn't configured credentials" path.
func TestClientFromLLMConfig_NoProvider_NoEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := clientFromLLMConfig(config.LLMConfig{})
	if !IsNoClient(err) {
		t.Errorf("expected ErrNoLLMClient on empty provider + empty env; got %v", err)
	}
}

// TestClientFromLLMConfig_NoProvider_EnvImplicitAnthropic: empty
// provider but ANTHROPIC_API_KEY is exported → implicit anthropic
// client. Lets `gemba serve` work with zero config.toml when the
// operator already has the env var set.
func TestClientFromLLMConfig_NoProvider_EnvImplicitAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-implicit")
	c, err := clientFromLLMConfig(config.LLMConfig{})
	if err != nil {
		t.Fatalf("expected implicit-anthropic client; got %v", err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

// TestClientFromLLMConfig_AnthropicNoKey: provider set but no
// api_key + no env var → no-client diagnostic. The fallback to
// ANTHROPIC_API_KEY happens here; we unset it so the test is
// hermetic.
func TestClientFromLLMConfig_AnthropicNoKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := clientFromLLMConfig(config.LLMConfig{Provider: "anthropic"})
	if !IsNoClient(err) {
		t.Errorf("expected ErrNoLLMClient when api_key + env var both absent; got %v", err)
	}
}

// TestClientFromLLMConfig_AnthropicEnvFallback: provider set, no
// config api_key, env var present → returns a working client.
func TestClientFromLLMConfig_AnthropicEnvFallback(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-from-env")
	c, err := clientFromLLMConfig(config.LLMConfig{Provider: "anthropic"})
	if err != nil {
		t.Fatalf("expected working client; got %v", err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

// TestClientFromLLMConfig_AnthropicHappy: explicit api_key in
// config → working client.
func TestClientFromLLMConfig_AnthropicHappy(t *testing.T) {
	t.Parallel()
	c, err := clientFromLLMConfig(config.LLMConfig{
		Provider: "anthropic",
		APIKey:   "sk-test",
		Model:    "claude-3-5-haiku-latest",
	})
	if err != nil {
		t.Fatalf("expected working client; got %v", err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

// TestClientFromLLMConfig_UnknownProvider: an unrecognized provider
// surfaces a non-diagnostic error so the operator sees their typo.
func TestClientFromLLMConfig_UnknownProvider(t *testing.T) {
	t.Parallel()
	_, err := clientFromLLMConfig(config.LLMConfig{
		Provider: "definitely-not-a-real-provider",
	})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if IsNoClient(err) {
		t.Errorf("unknown-provider error misclassified as no-client: %v", err)
	}
}

// TestDefaultResolver_MissingConfig: a resolver pointed at a
// missing file gets the zero-value UserConfig, which has no
// provider, which surfaces the no-client diagnostic.
func TestDefaultResolver_MissingConfig(t *testing.T) {
	t.Parallel()
	r := DefaultResolver("/definitely/not/a/real/path/config.toml")
	_, err := r(context.Background())
	if !IsNoClient(err) {
		t.Errorf("expected no-client diagnostic for missing config; got %v", err)
	}
}

// TestDefaultResolver_ParsesLLMTable: a config.toml with [llm] +
// api_key produces a working client.
func TestDefaultResolver_ParsesLLMTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[llm]
provider = "anthropic"
api_key = "sk-test"
model = "claude-3-5-haiku-latest"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r := DefaultResolver(path)
	c, err := r(context.Background())
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

// TestAnthropicClient_Complete: round-trip a Messages-API request
// through a httptest server, verifying the wire shape (headers,
// body keys) and the response decoder.
func TestAnthropicClient_Complete(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		capturedHeaders = req.Header.Clone()
		capturedBody, _ = io.ReadAll(req.Body)
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{
			"content": [
				{"type": "text", "text": "hello "},
				{"type": "text", "text": "world"}
			]
		}`)
	}))
	defer srv.Close()

	c := NewAnthropicClient(AnthropicClientConfig{
		APIKey:   "sk-test",
		Endpoint: srv.URL,
		Model:    "claude-test",
	})
	out, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "hello world" {
		t.Errorf("Complete: got %q, want %q", out, "hello world")
	}

	// Body shape: model + system + messages + max_tokens.
	var req anthropicRequest
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("decode captured body: %v", err)
	}
	if req.Model != "claude-test" {
		t.Errorf("Model: got %q, want claude-test", req.Model)
	}
	if req.System != "sys" {
		t.Errorf("System: got %q", req.System)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "user" {
		t.Errorf("Messages: got %+v", req.Messages)
	}
	if req.MaxTokens <= 0 {
		t.Errorf("MaxTokens: got %d, want > 0", req.MaxTokens)
	}

	// Headers: x-api-key + anthropic-version present.
	if got := capturedHeaders.Get("x-api-key"); got != "sk-test" {
		t.Errorf("x-api-key: got %q, want sk-test", got)
	}
	if capturedHeaders.Get("anthropic-version") == "" {
		t.Error("anthropic-version header missing")
	}
}

// TestAnthropicClient_Non2xxSurfacesBody: a non-2xx response carries
// the upstream error message into the returned error.
func TestAnthropicClient_Non2xxSurfacesBody(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error": {"type": "authentication_error", "message": "invalid x-api-key"}}`)
	}))
	defer srv.Close()

	c := NewAnthropicClient(AnthropicClientConfig{
		APIKey:   "sk-bad",
		Endpoint: srv.URL,
	})
	_, err := c.Complete(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "authentication_error") {
		t.Errorf("expected upstream error type in message; got %q", err.Error())
	}
}

// TestAnthropicClient_EmptyCompletionFails: a 200 with no text
// blocks is still an error so the skill doesn't see ""→ valid
// state.
func TestAnthropicClient_EmptyCompletionFails(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"content": []}`)
	}))
	defer srv.Close()

	c := NewAnthropicClient(AnthropicClientConfig{APIKey: "x", Endpoint: srv.URL})
	_, err := c.Complete(context.Background(), "s", "u")
	if err == nil {
		t.Fatal("expected error on empty completion")
	}
}
