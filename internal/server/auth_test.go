package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/auth"
	"github.com/MikeBengtson/gemba/internal/config"
)

// gm-e5.2: with a hashed-file verifier, a request bearing the plaintext
// token matching the stored hash must be accepted — proving the server
// never needs the plaintext on disk.
func TestTokenAuth_HashFile_AcceptsMatchingBearer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "primary")
	hash, err := auth.HashToken("plaintext-token")
	if err != nil {
		t.Fatalf("HashToken: %v", err)
	}
	if err := auth.WriteHash(path, hash); err != nil {
		t.Fatalf("WriteHash: %v", err)
	}

	cfg := config.ServeConfig{
		Listen:            "127.0.0.1",
		AuthMode:          "token",
		AuthTokenHashPath: path,
	}
	h := NewRouter(cfg, fakeSPA())

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer plaintext-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 with matching token, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

// gm-e5.2 DoD: tests cover rotate-while-running. Writing a new hash file
// while the same router instance keeps serving must flip which plaintext
// token is accepted.
func TestTokenAuth_RotateWhileRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "primary")

	oldHash, err := auth.HashToken("old-token")
	if err != nil {
		t.Fatalf("HashToken old: %v", err)
	}
	if err := auth.WriteHash(path, oldHash); err != nil {
		t.Fatalf("WriteHash old: %v", err)
	}

	cfg := config.ServeConfig{
		Listen:            "127.0.0.1",
		AuthMode:          "token",
		AuthTokenHashPath: path,
	}
	h := NewRouter(cfg, fakeSPA())

	if code := doBearer(h, "old-token"); code != http.StatusOK {
		t.Fatalf("old-token pre-rotation: want 200, got %d", code)
	}
	if code := doBearer(h, "new-token"); code != http.StatusUnauthorized {
		t.Fatalf("new-token pre-rotation: want 401, got %d", code)
	}

	// Advance mtime past filesystem resolution.
	time.Sleep(1100 * time.Millisecond)

	newHash, err := auth.HashToken("new-token")
	if err != nil {
		t.Fatalf("HashToken new: %v", err)
	}
	if err := auth.WriteHash(path, newHash); err != nil {
		t.Fatalf("WriteHash new: %v", err)
	}

	if code := doBearer(h, "old-token"); code != http.StatusUnauthorized {
		t.Fatalf("old-token post-rotation: want 401, got %d", code)
	}
	if code := doBearer(h, "new-token"); code != http.StatusOK {
		t.Fatalf("new-token post-rotation: want 200, got %d", code)
	}
}

// gm-e5.2: /api/auth/login exchanges a valid bearer for a signed session
// cookie. Subsequent requests carrying only the cookie are accepted.
func TestLogin_ExchangesBearerForCookie(t *testing.T) {
	cfg := config.ServeConfig{
		Listen:    "127.0.0.1",
		AuthMode:  "token",
		AuthToken: "my-bearer",
	}
	h := NewRouter(cfg, fakeSPA())

	// POST /api/auth/login with bearer → 200 + Set-Cookie.
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.Header.Set("Authorization", "Bearer my-bearer")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	result := rec.Result()
	defer result.Body.Close()
	cookies := result.Cookies()
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.SessionCookieName {
			session = c
			break
		}
	}
	if session == nil {
		t.Fatalf("expected %s cookie", auth.SessionCookieName)
	}
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie must be HttpOnly + SameSite=Strict; got %+v", session)
	}

	// Subsequent GET /api/health with cookie only → 200.
	req = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie-only: want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

// gm-e5.2: login must refuse requests that present no credentials — the
// auth middleware runs before the login handler for this very reason, so
// anonymous callers can't trigger an unauthenticated cookie mint.
func TestLogin_RejectsAnonymous(t *testing.T) {
	cfg := config.ServeConfig{
		Listen:    "127.0.0.1",
		AuthMode:  "token",
		AuthToken: "my-bearer",
	}
	h := NewRouter(cfg, fakeSPA())
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

// gm-e5.2 DoD: token never logged anywhere. The stored file is an argon2id
// hash — the plaintext must never appear inside it. We prove the invariant
// by round-tripping a fixed plaintext through WriteHash/ReadHash and
// asserting the plaintext is absent from the on-disk bytes.
func TestTokenHashFile_ContainsNoPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "primary")
	const plaintext = "do-not-log-this-token"
	hash, err := auth.HashToken(plaintext)
	if err != nil {
		t.Fatalf("HashToken: %v", err)
	}
	if err := auth.WriteHash(path, hash); err != nil {
		t.Fatalf("WriteHash: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), plaintext) {
		t.Fatalf("plaintext token leaked into hash file")
	}
}

func doBearer(h http.Handler, tok string) int {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}
