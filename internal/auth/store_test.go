package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteHash_CreatesFileWith0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "tokens", "primary")

	if err := WriteHash(path, "$argon2id$opaque"); err != nil {
		t.Fatalf("WriteHash: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("want 0600, got %o", got)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if got := parent.Mode().Perm(); got != 0o700 {
		t.Fatalf("want parent 0700, got %o", got)
	}
}

func TestReadHash_MissingFile_ReturnsEmptyNoError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope")
	hash, err := ReadHash(path)
	if err != nil {
		t.Fatalf("ReadHash: %v", err)
	}
	if hash != "" {
		t.Fatalf("want empty hash, got %q", hash)
	}
}

func TestWriteHash_OverwritesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "primary")
	if err := WriteHash(path, "first"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := WriteHash(path, "second"); err != nil {
		t.Fatalf("second: %v", err)
	}
	got, err := ReadHash(path)
	if err != nil {
		t.Fatalf("ReadHash: %v", err)
	}
	if got != "second" {
		t.Fatalf("want second, got %q", got)
	}
}
