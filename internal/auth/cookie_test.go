package auth

import (
	"strings"
	"testing"
	"time"
)

func TestCookieSigner_IssueAndVerify(t *testing.T) {
	cs, err := NewCookieSigner()
	if err != nil {
		t.Fatalf("NewCookieSigner: %v", err)
	}
	now := time.Now()
	cookie := cs.Issue(now)
	if !strings.Contains(cookie, ".") {
		t.Fatalf("expected '.' separator in cookie, got %q", cookie)
	}
	if !cs.Verify(cookie, now) {
		t.Fatal("freshly issued cookie failed verification")
	}
}

func TestCookieSigner_RejectsExpired(t *testing.T) {
	cs, err := NewCookieSigner()
	if err != nil {
		t.Fatalf("NewCookieSigner: %v", err)
	}
	base := time.Now()
	cookie := cs.Issue(base)
	future := base.Add(cs.TTL() + time.Hour)
	if cs.Verify(cookie, future) {
		t.Fatal("expired cookie accepted")
	}
}

func TestCookieSigner_RejectsTampered(t *testing.T) {
	cs, err := NewCookieSigner()
	if err != nil {
		t.Fatalf("NewCookieSigner: %v", err)
	}
	now := time.Now()
	cookie := cs.Issue(now)
	parts := strings.Split(cookie, ".")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %q", cookie)
	}
	// Flip a byte in the signature portion.
	tampered := parts[0] + "." + flipFirstByte(parts[1])
	if cs.Verify(tampered, now) {
		t.Fatal("tampered cookie accepted")
	}
}

func TestCookieSigner_RejectsForeignKey(t *testing.T) {
	a, _ := NewCookieSigner()
	b, _ := NewCookieSigner()
	now := time.Now()
	cookie := a.Issue(now)
	if b.Verify(cookie, now) {
		t.Fatal("cookie signed by a accepted by b")
	}
}

func TestCookieSigner_RejectsGarbage(t *testing.T) {
	cs, _ := NewCookieSigner()
	now := time.Now()
	for _, bad := range []string{"", "no-dot", "a.b.c", "!!!.???", "."} {
		if cs.Verify(bad, now) {
			t.Fatalf("accepted garbage cookie %q", bad)
		}
	}
}

func flipFirstByte(s string) string {
	if s == "" {
		return "A"
	}
	b := []byte(s)
	b[0] ^= 0x01
	return string(b)
}
