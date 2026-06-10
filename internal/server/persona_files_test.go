package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

func TestListPersonaFiles_MissingDirReturnsEmpty(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPersonasDir(filepath.Join(t.TempDir(), "does-not-exist"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/personas", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Personas []PersonaFile `json:"personas"`
		Missing  bool          `json:"missing"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Missing {
		t.Errorf("missing flag should be set")
	}
	if len(body.Personas) != 0 {
		t.Errorf("expected 0 personas, got %d", len(body.Personas))
	}
}

func TestListPersonaFiles_EnumeratesFixtures(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "engineer-claude.toml"), `
name = "Engineer Claude"
agent_type = "claude"
skills = ["code-review", "refactor"]
`)
	mustWrite(t, filepath.Join(dir, "pm-claude.toml"), `
[persona]
name = "PM Claude"
agent_type = "claude"
`)
	// Non-toml file should be ignored.
	mustWrite(t, filepath.Join(dir, "README.md"), "noise")

	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachPersonasDir(dir)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/personas", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Personas []PersonaFile `json:"personas"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Personas) != 2 {
		t.Fatalf("want 2 personas, got %d (%+v)", len(body.Personas), body.Personas)
	}
	// Sorted alphabetically — engineer-claude first.
	if body.Personas[0].ID != "engineer-claude" {
		t.Errorf("[0].ID = %q want engineer-claude", body.Personas[0].ID)
	}
	if body.Personas[0].Name != "Engineer Claude" {
		t.Errorf("[0].Name = %q", body.Personas[0].Name)
	}
	if body.Personas[0].AgentType != "claude" {
		t.Errorf("[0].AgentType = %q", body.Personas[0].AgentType)
	}
	if len(body.Personas[0].Skills) != 2 {
		t.Errorf("[0].Skills = %v", body.Personas[0].Skills)
	}
	if body.Personas[1].ID != "pm-claude" {
		t.Errorf("[1].ID = %q want pm-claude", body.Personas[1].ID)
	}
	if body.Personas[1].Name != "PM Claude" {
		t.Errorf("[1].Name = %q (expected [persona].name path)", body.Personas[1].Name)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
