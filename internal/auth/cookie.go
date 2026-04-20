package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SessionCookieName is the cookie name the server issues after a successful
// bearer exchange at POST /api/auth/login.
const SessionCookieName = "gemba_session"

// DefaultSessionTTL is how long an issued session cookie is valid.
const DefaultSessionTTL = 24 * time.Hour

// CookieSigner issues and verifies signed session cookies using
// HMAC-SHA256 over a randomly generated per-process key.
//
// Cookie format:
//
//	base64url(exp_unix_seconds_decimal) + "." + base64url(HMAC-SHA256(key, exp_bytes))
//
// The key is regenerated every process start so restarting the server
// invalidates every outstanding cookie. Clients recover by re-exchanging
// their bearer at POST /api/auth/login.
type CookieSigner struct {
	key []byte
	ttl time.Duration
}

// NewCookieSigner returns a CookieSigner with a random 256-bit key and the
// default session TTL. An error is returned only if the system RNG fails.
func NewCookieSigner() (*CookieSigner, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("cookie signer key: %w", err)
	}
	return &CookieSigner{key: key, ttl: DefaultSessionTTL}, nil
}

// TTL returns how long an issued cookie is valid.
func (c *CookieSigner) TTL() time.Duration { return c.ttl }

// Issue builds a signed cookie value whose expiry is now+TTL.
func (c *CookieSigner) Issue(now time.Time) string {
	exp := now.Add(c.ttl).Unix()
	return c.sign(exp)
}

func (c *CookieSigner) sign(exp int64) string {
	payload := strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(payload))
	sig := mac.Sum(nil)
	b64 := base64.RawURLEncoding
	return b64.EncodeToString([]byte(payload)) + "." + b64.EncodeToString(sig)
}

// Verify returns true if value is a well-formed, signed, unexpired cookie.
func (c *CookieSigner) Verify(value string, now time.Time) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return false
	}
	b64 := base64.RawURLEncoding
	payload, err := b64.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sig, err := b64.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, c.key)
	mac.Write(payload)
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return false
	}
	exp, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		return false
	}
	return now.Unix() < exp
}

// SetSessionCookie writes the signed cookie onto w with secure defaults.
// Secure=true is set only when the request arrived over TLS so the cookie
// still works on the localhost-only default bind.
func (c *CookieSigner) SetSessionCookie(w http.ResponseWriter, r *http.Request, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(c.ttl.Seconds()),
	})
}
