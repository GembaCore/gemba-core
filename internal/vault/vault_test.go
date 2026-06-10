// Vault contract tests — the eight cases enumerated on
// gm-o9t8.3.7. Each test constructs a fresh bolt file under t.TempDir()
// so the suite is hermetic.

package vault

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

// newTestVault builds a fresh vault rooted at a unique temp file and
// returns it alongside the on-disk path and KEK so persistence tests
// can reopen. The caller is responsible for closing.
func newTestVault(t *testing.T) (Vault, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	kek := make([]byte, kekSize)
	if _, err := io.ReadFull(rand.Reader, kek); err != nil {
		t.Fatalf("seed kek: %v", err)
	}
	v, err := New(Options{Path: path, KEK: kek})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if c, ok := v.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	})
	return v, path, kek
}

func TestRoundtrip_PutGet(t *testing.T) {
	v, _, _ := newTestVault(t)
	ctx := context.Background()
	want := []byte("super-secret")
	if err := v.Put(ctx, "ws1", "API_KEY", want); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := v.Get(ctx, "ws1", "API_KEY")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("get = %q, want %q", got, want)
	}
}

func TestDelete_Idempotent(t *testing.T) {
	v, _, _ := newTestVault(t)
	ctx := context.Background()
	if err := v.Put(ctx, "ws1", "K", []byte("v")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := v.Delete(ctx, "ws1", "K"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Second delete on the same key must succeed (idempotent).
	if err := v.Delete(ctx, "ws1", "K"); err != nil {
		t.Fatalf("delete (idempotent): %v", err)
	}
	// Delete on a key that never existed must also succeed.
	if err := v.Delete(ctx, "ws1", "NEVER_EXISTED"); err != nil {
		t.Fatalf("delete (missing): %v", err)
	}
	// And the value really is gone.
	if _, err := v.Get(ctx, "ws1", "K"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
}

func TestList_KeysOnly(t *testing.T) {
	v, _, _ := newTestVault(t)
	ctx := context.Background()
	for _, kv := range [][2]string{{"A", "1"}, {"B", "2"}, {"C", "3"}} {
		if err := v.Put(ctx, "ws1", kv[0], []byte(kv[1])); err != nil {
			t.Fatalf("put %s: %v", kv[0], err)
		}
	}
	keys, err := v.List(ctx, "ws1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"A", "B", "C"}
	if len(keys) != len(want) {
		t.Fatalf("list = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("list[%d] = %q, want %q", i, keys[i], want[i])
		}
		// Defense-in-depth: no value bytes should ever appear in
		// the returned keys slice.
		for _, leak := range []string{"1", "2", "3"} {
			if keys[i] == leak {
				t.Fatalf("list leaked a value: %q", keys[i])
			}
		}
	}
}

func TestInject_EnvMap(t *testing.T) {
	v, _, _ := newTestVault(t)
	ctx := context.Background()
	pairs := map[string]string{
		"FOO":    "bar",
		"DB_URL": "postgres://x/y",
	}
	for k, val := range pairs {
		if err := v.Put(ctx, "ws1", k, []byte(val)); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}
	env, err := v.Inject(ctx, "ws1")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if len(env) != len(pairs) {
		t.Fatalf("inject len = %d, want %d", len(env), len(pairs))
	}
	for k, val := range pairs {
		if env[k] != val {
			t.Fatalf("inject[%s] = %q, want %q", k, env[k], val)
		}
	}
}

func TestWrongKEK_FailsDecrypt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	good := make([]byte, kekSize)
	if _, err := io.ReadFull(rand.Reader, good); err != nil {
		t.Fatalf("seed: %v", err)
	}
	v, err := New(Options{Path: path, KEK: good})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := v.Put(ctx, "ws1", "TOKEN", []byte("hunter2")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if c, ok := v.(interface{ Close() error }); ok {
		_ = c.Close()
	}

	bad := make([]byte, kekSize)
	if _, err := io.ReadFull(rand.Reader, bad); err != nil {
		t.Fatalf("seed bad: %v", err)
	}
	v2, err := New(Options{Path: path, KEK: bad})
	if err != nil {
		t.Fatalf("reopen with bad KEK: %v", err)
	}
	defer func() {
		if c, ok := v2.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}()
	if _, err := v2.Get(ctx, "ws1", "TOKEN"); err == nil {
		t.Fatal("Get with wrong KEK succeeded; expected decrypt error")
	}
}

func TestWorkspaceIsolation(t *testing.T) {
	v, _, _ := newTestVault(t)
	ctx := context.Background()
	if err := v.Put(ctx, "wsA", "KEY", []byte("A-secret")); err != nil {
		t.Fatalf("put A: %v", err)
	}
	if err := v.Put(ctx, "wsB", "KEY", []byte("B-secret")); err != nil {
		t.Fatalf("put B: %v", err)
	}
	gotA, err := v.Get(ctx, "wsA", "KEY")
	if err != nil {
		t.Fatalf("get A: %v", err)
	}
	gotB, err := v.Get(ctx, "wsB", "KEY")
	if err != nil {
		t.Fatalf("get B: %v", err)
	}
	if string(gotA) != "A-secret" {
		t.Fatalf("wsA = %q, want A-secret", gotA)
	}
	if string(gotB) != "B-secret" {
		t.Fatalf("wsB = %q, want B-secret", gotB)
	}
	// And listing wsA must not reveal wsB's keys.
	keysA, err := v.List(ctx, "wsA")
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(keysA) != 1 || keysA[0] != "KEY" {
		t.Fatalf("wsA keys = %v, want [KEY]", keysA)
	}
}

func TestPersistence_AcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	kek := make([]byte, kekSize)
	if _, err := io.ReadFull(rand.Reader, kek); err != nil {
		t.Fatalf("seed: %v", err)
	}
	v1, err := New(Options{Path: path, KEK: kek})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := v1.Put(ctx, "ws1", "API_KEY", []byte("persist-me")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if c, ok := v1.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}

	v2, err := New(Options{Path: path, KEK: kek})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() {
		if c, ok := v2.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}()
	got, err := v2.Get(ctx, "ws1", "API_KEY")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if string(got) != "persist-me" {
		t.Fatalf("get = %q, want persist-me", got)
	}
}

func TestGet_ErrNotFound(t *testing.T) {
	v, _, _ := newTestVault(t)
	ctx := context.Background()
	// Workspace doesn't exist yet.
	if _, err := v.Get(ctx, "wsZ", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing wsid = %v, want ErrNotFound", err)
	}
	// Workspace exists (DEK created via a Put) but key is absent.
	if err := v.Put(ctx, "ws1", "OTHER", []byte("x")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := v.Get(ctx, "ws1", "absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing key = %v, want ErrNotFound", err)
	}
}

func TestAuditor_Hook(t *testing.T) {
	// Auditor records every non-system action so the audit-impl
	// subagent can drop in a real implementation without breaking
	// this contract.
	var calls []string
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	kek := make([]byte, kekSize)
	if _, err := io.ReadFull(rand.Reader, kek); err != nil {
		t.Fatalf("seed: %v", err)
	}
	v, err := New(Options{
		Path: path,
		KEK:  kek,
		Auditor: func(action, wsid, key string) {
			calls = append(calls, action+":"+wsid+":"+key)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if c, ok := v.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}()
	ctx := context.Background()
	if err := v.Put(ctx, "ws1", "K", []byte("v")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := v.Get(ctx, "ws1", "K"); err != nil {
		t.Fatalf("get: %v", err)
	}
	// System Get must NOT audit (this is the Inject path).
	if _, err := v.Get(WithSystemActor(ctx), "ws1", "K"); err != nil {
		t.Fatalf("system get: %v", err)
	}
	if err := v.Delete(ctx, "ws1", "K"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	want := []string{"put:ws1:K", "get:ws1:K", "delete:ws1:K"}
	if len(calls) != len(want) {
		t.Fatalf("audit calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("call[%d] = %q, want %q", i, calls[i], want[i])
		}
	}
}
