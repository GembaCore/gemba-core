// X-GEMBA-Confirm idempotency cache (gm-root.8 slice 1).
//
// Every mutating /api/* call MUST carry the header. The first request
// runs the underlying handler; the cached response (status + body) is
// returned for any replay of the same nonce within the TTL.
//
// Why an LRU instead of a write-once map: bounded memory under
// pathological clients that mint a nonce per request without ever
// replaying. TTL on top of LRU because a long-tail of stale nonces
// would otherwise accumulate without ever being evicted.
//
// The cache lives in-memory per process — fine for the M2 single-binary
// gemba serve. A multi-instance future will need a shared store; the
// interface is small enough to stub out then.

package server

import (
	"bytes"
	"container/list"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ConfirmHeader is the request header every mutating route requires.
// Echo it back in the response so callers can confirm the server saw
// the right token (helps debug "did my retry land?" without parsing
// bodies).
const ConfirmHeader = "X-GEMBA-Confirm"

// NonceCache caches recent (nonce → response) tuples so a replayed
// mutating request returns the cached response unchanged.
//
// Capacity bounds memory; TTL keeps stale entries from sitting forever
// once the LRU stops touching them.
type NonceCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	now      func() time.Time
	order    *list.List
	entries  map[string]*list.Element
}

type nonceEntry struct {
	nonce    string
	expires  time.Time
	response cachedResponse
}

type cachedResponse struct {
	Status  int
	Headers http.Header
	Body    []byte
}

// NewNonceCache returns a cache with the given capacity (max entries)
// and TTL. Defaults: capacity=1024, ttl=5min — sized for normal SPA
// double-click protection without a real footprint cost.
func NewNonceCache(capacity int, ttl time.Duration) *NonceCache {
	if capacity <= 0 {
		capacity = 1024
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &NonceCache{
		capacity: capacity,
		ttl:      ttl,
		now:      time.Now,
		order:    list.New(),
		entries:  make(map[string]*list.Element),
	}
}

// Get returns the cached response for nonce when present and not
// expired. Touches LRU order so a hot nonce stays warm.
func (c *NonceCache) Get(nonce string) (cachedResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[nonce]
	if !ok {
		return cachedResponse{}, false
	}
	entry := el.Value.(*nonceEntry)
	if c.now().After(entry.expires) {
		c.order.Remove(el)
		delete(c.entries, nonce)
		return cachedResponse{}, false
	}
	c.order.MoveToFront(el)
	return entry.response, true
}

// Put stores resp under nonce, evicting the least-recently-used entry
// if the cache is at capacity.
func (c *NonceCache) Put(nonce string, resp cachedResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[nonce]; ok {
		entry := el.Value.(*nonceEntry)
		entry.response = resp
		entry.expires = c.now().Add(c.ttl)
		c.order.MoveToFront(el)
		return
	}
	for c.order.Len() >= c.capacity {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*nonceEntry).nonce)
	}
	entry := &nonceEntry{
		nonce:    nonce,
		expires:  c.now().Add(c.ttl),
		response: resp,
	}
	el := c.order.PushFront(entry)
	c.entries[nonce] = el
}

// recordingResponseWriter buffers status + body so the nonce middleware
// can stash a copy after the inner handler runs. Headers are still
// written through to the wire as the inner handler sets them; only the
// post-write snapshot is captured.
type recordingResponseWriter struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (r *recordingResponseWriter) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recordingResponseWriter) Write(p []byte) (int, error) {
	r.buf.Write(p)
	return r.ResponseWriter.Write(p)
}

// requireConfirmNonce is the mutation gate. Wrap any PATCH/POST/PUT
// route with this so:
//   - Missing or whitespace-only X-GEMBA-Confirm → 400 envelope.
//   - First request: runs the inner handler and caches the response.
//   - Replays of the same nonce within the TTL: returns the cached
//     response without re-invoking the handler. Verbatim status, body,
//     and any captured headers.
//
// Replays SHOULD be safe even when the inner handler emits non-2xx —
// the cache stores the same envelope the first call returned.
func requireConfirmNonce(cache *NonceCache) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nonce := strings.TrimSpace(r.Header.Get(ConfirmHeader))
			if nonce == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":   "missing_confirm_nonce",
					"message": "X-GEMBA-Confirm header is required for mutating requests",
				})
				return
			}
			if cached, ok := cache.Get(nonce); ok {
				replayHeader := w.Header()
				for k, vs := range cached.Headers {
					for _, v := range vs {
						replayHeader.Add(k, v)
					}
				}
				replayHeader.Set(ConfirmHeader+"-Replay", "true")
				w.WriteHeader(cached.Status)
				_, _ = w.Write(cached.Body)
				return
			}
			rec := &recordingResponseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			cache.Put(nonce, cachedResponse{
				Status:  rec.status,
				Headers: cloneRelevantHeaders(rec.Header()),
				Body:    append([]byte(nil), rec.buf.Bytes()...),
			})
		})
	}
}

// cloneRelevantHeaders captures Content-Type and similar response
// metadata so a replay reproduces the wire shape. Skip headers that
// are per-connection (Set-Cookie, Date, …) — replays SHOULD NOT pin
// stale session state.
func cloneRelevantHeaders(h http.Header) http.Header {
	out := http.Header{}
	for _, key := range []string{"Content-Type", "Content-Encoding"} {
		if v := h.Values(key); len(v) > 0 {
			out[key] = append([]string(nil), v...)
		}
	}
	return out
}
