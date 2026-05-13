package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func testMaster(t *testing.T) [MasterKeyLen]byte {
	t.Helper()
	k, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey: %v", err)
	}
	return k
}

func TestPutGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	master := testMaster(t)
	v, err := Open(dir, "ws-alpha", master)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := v.Put("ANTHROPIC_API_KEY", []byte("sk-secret")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := v.Inject("ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if !bytes.Equal(got, []byte("sk-secret")) {
		t.Fatalf("Inject mismatch: got %q want %q", got, "sk-secret")
	}
	// Reopening with the same master must preserve state.
	v2, err := Open(dir, "ws-alpha", master)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got2, err := v2.Inject("ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatalf("Inject after reopen: %v", err)
	}
	if !bytes.Equal(got2, []byte("sk-secret")) {
		t.Fatalf("reopened value mismatch: got %q want %q", got2, "sk-secret")
	}
}

func TestMasterKeyFromEnv(t *testing.T) {
	t.Setenv(MasterKeyEnv, "")
	if _, err := MasterKeyFromEnv(); err != ErrMasterKeyMissing {
		t.Fatalf("missing env: want %v, got %v", ErrMasterKeyMissing, err)
	}
	t.Setenv(MasterKeyEnv, "not-base-64-!!@@##")
	if _, err := MasterKeyFromEnv(); err != ErrMasterKeyMalformed {
		t.Fatalf("bad b64: want %v, got %v", ErrMasterKeyMalformed, err)
	}
	t.Setenv(MasterKeyEnv, "c2hvcnQ=") // "short" = 5 bytes
	if _, err := MasterKeyFromEnv(); err != ErrMasterKeyMalformed {
		t.Fatalf("wrong len: want %v, got %v", ErrMasterKeyMalformed, err)
	}
	good, _ := GenerateMasterKey()
	t.Setenv(MasterKeyEnv, EncodeMasterKey(good))
	got, err := MasterKeyFromEnv()
	if err != nil {
		t.Fatalf("good key: %v", err)
	}
	if got != good {
		t.Fatalf("round-trip mismatch")
	}
}

func TestListAndDelete(t *testing.T) {
	v, _ := Open(t.TempDir(), "ws-list", testMaster(t))
	if err := v.Put("A", []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := v.Put("B", []byte("2")); err != nil {
		t.Fatal(err)
	}
	names, err := v.List()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	if !equalSlices(names, []string{"A", "B"}) {
		t.Fatalf("List=%v, want [A B]", names)
	}
	if err := v.Delete("A"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Inject("A"); err != ErrNotFound {
		t.Fatalf("Inject after Delete: want ErrNotFound, got %v", err)
	}
	// Delete is idempotent.
	if err := v.Delete("A"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestPutIsolatedAcrossWorkspaces(t *testing.T) {
	master := testMaster(t)
	dir := t.TempDir()
	a, _ := Open(dir, "ws-A", master)
	b, _ := Open(dir, "ws-B", master)
	if err := a.Put("S", []byte("for-A")); err != nil {
		t.Fatal(err)
	}
	if err := b.Put("S", []byte("for-B")); err != nil {
		t.Fatal(err)
	}
	got, err := a.Inject("S")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("for-A")) {
		t.Fatalf("A.Inject got %q want for-A", got)
	}
	gotB, err := b.Inject("S")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotB, []byte("for-B")) {
		t.Fatalf("B.Inject got %q want for-B", gotB)
	}
}

func TestTamperedBlobDetected(t *testing.T) {
	dir := t.TempDir()
	master := testMaster(t)
	v, _ := Open(dir, "ws-tamper", master)
	if err := v.Put("X", []byte("xyz")); err != nil {
		t.Fatal(err)
	}
	// Flip a byte in the ciphertext (after nonce).
	p := filepath.Join(dir, "ws-tamper", "secrets.enc")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xFF
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	v2, _ := Open(dir, "ws-tamper", master)
	if _, err := v2.Inject("X"); err != ErrTampered {
		t.Fatalf("Inject after tamper: want ErrTampered, got %v", err)
	}
}

func TestCrossWorkspaceMasterKeyHasNoAccess(t *testing.T) {
	dir := t.TempDir()
	masterA := testMaster(t)
	v, _ := Open(dir, "ws-1", masterA)
	if err := v.Put("S", []byte("v")); err != nil {
		t.Fatal(err)
	}
	// Different master key => different derived workspace key => can't decrypt.
	masterB := testMaster(t)
	v2, err := Open(dir, "ws-1", masterB)
	if err != nil {
		t.Fatalf("Open with new master should succeed (no key check at open): %v", err)
	}
	_, err = v2.Inject("S")
	if err != ErrTampered {
		t.Fatalf("Inject under wrong master: want ErrTampered, got %v", err)
	}
}

func TestInjectReturnsCopy(t *testing.T) {
	v, _ := Open(t.TempDir(), "ws-cp", testMaster(t))
	if err := v.Put("S", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	a, err := v.Inject("S")
	if err != nil {
		t.Fatal(err)
	}
	a[0] = 'X'
	b, err := v.Inject("S")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, []byte("hello")) {
		t.Fatalf("Inject mutated stored value: got %q", b)
	}
}

func TestZero(t *testing.T) {
	b := []byte("secret")
	Zero(b)
	if !bytes.Equal(b, []byte{0, 0, 0, 0, 0, 0}) {
		t.Fatalf("Zero: %v", b)
	}
}

func TestOpenRequiresWorkspaceID(t *testing.T) {
	_, err := Open(t.TempDir(), "", testMaster(t))
	if err == nil || !strings.Contains(err.Error(), "workspaceID") {
		t.Fatalf("Open with empty ws id: %v", err)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
