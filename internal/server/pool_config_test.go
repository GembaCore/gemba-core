package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

func TestGetPoolConfig_NoAttachReturnsEmpty(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pool-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var env PoolConfigEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.Path != "" || env.Body != "" {
		t.Errorf("expected empty envelope, got %+v", env)
	}
}

func TestGetPoolConfig_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pool.toml")
	body := []byte(`
[pool]
default_size = 0
default_persona = "engineer-claude"

[pool.gemba.engineer-claude]
size = 3
floor = 0.5
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPoolConfig(path, 4, 1)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pool-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var env PoolConfigEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.Path != path {
		t.Errorf("path = %q want %q", env.Path, path)
	}
	if env.MaxParallel != 4 {
		t.Errorf("max_parallel = %d want 4", env.MaxParallel)
	}
	if !strings.Contains(env.Body, "default_persona") {
		t.Errorf("body missing default_persona: %q", env.Body)
	}
	if env.Parsed.DefaultPersona != "engineer-claude" {
		t.Errorf("parsed default_persona = %q", env.Parsed.DefaultPersona)
	}
	if env.Parsed.Pools["gemba"]["engineer-claude"].Size != 3 {
		t.Errorf("parsed pool size = %d", env.Parsed.Pools["gemba"]["engineer-claude"].Size)
	}
}

func TestPutPoolConfig_NonceGate(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPoolConfig(filepath.Join(t.TempDir(), "pool.toml"), 4, 1)

	body := bytes.NewBufferString(`{"default_size":0}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/pool-config", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing nonce should 400, got %d", rec.Code)
	}
}

func TestPutPoolConfig_WritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pool.toml")
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPoolConfig(path, 4, 1)

	body := bytes.NewBufferString(`{
  "default_size": 0,
  "default_persona": "engineer-claude",
  "default_floor": 0.5,
  "reserved_for_manual": 1,
  "routing": {"epic": "pm-claude"},
  "pools": {
    "gemba": {
      "engineer-claude": {
        "size": 2,
        "agent_type": "claude",
        "floor": 0.4,
        "recycle_after_beads": 0,
        "idle_ceiling_minutes": 0,
        "min_interval_per_session_seconds": 0,
        "max_concurrent": 0
      }
    }
  }
}`)
	req := httptest.NewRequest(http.MethodPut, "/api/pool-config", body)
	req.Header.Set(ConfirmHeader, "test-nonce-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", rec.Code, rec.Body.String())
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	got := string(written)
	if !strings.Contains(got, `default_persona = "engineer-claude"`) {
		t.Errorf("written body missing default_persona: %q", got)
	}
	if !strings.Contains(got, "[pool.gemba.engineer-claude]") {
		t.Errorf("written body missing pool block: %q", got)
	}
}

func TestPutPoolConfig_NoPathReturns400(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	// Intentionally do NOT call AttachPoolConfig.
	req := httptest.NewRequest(http.MethodPut, "/api/pool-config",
		bytes.NewBufferString(`{}`))
	req.Header.Set(ConfirmHeader, "n1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 path_not_configured", rec.Code)
	}
}

func TestPutPoolConfig_InvalidJSONRejected(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPoolConfig(filepath.Join(t.TempDir(), "pool.toml"), 4, 1)
	req := httptest.NewRequest(http.MethodPut, "/api/pool-config",
		bytes.NewBufferString(`{not json`))
	req.Header.Set(ConfirmHeader, "n2")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 invalid_body", rec.Code)
	}
}
