package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/auth"
)

func TestTokenRotate_WritesHashAndPrintsToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "primary")
	var out, stderr bytes.Buffer
	if err := runTokenRotate(&out, &stderr, path); err != nil {
		t.Fatalf("runTokenRotate: %v", err)
	}
	tok := strings.TrimSpace(out.String())
	if len(tok) != 64 {
		t.Fatalf("want 64-char hex token, got %q", tok)
	}

	hash, err := auth.ReadHash(path)
	if err != nil {
		t.Fatalf("ReadHash: %v", err)
	}
	if hash == "" {
		t.Fatal("expected hash file to be written")
	}
	ok, err := auth.VerifyHash(tok, hash)
	if err != nil {
		t.Fatalf("VerifyHash: %v", err)
	}
	if !ok {
		t.Fatal("printed token does not verify against stored hash")
	}
}

func TestTokenRotate_TwiceProducesDifferentTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "primary")
	var out bytes.Buffer
	stderr := &bytes.Buffer{}
	if err := runTokenRotate(&out, stderr, path); err != nil {
		t.Fatalf("first: %v", err)
	}
	first := strings.TrimSpace(out.String())
	out.Reset()
	stderr.Reset()
	if err := runTokenRotate(&out, stderr, path); err != nil {
		t.Fatalf("second: %v", err)
	}
	second := strings.TrimSpace(out.String())
	if first == second {
		t.Fatal("two rotations should produce different tokens")
	}
	// After rotation, only the new token must verify.
	hash, _ := auth.ReadHash(path)
	if ok, _ := auth.VerifyHash(first, hash); ok {
		t.Fatal("old token still accepted after rotation")
	}
	if ok, _ := auth.VerifyHash(second, hash); !ok {
		t.Fatal("new token not accepted after rotation")
	}
}
