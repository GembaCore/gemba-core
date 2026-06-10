package credstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// gm-o9t8.1.1.2: a fresh Load of a non-existent file returns an empty,
// writable store. Put + reload round-trips the credential intact.
func TestRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "credentials.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(s.Servers); got != 0 {
		t.Fatalf("fresh store: want 0 servers, got %d", got)
	}

	cred := Credential{
		Token:    "tok-abc",
		Subject:  "operator",
		StoredAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.Put("http://localhost:7666", cred); err != nil {
		t.Fatalf("Put: %v", err)
	}

	s2, err := Load(path)
	if err != nil {
		t.Fatalf("reload Load: %v", err)
	}
	got, ok := s2.Get("http://localhost:7666")
	if !ok {
		t.Fatalf("Get: server missing after reload")
	}
	if got.Token != cred.Token || got.Subject != cred.Subject {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, cred)
	}
	if !got.StoredAt.Equal(cred.StoredAt) {
		t.Fatalf("stored_at mismatch: got %v want %v", got.StoredAt, cred.StoredAt)
	}
}

// gm-o9t8.1.1.2 AC: file mode 0600 after Put.
func TestPut_EnforcesFileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits not meaningful on Windows")
	}
	path := filepath.Join(t.TempDir(), "gemba", "credentials.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := s.Put("http://localhost:7666", Credential{Token: "t"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode: want 0600, got %#o", mode)
	}
	// Parent dir should be 0700.
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat parent: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("parent dir mode: want 0700, got %#o", mode)
	}
}

// gm-o9t8.1.1.2: missing parent directory is auto-created.
func TestPut_AutoCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "nest", "credentials.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := s.Put("http://x", Credential{Token: "t"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}

// gm-o9t8.1.1.2: Delete removes the entry and is idempotent.
func TestDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	s, _ := Load(path)
	_ = s.Put("http://a", Credential{Token: "ta"})
	_ = s.Put("http://b", Credential{Token: "tb"})

	if err := s.Delete("http://a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get("http://a"); ok {
		t.Fatalf("Get(a): want missing")
	}
	if _, ok := s.Get("http://b"); !ok {
		t.Fatalf("Get(b): want present")
	}
	// Idempotent.
	if err := s.Delete("http://a"); err != nil {
		t.Fatalf("Delete idempotent: %v", err)
	}
}

// gm-o9t8.1.1.2: malformed JSON surfaces as an error rather than a
// silent reset.
func TestLoad_MalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatalf("want error from malformed JSON, got nil")
	}
}

// gm-o9t8.1.1.2: atomic write doesn't leave the destination corrupt.
// We prove the temp-file-then-rename pattern by writing once, then
// asserting that no stray temp files survive in the dir.
func TestPut_AtomicNoLeftoverTemps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	s, _ := Load(path)
	if err := s.Put("http://a", Credential{Token: "t"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".credentials-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

// gm-o9t8.1.1.2: existing valid file is read back into the in-memory
// store, then a second Put preserves the prior entries.
func TestPut_PreservesOtherServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	s, _ := Load(path)
	_ = s.Put("http://a", Credential{Token: "ta", Subject: "alice"})

	s2, _ := Load(path)
	_ = s2.Put("http://b", Credential{Token: "tb", Subject: "bob"})

	final, _ := Load(path)
	if _, ok := final.Get("http://a"); !ok {
		t.Fatalf("server a lost after second Put")
	}
	if _, ok := final.Get("http://b"); !ok {
		t.Fatalf("server b missing")
	}
}

// gm-o9t8.1.1.2: stored on-disk JSON has the documented shape.
func TestPut_OnDiskShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	s, _ := Load(path)
	_ = s.Put("http://a", Credential{Token: "ta", Subject: "alice", StoredAt: time.Unix(1700000000, 0).UTC()})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got struct {
		Servers map[string]Credential `json:"servers"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	c, ok := got.Servers["http://a"]
	if !ok || c.Token != "ta" || c.Subject != "alice" {
		t.Fatalf("on-disk shape mismatch: %s", raw)
	}
}

// gm-o9t8.1.1.2: DefaultPath honours XDG_CONFIG_HOME.
func TestDefaultPath_XDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join("/tmp/xdg", "gemba", "credentials.json")
	if got != want {
		t.Fatalf("DefaultPath: got %s want %s", got, want)
	}
}

// gm-o9t8.1.1.2: DefaultPath falls back to $HOME/.config when XDG is unset.
func TestDefaultPath_HomeFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/fakehome")
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("gemba", "credentials.json")) {
		t.Fatalf("DefaultPath: got %s", got)
	}
}
