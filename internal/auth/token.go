package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// NewToken returns a hex-encoded 256-bit random token suitable for use as
// a bearer credential. The return value is the plaintext token and must be
// shown to the operator exactly once; only its hash should be persisted.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// BearerAuth returns middleware that verifies the Authorization header using
// v. Retained for call sites (including tests) that only need bearer auth;
// BearerOrCookieAuth is the full gm-e5.2 surface.
func BearerAuth(v Verifier) func(http.Handler) http.Handler {
	return BearerOrCookieAuth(v, nil)
}

// BearerOrCookieAuth returns middleware that accepts either:
//   - an Authorization: Bearer <token> header whose token v accepts, or
//   - a signed session cookie (when cs is non-nil) whose signature matches.
//
// Missing and invalid credentials return 401 with a stable JSON envelope.
// The middleware runs before route lookup so unauthenticated clients cannot
// distinguish existing routes from absent ones via 401-vs-404 oracles
// (gm-b3 / gm-99g).
func BearerOrCookieAuth(v Verifier, cs *CookieSigner) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cs != nil {
				if ck, err := r.Cookie(SessionCookieName); err == nil {
					if cs.Verify(ck.Value, time.Now()) {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
			h := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(h, prefix) {
				writeUnauthorized(w, "missing_bearer")
				return
			}
			presented := strings.TrimPrefix(h, prefix)
			if !v.Verify(presented) {
				writeUnauthorized(w, "invalid_bearer")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LoginHandler returns an http.Handler that issues a signed session cookie.
// It expects the surrounding middleware to have already verified the
// request's bearer token; the handler itself just stamps a fresh cookie.
// Exchanging a valid cookie for a new cookie is harmless and acts as a
// session refresh.
func LoginHandler(cs *CookieSigner) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeUnauthorized(w, "method_not_allowed")
			return
		}
		value := cs.Issue(time.Now())
		cs.SetSessionCookie(w, r, value)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"cookie":     SessionCookieName,
			"expires_in": int(cs.TTL().Seconds()),
		})
	})
}

func writeUnauthorized(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "unauthorized",
		"code":  code,
	})
}
