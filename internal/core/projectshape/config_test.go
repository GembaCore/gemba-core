package projectshape

import (
	"errors"
	"path/filepath"
	"testing"

	"os"
)

func writeTempFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestLoadConfig_HappyPath(t *testing.T) {
	path := writeTempFile(t, `
[project_shape]
active = "two-tier-spa"
`)
	got, err := LoadConfig(path, DefaultRegistry())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Name != "two-tier-spa" {
		t.Errorf("LoadConfig got name %q, want two-tier-spa", got.Name)
	}
}

func TestLoadConfig_MissingFileReturnsGenericNoError(t *testing.T) {
	// gm-4uso contract: a workspace without a project_shape.toml
	// MUST NOT error out — the dispatcher carries on with the
	// generic (zero) shape.
	path := filepath.Join(t.TempDir(), "definitely-not-here.toml")
	got, err := LoadConfig(path, DefaultRegistry())
	if err != nil {
		t.Fatalf("LoadConfig on missing file returned error: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("missing file should yield zero Shape, got %+v", got)
	}
}

func TestLoadConfig_EmptyFileReturnsGenericNoError(t *testing.T) {
	path := writeTempFile(t, "")
	got, err := LoadConfig(path, DefaultRegistry())
	if err != nil {
		t.Fatalf("LoadConfig empty file: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("empty file should yield zero Shape, got %+v", got)
	}
}

func TestLoadConfig_BlockPresentButActiveEmptyReturnsGeneric(t *testing.T) {
	path := writeTempFile(t, `
[project_shape]
active = ""
`)
	got, err := LoadConfig(path, DefaultRegistry())
	if err != nil {
		t.Fatalf("LoadConfig empty active: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("empty active should yield zero Shape, got %+v", got)
	}
}

func TestLoadConfig_UnknownShapeNameReturnsTypedError(t *testing.T) {
	path := writeTempFile(t, `
[project_shape]
active = "does-not-exist"
`)
	_, err := LoadConfig(path, DefaultRegistry())
	if err == nil {
		t.Fatal("LoadConfig with unknown shape returned nil error")
	}
	if !errors.Is(err, ErrUnknownShape) {
		t.Errorf("error not Is(ErrUnknownShape): %v", err)
	}
}

func TestLoadConfig_MalformedTOMLReturnsError(t *testing.T) {
	path := writeTempFile(t, `this is not = valid = toml [[[`)
	_, err := LoadConfig(path, DefaultRegistry())
	if err == nil {
		t.Fatal("LoadConfig with malformed TOML returned nil error")
	}
}

func TestLoadConfig_NilRegistryRejected(t *testing.T) {
	path := writeTempFile(t, "")
	_, err := LoadConfig(path, nil)
	if err == nil {
		t.Fatal("LoadConfig with nil registry returned nil error")
	}
}
