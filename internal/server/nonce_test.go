package server

import (
	"net/http"
	"testing"
	"time"
)

// LRU eviction: capacity-bounded so a flood of unique nonces can't
// blow up memory.
func TestNonceCache_EvictsLRU(t *testing.T) {
	cache := NewNonceCache(2, time.Hour)
	cache.Put("a", cachedResponse{Status: 200, Body: []byte(`a`)})
	cache.Put("b", cachedResponse{Status: 200, Body: []byte(`b`)})
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("a should still be present")
	}
	cache.Put("c", cachedResponse{Status: 200, Body: []byte(`c`)})
	// LRU evicts the least-recently-used. We touched a (Get above),
	// so b is the oldest now.
	if _, ok := cache.Get("b"); ok {
		t.Fatal("b should have been evicted by capacity")
	}
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("a should still be present after eviction")
	}
	if _, ok := cache.Get("c"); !ok {
		t.Fatal("c should still be present")
	}
}

// Expired entries return miss and clean themselves up.
func TestNonceCache_ExpiresEntries(t *testing.T) {
	cache := NewNonceCache(8, 10*time.Millisecond)
	now := time.Now()
	cache.now = func() time.Time { return now }
	cache.Put("a", cachedResponse{Status: 200, Body: []byte(`a`)})
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("a should be present immediately")
	}
	cache.now = func() time.Time { return now.Add(time.Hour) }
	if _, ok := cache.Get("a"); ok {
		t.Fatal("a should have expired")
	}
}

// Replays surface a header so callers can detect "your retry hit the
// cache" without diffing bodies.
func TestRequireConfirmNonce_StampsReplayHeader(t *testing.T) {
	cache := NewNonceCache(8, time.Hour)
	cache.Put("nonce-X", cachedResponse{
		Status:  200,
		Headers: http.Header{"Content-Type": []string{"application/json"}},
		Body:    []byte(`{"ok":true}`),
	})

	mw := requireConfirmNonce(cache)
	called := false
	handler := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	req, _ := http.NewRequest(http.MethodPatch, "/x", nil)
	req.Header.Set(ConfirmHeader, "nonce-X")
	rw := newCapturingRW()
	handler.ServeHTTP(rw, req)

	if called {
		t.Fatal("inner handler must NOT run on a cache hit")
	}
	if rw.Status != http.StatusOK {
		t.Fatalf("want 200 from cache, got %d", rw.Status)
	}
	if rw.Header().Get(ConfirmHeader+"-Replay") != "true" {
		t.Fatalf("missing replay header; got %q", rw.Header().Get(ConfirmHeader+"-Replay"))
	}
}

// capturingRW is a minimal http.ResponseWriter that records what was
// written so the nonce middleware tests can assert without standing up
// httptest.NewRecorder (avoids the extra dependency on full HTTP
// machinery for unit-style assertions).
type capturingRW struct {
	Status int
	hdr    http.Header
	body   []byte
}

func newCapturingRW() *capturingRW {
	return &capturingRW{hdr: http.Header{}}
}

func (c *capturingRW) Header() http.Header { return c.hdr }
func (c *capturingRW) Write(p []byte) (int, error) {
	c.body = append(c.body, p...)
	if c.Status == 0 {
		c.Status = http.StatusOK
	}
	return len(p), nil
}
func (c *capturingRW) WriteHeader(s int) { c.Status = s }
