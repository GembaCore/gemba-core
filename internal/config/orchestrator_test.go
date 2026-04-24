package config_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadOrchestratorConfig_Gastown(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "orchestrator.json", `{
		"orchestrator": "gastown",
		"config": {
			"rig": "gemba",
			"rig_abbr": "gm",
			"kind_prefixes": {"bug": "BUG: ", "task": ""}
		}
	}`)

	oc, err := config.LoadOrchestratorConfig(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if oc.Orchestrator != "gastown" {
		t.Errorf("Orchestrator = %q, want gastown", oc.Orchestrator)
	}

	var gcfg config.GastownShaderConfig
	if err := json.Unmarshal(oc.Config, &gcfg); err != nil {
		t.Fatalf("decode gastown: %v", err)
	}
	if gcfg.Rig != "gemba" || gcfg.RigAbbr != "gm" {
		t.Errorf("unexpected gastown config: %+v", gcfg)
	}
	if gcfg.KindPrefixes["bug"] != "BUG: " {
		t.Errorf("kind_prefixes[bug] = %q", gcfg.KindPrefixes["bug"])
	}
}

func TestLoadOrchestratorConfig_MissingFile_IsErrNotExist(t *testing.T) {
	_, err := config.LoadOrchestratorConfig(filepath.Join(t.TempDir(), "nope.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: want os.ErrNotExist, got %v", err)
	}
}

func TestLoadOrchestratorConfig_RejectsMissingOrchestrator(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "bad.json", `{"config":{}}`)
	_, err := config.LoadOrchestratorConfig(path)
	if err == nil {
		t.Fatal("want error when orchestrator field is empty")
	}
}

func TestResolveOrchestratorConfigPath_OverrideAbsolute(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "custom.json", `{"orchestrator":"nop"}`)
	got := config.ResolveOrchestratorConfigPath(path, "/tmp")
	if got != path {
		t.Errorf("override absolute: got %q, want %q", got, path)
	}
}

func TestResolveOrchestratorConfigPath_OverrideRelative(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "custom.json", `{"orchestrator":"nop"}`)
	got := config.ResolveOrchestratorConfigPath("custom.json", dir)
	if got != filepath.Join(dir, "custom.json") {
		t.Errorf("override relative: got %q", got)
	}
}

func TestResolveOrchestratorConfigPath_DefaultProbe(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gemba/orchestrator.json", `{"orchestrator":"nop"}`)
	got := config.ResolveOrchestratorConfigPath("", dir)
	if got != filepath.Join(dir, ".gemba/orchestrator.json") {
		t.Errorf("default probe: got %q", got)
	}
}

func TestResolveOrchestratorConfigPath_MissingReturnsEmpty(t *testing.T) {
	got := config.ResolveOrchestratorConfigPath("", t.TempDir())
	if got != "" {
		t.Errorf("missing file: want empty, got %q", got)
	}
}
