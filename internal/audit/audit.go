// Package audit implements the append-only audit log defined by
// decision gm-o9t8.3.8. Events are JSONL records stored at
// <dir>/audit-YYYY-MM-DD.jsonl, rotated at UTC midnight. Each Event
// carries an HMAC-SHA256 signature over the canonical JSON of its
// non-signature fields, and a PrevID that chains it to the previous
// record in the same daily file.
//
// V1 is local-only — no cloud sink. The vault and microVM producers
// inject this Auditor at boot; HTTP read endpoints are wired by the
// server package.
package audit

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event is the canonical audit record. Field tags match the wire shape
// declared in gm-o9t8.3.8.
type Event struct {
	ID        string          `json:"id"`
	Timestamp time.Time       `json:"ts"`
	Type      string          `json:"type"`
	Tenant    string          `json:"tenant"`
	Workspace string          `json:"wsid,omitempty"`
	Actor     string          `json:"actor"`
	Resource  string          `json:"resource,omitempty"`
	Action    string          `json:"action"`
	Outcome   string          `json:"outcome"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Signature string          `json:"sig"`
	PrevID    string          `json:"prev_id,omitempty"`
}

// Query narrows a Query() lookup. Zero-value fields match anything.
type Query struct {
	Since     time.Time
	Until     time.Time
	Type      string
	Tenant    string
	Workspace string
	Limit     int
}

// Auditor is the public interface every producer talks to.
type Auditor interface {
	Append(ctx context.Context, e Event) error
	Tail(ctx context.Context) (<-chan Event, error)
	Query(ctx context.Context, q Query) ([]Event, error)
}

// Options controls Auditor construction. Dir and Key are required;
// Clock is optional (defaults to time.Now). EphemeralKey is set to true
// internally when Key was empty and a random one was generated.
type Options struct {
	Dir          string
	Key          []byte
	Clock        func() time.Time
	EphemeralKey bool
}

// Sentinel errors.
var (
	ErrClosed = errors.New("audit: auditor closed")
)

// New constructs a file-backed Auditor.
func New(opts Options) (Auditor, error) {
	if opts.Dir == "" {
		return nil, errors.New("audit: Dir required")
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("audit: mkdir %s: %w", opts.Dir, err)
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	key := opts.Key
	ephemeral := opts.EphemeralKey
	if len(key) == 0 {
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, fmt.Errorf("audit: random key: %w", err)
		}
		key = k
		ephemeral = true
		slog.Warn("audit: no signing key provided; using ephemeral key — cross-restart verification will break",
			"dir", opts.Dir)
	}
	a := &fileAuditor{
		dir:       opts.Dir,
		key:       append([]byte(nil), key...),
		clock:     clock,
		ephemeral: ephemeral,
		subs:      make(map[chan Event]struct{}),
	}
	return a, nil
}

// fileAuditor is the file-backed Auditor returned by New.
type fileAuditor struct {
	dir       string
	key       []byte
	clock     func() time.Time
	ephemeral bool

	mu        sync.Mutex
	prevID    string    // ID of last Append in the current open file
	openDay   string    // YYYY-MM-DD of current file
	openFile  *os.File  // nil until first Append
	monoSeq   uint32    // monotonic suffix per server (for ULID-like ids)
	monoEpoch int64     // last unix-ms used; bumped on collision
	subs      map[chan Event]struct{}
	closed    bool
}

// Append writes the event to today's file, computing ID / PrevID /
// Signature before write. The event the caller passes in is treated as
// a template — ID, Timestamp, Tenant, Outcome are normalized when zero.
func (a *fileAuditor) Append(ctx context.Context, e Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrClosed
	}
	now := a.clock().UTC()
	if e.Timestamp.IsZero() {
		e.Timestamp = now
	} else {
		e.Timestamp = e.Timestamp.UTC()
	}
	if e.Tenant == "" {
		e.Tenant = SingleTenant
	}
	if e.Outcome == "" {
		e.Outcome = OutcomeOK
	}
	day := e.Timestamp.Format("2006-01-02")
	if err := a.openForDayLocked(day); err != nil {
		return err
	}
	if e.ID == "" {
		e.ID = a.nextIDLocked(e.Timestamp)
	}
	e.PrevID = a.prevID
	sig, err := signEvent(a.key, e)
	if err != nil {
		return err
	}
	e.Signature = sig
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}
	if _, err := a.openFile.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("audit: write: %w", err)
	}
	if err := a.openFile.Sync(); err != nil {
		return fmt.Errorf("audit: fsync: %w", err)
	}
	a.prevID = e.ID
	a.fanoutLocked(e)
	return nil
}

// openForDayLocked rotates the underlying file when the UTC day flips.
// Must be called with a.mu held.
func (a *fileAuditor) openForDayLocked(day string) error {
	if a.openFile != nil && a.openDay == day {
		return nil
	}
	if a.openFile != nil {
		_ = a.openFile.Close()
		a.openFile = nil
	}
	path := filepath.Join(a.dir, "audit-"+day+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("audit: open %s: %w", path, err)
	}
	// Recover prevID from the last record in the file, if any. For a
	// fresh file the prevID stays empty (first event of the day).
	prev, err := lastIDInFile(path)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("audit: scan %s: %w", path, err)
	}
	a.openFile = f
	a.openDay = day
	a.prevID = prev
	return nil
}

// nextIDLocked returns a ULID-style monotonic id: 48 bits of unix-ms
// followed by 80 bits of randomness. Encoded as 26-char Crockford b32
// is the strict spec, but for v1 we emit lowercase hex (32 chars) — it
// sorts the same, is shorter to read, and avoids pulling a ULID dep.
func (a *fileAuditor) nextIDLocked(ts time.Time) string {
	ms := ts.UnixMilli()
	if ms <= a.monoEpoch {
		// clock didn't advance; bump the in-second sequence
		a.monoSeq++
	} else {
		a.monoEpoch = ms
		a.monoSeq = 0
	}
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(ms))
	// shift the 48-bit ms into the top 6 bytes
	// (re-pack to avoid leading zeros for older timestamps)
	buf[0] = byte(ms >> 40)
	buf[1] = byte(ms >> 32)
	buf[2] = byte(ms >> 24)
	buf[3] = byte(ms >> 16)
	buf[4] = byte(ms >> 8)
	buf[5] = byte(ms)
	// 10 bytes of "random": 4-byte seq + 6 random bytes
	binary.BigEndian.PutUint32(buf[6:10], a.monoSeq)
	if _, err := rand.Read(buf[10:16]); err != nil {
		// Random failure: fall back to a deterministic suffix so the
		// id still chains. The signature still covers it.
		for i := 10; i < 16; i++ {
			buf[i] = byte(a.monoSeq >> uint(8*(i-10)))
		}
	}
	return hex.EncodeToString(buf[:])
}

// fanoutLocked sends e to every live Tail subscriber. Non-blocking —
// slow consumers drop frames rather than back-pressure Append.
func (a *fileAuditor) fanoutLocked(e Event) {
	for ch := range a.subs {
		select {
		case ch <- e:
		default:
			// drop
		}
	}
}

// Tail returns a channel that receives every Append that lands after
// the call. The channel closes when ctx is canceled or the Auditor
// shuts down.
func (a *fileAuditor) Tail(ctx context.Context) (<-chan Event, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, ErrClosed
	}
	ch := make(chan Event, 64)
	a.subs[ch] = struct{}{}
	a.mu.Unlock()
	go func() {
		<-ctx.Done()
		a.mu.Lock()
		if _, ok := a.subs[ch]; ok {
			delete(a.subs, ch)
			close(ch)
		}
		a.mu.Unlock()
	}()
	return ch, nil
}

// Query scans the daily files in [Since, Until], verifies each event's
// signature and chain, and returns matching records.
func (a *fileAuditor) Query(ctx context.Context, q Query) ([]Event, error) {
	a.mu.Lock()
	key := append([]byte(nil), a.key...)
	a.mu.Unlock()

	entries, err := os.ReadDir(a.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("audit: read dir: %w", err)
	}
	var files []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasPrefix(name, "audit-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		dayStr := strings.TrimSuffix(strings.TrimPrefix(name, "audit-"), ".jsonl")
		day, err := time.Parse("2006-01-02", dayStr)
		if err != nil {
			continue
		}
		// Day window check: include if the day overlaps [since, until].
		if !q.Since.IsZero() {
			dayEnd := day.Add(24 * time.Hour)
			if !dayEnd.After(q.Since) {
				continue
			}
		}
		if !q.Until.IsZero() && day.After(q.Until) {
			continue
		}
		files = append(files, filepath.Join(a.dir, name))
	}
	sort.Strings(files)

	var out []Event
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		evs, err := readAndVerifyFile(path, key)
		if err != nil {
			return nil, err
		}
		for _, e := range evs {
			if !matches(e, q) {
				continue
			}
			out = append(out, e)
			if q.Limit > 0 && len(out) >= q.Limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// matches applies the Query predicate to a single event.
func matches(e Event, q Query) bool {
	if !q.Since.IsZero() && e.Timestamp.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && e.Timestamp.After(q.Until) {
		return false
	}
	if q.Type != "" && e.Type != q.Type {
		return false
	}
	if q.Tenant != "" && e.Tenant != q.Tenant {
		return false
	}
	if q.Workspace != "" && e.Workspace != q.Workspace {
		return false
	}
	return true
}

// Close releases the open file handle and shuts down subscriber
// channels. Safe to call multiple times.
func (a *fileAuditor) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	for ch := range a.subs {
		close(ch)
	}
	a.subs = nil
	if a.openFile != nil {
		err := a.openFile.Close()
		a.openFile = nil
		return err
	}
	return nil
}

// --- canonical JSON + HMAC ----------------------------------------------------

// canonicalJSON encodes v as JSON with deterministic key ordering and
// no whitespace. We marshal the event with encoding/json, parse it back
// into a generic structure, then re-emit with sorted keys. This is
// expensive but bulletproof; the audit hot path is not throughput-critical.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var any1 any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&any1); err != nil {
		return nil, err
	}
	var buf strings.Builder
	if err := emitCanonical(&buf, any1); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func emitCanonical(w *strings.Builder, v any) error {
	switch t := v.(type) {
	case nil:
		w.WriteString("null")
	case bool:
		if t {
			w.WriteString("true")
		} else {
			w.WriteString("false")
		}
	case string:
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		w.Write(b)
	case json.Number:
		w.WriteString(t.String())
	case float64:
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		w.Write(b)
	case []any:
		w.WriteByte('[')
		for i, el := range t {
			if i > 0 {
				w.WriteByte(',')
			}
			if err := emitCanonical(w, el); err != nil {
				return err
			}
		}
		w.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		w.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				w.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			w.Write(kb)
			w.WriteByte(':')
			if err := emitCanonical(w, t[k]); err != nil {
				return err
			}
		}
		w.WriteByte('}')
	default:
		// Fallback: defer to json.Marshal. Shouldn't happen — every
		// JSON-decodable value falls in the cases above.
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		w.Write(b)
	}
	return nil
}

// signEvent computes HMAC-SHA256 over the canonical JSON of e with the
// Signature field zeroed.
func signEvent(key []byte, e Event) (string, error) {
	e.Signature = ""
	buf, err := canonicalJSON(e)
	if err != nil {
		return "", fmt.Errorf("audit: canonicalize: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(buf)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// verifyEvent returns nil if e.Signature matches HMAC-SHA256 of the
// rest of the event under key.
func verifyEvent(key []byte, e Event) error {
	got := e.Signature
	want, err := signEvent(key, e)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(got), []byte(want)) {
		return fmt.Errorf("audit: signature mismatch (id=%s)", e.ID)
	}
	return nil
}

// --- file scanning ------------------------------------------------------------

// readAndVerifyFile parses every line in path, verifies the HMAC on
// each record, and confirms that each event's PrevID equals the
// previous event's ID. Any tampering or break in the chain surfaces
// as an error.
func readAndVerifyFile(path string, key []byte) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	// Allow up to 1 MiB per event line. Generous for payloads but
	// bounded against pathological files.
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []Event
	var prevID string
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("audit: %s line %d: parse: %w", path, lineNo, err)
		}
		if e.PrevID != prevID {
			return nil, fmt.Errorf("audit: %s line %d: chain break: prev_id=%q want %q",
				path, lineNo, e.PrevID, prevID)
		}
		if err := verifyEvent(key, e); err != nil {
			return nil, fmt.Errorf("audit: %s line %d: %w", path, lineNo, err)
		}
		out = append(out, e)
		prevID = e.ID
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("audit: %s: scan: %w", path, err)
	}
	return out, nil
}

// lastIDInFile returns the ID of the last record in path, or "" when
// the file is empty / missing. Used by openForDayLocked to seed
// prevID on restart. Does NOT verify signatures — that's Query's job
// — because Append must not fail on a tampered file (the next Query
// will surface it).
func lastIDInFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var lastID string
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			return "", err
		}
		lastID = e.ID
	}
	return lastID, sc.Err()
}

// Closer lets callers shut the file handle. The interface returned by
// New does not expose this — server wiring asserts on *fileAuditor or
// the package-level helper Close(a Auditor) below.
var _ io.Closer = (*fileAuditor)(nil)

// Close shuts down an Auditor returned by New, if it supports it.
func Close(a Auditor) error {
	if c, ok := a.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
