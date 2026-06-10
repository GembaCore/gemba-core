package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBearerOrCookieAuth_BearerPasses(t *testing.T) {
	cs, _ := NewCookieSigner()
	mw := BearerOrCookieAuth(NewPlainVerifier("tok"), cs)(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestBearerOrCookieAuth_ValidCookiePasses(t *testing.T) {
	cs, _ := NewCookieSigner()
	// Deliberately pass a verifier that rejects everything so we know the
	// cookie path is what is allowing the request through.
	mw := BearerOrCookieAuth(NewPlainVerifier("nope"), cs)(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cs.Issue(time.Now())})
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

func TestBearerOrCookieAuth_NoCredentialsRejects(t *testing.T) {
	cs, _ := NewCookieSigner()
	mw := BearerOrCookieAuth(NewPlainVerifier("tok"), cs)(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestBearerOrCookieAuth_ExpiredCookieFallsThroughToBearer(t *testing.T) {
	cs, _ := NewCookieSigner()
	mw := BearerOrCookieAuth(NewPlainVerifier("tok"), cs)(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "garbage"})
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad cookie + good bearer should pass, got %d", rec.Code)
	}
}

func TestLoginHandler_IssuesSignedCookie(t *testing.T) {
	cs, _ := NewCookieSigner()
	h := LoginHandler(cs)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	setCookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, SessionCookieName+"=") {
		t.Fatalf("expected %s cookie in Set-Cookie, got %q", SessionCookieName, setCookie)
	}
	if !strings.Contains(setCookie, "HttpOnly") {
		t.Fatalf("cookie must be HttpOnly, got %q", setCookie)
	}
	if !strings.Contains(setCookie, "SameSite=Strict") {
		t.Fatalf("cookie must be SameSite=Strict, got %q", setCookie)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["cookie"] != SessionCookieName {
		t.Fatalf("body.cookie = %v, want %s", body["cookie"], SessionCookieName)
	}
}

func TestLoginHandler_OnlyAcceptsPOST(t *testing.T) {
	cs, _ := NewCookieSigner()
	h := LoginHandler(cs)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET should 401 (method_not_allowed envelope), got %d", rec.Code)
	}
}
