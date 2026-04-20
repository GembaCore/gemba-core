package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestBearerAuth_MissingHeader_Returns401(t *testing.T) {
	mw := BearerAuth(NewPlainVerifier("s3cret"))(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/any", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("want application/json, got %q", ct)
	}
}

func TestBearerAuth_WrongToken_Returns401(t *testing.T) {
	mw := BearerAuth(NewPlainVerifier("s3cret"))(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/any", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestBearerAuth_CorrectToken_PassesThrough(t *testing.T) {
	mw := BearerAuth(NewPlainVerifier("s3cret"))(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/any", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

func TestNewToken_IsRandomHex(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	b, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if a == b {
		t.Fatal("consecutive tokens should differ")
	}
	if len(a) != 64 {
		t.Fatalf("want 64 hex chars, got %d", len(a))
	}
}
