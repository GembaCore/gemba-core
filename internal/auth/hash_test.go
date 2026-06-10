package auth

import (
	"strings"
	"testing"
)

func TestHashToken_VerifiesItsOwnOutput(t *testing.T) {
	const tok = "s3cret-token"
	hash, err := HashToken(tok)
	if err != nil {
		t.Fatalf("HashToken: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("expected argon2id PHC prefix, got %q", hash)
	}
	ok, err := VerifyHash(tok, hash)
	if err != nil {
		t.Fatalf("VerifyHash: %v", err)
	}
	if !ok {
		t.Fatal("VerifyHash returned false for matching token")
	}
}

func TestHashToken_RejectsWrongToken(t *testing.T) {
	hash, err := HashToken("correct")
	if err != nil {
		t.Fatalf("HashToken: %v", err)
	}
	ok, err := VerifyHash("wrong", hash)
	if err != nil {
		t.Fatalf("VerifyHash: %v", err)
	}
	if ok {
		t.Fatal("VerifyHash accepted a wrong token")
	}
}

func TestHashToken_ProducesDifferentSalts(t *testing.T) {
	a, _ := HashToken("same")
	b, _ := HashToken("same")
	if a == b {
		t.Fatal("two hashes of the same token should differ (random salt)")
	}
}

func TestVerifyHash_RejectsMalformed(t *testing.T) {
	for _, bad := range []string{
		"",
		"not-a-hash",
		"$argon2i$v=19$m=16,t=1,p=1$aaaa$bbbb", // wrong variant
		"$argon2id$v=1$m=16,t=1,p=1$aaaa$bbbb", // wrong version
	} {
		if _, err := VerifyHash("x", bad); err == nil {
			t.Fatalf("expected error for malformed hash %q", bad)
		}
	}
}
