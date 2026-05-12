package audit

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return priv
}

func TestAppendAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	priv := testKey(t)
	l, err := Open(path, priv)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	r1, err := l.Append(context.Background(), "ws-1", "run-A", KindFileWrite, map[string]string{"file": "a.txt"})
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if r1.Seq != 1 {
		t.Fatalf("seq=%d want 1", r1.Seq)
	}
	if r1.PrevHash != "" {
		t.Fatalf("first prev_hash should be empty, got %q", r1.PrevHash)
	}
	r2, err := l.Append(context.Background(), "ws-1", "run-A", KindCommandRun, map[string]any{"cmd": "ls"})
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if r2.Seq != 2 {
		t.Fatalf("seq=%d want 2", r2.Seq)
	}
	if r2.PrevHash == "" {
		t.Fatalf("second prev_hash should be set")
	}

	all, err := l.Read(0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("Read returned %d records, want 2", len(all))
	}
	from1, err := l.Read(1)
	if err != nil {
		t.Fatalf("Read(1): %v", err)
	}
	if len(from1) != 1 || from1[0].Seq != 2 {
		t.Fatalf("Read(1) = %v", from1)
	}
}

func TestVerifyChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	priv := testKey(t)
	l, _ := Open(path, priv)
	for i := 0; i < 5; i++ {
		if _, err := l.Append(context.Background(), "ws", "run", KindFileWrite, map[string]int{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	recs, _ := l.Read(0)
	if err := Verify(recs, l.PublicKey()); err != nil {
		t.Fatalf("Verify clean chain: %v", err)
	}

	// Tamper a payload after the fact and re-verify — should fail.
	recs[2].Payload = json.RawMessage(`{"tampered":true}`)
	if err := Verify(recs, l.PublicKey()); err == nil {
		t.Fatalf("Verify tampered chain: want error, got nil")
	}
}

func TestReopenContinuesChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	priv := testKey(t)
	l, _ := Open(path, priv)
	r1, _ := l.Append(context.Background(), "ws", "run", KindFileWrite, "a")
	r2, _ := l.Append(context.Background(), "ws", "run", KindFileWrite, "b")
	// Reopen.
	l2, err := Open(path, priv)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	r3, err := l2.Append(context.Background(), "ws", "run", KindFileWrite, "c")
	if err != nil {
		t.Fatalf("Append after reopen: %v", err)
	}
	if r3.Seq != 3 {
		t.Fatalf("seq=%d after reopen, want 3", r3.Seq)
	}
	if r3.PrevHash == "" {
		t.Fatalf("prev_hash empty after reopen")
	}
	// Whole chain still verifies.
	recs, _ := l2.Read(0)
	if len(recs) != 3 {
		t.Fatalf("want 3 records, got %d (%v %v)", len(recs), r1, r2)
	}
	if err := Verify(recs, l2.PublicKey()); err != nil {
		t.Fatalf("Verify across reopen: %v", err)
	}
}

func TestTailEmitsAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	priv := testKey(t)
	l, _ := Open(path, priv)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := l.Tail(ctx, 0, 20*time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(1)
	got := 0
	go func() {
		defer wg.Done()
		for range ch {
			got++
			if got == 3 {
				cancel()
				return
			}
		}
	}()
	for i := 0; i < 3; i++ {
		if _, err := l.Append(context.Background(), "ws", "r", KindFileWrite, i); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		time.Sleep(40 * time.Millisecond)
	}
	wg.Wait()
	if got != 3 {
		t.Fatalf("Tail got %d records, want 3", got)
	}
}

func TestKeyFromEnvRoundTrip(t *testing.T) {
	_, priv, _ := GenerateKey()
	t.Setenv(KeyEnv, EncodeSeed(priv))
	got, err := KeyFromEnv()
	if err != nil {
		t.Fatalf("KeyFromEnv: %v", err)
	}
	if string(got.Seed()) != string(priv.Seed()) {
		t.Fatalf("seed mismatch")
	}
}

func TestKeyFromEnvBadInput(t *testing.T) {
	t.Setenv(KeyEnv, "")
	if _, err := KeyFromEnv(); err == nil || !strings.Contains(err.Error(), "not set") {
		t.Fatalf("empty env: %v", err)
	}
	t.Setenv(KeyEnv, "!!!not-base-64!!!")
	if _, err := KeyFromEnv(); err == nil {
		t.Fatalf("garbage env: want error")
	}
	t.Setenv(KeyEnv, "c2hvcnQ=")
	if _, err := KeyFromEnv(); err == nil {
		t.Fatalf("short seed: want error")
	}
}

func TestPayloadAcceptsAnyForm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	l, _ := Open(path, testKey(t))
	if _, err := l.Append(context.Background(), "ws", "r", KindFileWrite, []byte(`{"raw":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(context.Background(), "ws", "r", KindFileWrite, json.RawMessage(`{"rawmsg":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(context.Background(), "ws", "r", KindFileWrite, map[string]int{"struct": 1}); err != nil {
		t.Fatal(err)
	}
}

func TestFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	l, _ := Open(path, testKey(t))
	if _, err := l.Append(context.Background(), "ws", "r", KindFileWrite, 1); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("log perm = %o want 600", st.Mode().Perm())
	}
}
