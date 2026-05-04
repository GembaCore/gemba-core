package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BootstrapStore owns one short-lived, one-time launch token. The token is
// intended for URL fragments printed at startup; the server never sees the
// fragment until the SPA explicitly exchanges it.
type BootstrapStore struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
	used      bool
}

func NewBootstrapStore(token string, expiresAt time.Time) *BootstrapStore {
	if token == "" || expiresAt.IsZero() {
		return nil
	}
	return &BootstrapStore{token: token, expiresAt: expiresAt}
}

func (b *BootstrapStore) Consume(presented string, now time.Time) bool {
	if b == nil || presented == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used || !now.Before(b.expiresAt) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(b.token)) != 1 {
		return false
	}
	b.used = true
	return true
}

// BootstrapLoginHandler exchanges a valid one-time bootstrap token for the
// same signed session cookie issued by LoginHandler. It intentionally does not
// accept the primary bearer token; that remains behind /api/auth/login.
func BootstrapLoginHandler(cs *CookieSigner, bs *BootstrapStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeUnauthorized(w, "method_not_allowed")
			return
		}
		if !bs.Consume(bootstrapTokenFromRequest(r), time.Now()) {
			writeUnauthorized(w, "invalid_bootstrap")
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

func bootstrapTokenFromRequest(r *http.Request) string {
	const prefix = "Bearer "
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(h, prefix))
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
		return strings.TrimSpace(body.Token)
	}
	return ""
}
