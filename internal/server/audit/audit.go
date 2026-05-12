package audit

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// KeyEnv is the environment variable holding the base64-encoded 32-byte
// Ed25519 seed used for record signing.
const KeyEnv = "GEMBA_AUDIT_KEY"

// Kind is a coarse category for an audit entry.
type Kind string

const (
	KindFileWrite       Kind = "file_write"
	KindFileDelete      Kind = "file_delete"
	KindCommandRun      Kind = "command_run"
	KindBeadMutation    Kind = "bead_mutation"
	KindAgentStarted    Kind = "agent_started"
	KindAgentCompleted  Kind = "agent_completed"
	KindAgentFailed     Kind = "agent_failed"
	KindSecretInjected  Kind = "secret_injected"
)

// Record is one append-only audit entry. The JSON form is what's written
// to disk, one record per line.
type Record struct {
	Seq        int64           `json:"seq"`
	TS         time.Time       `json:"ts"`
	PrevHash   string          `json:"prev_hash"`
	Workspace  string          `json:"workspace,omitempty"`
	RunID      string          `json:"run_id,omitempty"`
	Kind       Kind            `json:"kind"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	ContentSHA string          `json:"content_sha,omitempty"` // SHA-256 hex of payload bytes
	Sig        string          `json:"sig"`                   // base64 Ed25519 signature
}

// Logger is the append handle for an audit log file. Safe for concurrent
// Append calls.
type Logger struct {
	path string
	key  ed25519.PrivateKey

	mu       sync.Mutex
	seq      int64
	prevHash string
}

// Open returns a Logger writing to path, creating the file if absent.
// On open, the file is scanned to determine the next sequence number
// and prevHash; this is O(n) in the number of records.
func Open(path string, key ed25519.PrivateKey) (*Logger, error) {
	if path == "" {
		return nil, errors.New("audit: path required")
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("audit: invalid Ed25519 private key length %d", len(key))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("audit: mkdir: %w", err)
	}
	l := &Logger{path: path, key: key}
	if err := l.scan(); err != nil {
		return nil, err
	}
	return l, nil
}

// Append writes a new record and returns it (with computed Seq, TS,
// PrevHash, and Sig fields populated).
func (l *Logger) Append(ctx context.Context, workspace, runID string, kind Kind, payload any) (*Record, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	pBytes, err := payloadBytes(payload)
	if err != nil {
		return nil, err
	}
	contentSHA := sha256.Sum256(pBytes)

	rec := Record{
		Seq:        l.seq + 1,
		TS:         time.Now().UTC(),
		PrevHash:   l.prevHash,
		Workspace:  workspace,
		RunID:      runID,
		Kind:       kind,
		Payload:    pBytes,
		ContentSHA: hex.EncodeToString(contentSHA[:]),
	}
	rec.Sig = sign(l.key, recordSigningBytes(&rec))

	line, err := json.Marshal(&rec)
	if err != nil {
		return nil, fmt.Errorf("audit: marshal: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open append: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return nil, fmt.Errorf("audit: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("audit: sync: %w", err)
	}

	l.seq = rec.Seq
	l.prevHash = chainHash(&rec)
	return &rec, nil
}

// PublicKey returns the public half of the logger's signing key.
func (l *Logger) PublicKey() ed25519.PublicKey {
	return l.key.Public().(ed25519.PublicKey)
}

// Read returns all records with Seq > fromSeq. fromSeq=0 returns
// everything. Records are returned in append order. Reading does not
// hold the append mutex.
func (l *Logger) Read(fromSeq int64) ([]Record, error) {
	f, err := os.Open(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return out, fmt.Errorf("audit: decode line: %w", err)
		}
		if r.Seq > fromSeq {
			out = append(out, r)
		}
	}
	return out, sc.Err()
}

// Tail emits records as they are appended. Polling-based: not real-time
// but cheap and simple. Callers receive records via ch; the channel
// closes when ctx cancels.
func (l *Logger) Tail(ctx context.Context, fromSeq int64, pollInterval time.Duration) <-chan Record {
	if pollInterval <= 0 {
		pollInterval = 250 * time.Millisecond
	}
	ch := make(chan Record, 64)
	cursor := fromSeq
	go func() {
		defer close(ch)
		t := time.NewTicker(pollInterval)
		defer t.Stop()
		for {
			recs, err := l.Read(cursor)
			if err == nil {
				for _, r := range recs {
					select {
					case <-ctx.Done():
						return
					case ch <- r:
						cursor = r.Seq
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return ch
}

// Verify checks every record's signature against pub and confirms the
// chain (prev_hash linkage). Returns nil on success, or an error
// describing the first failing record.
func Verify(records []Record, pub ed25519.PublicKey) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("audit: invalid Ed25519 public key length")
	}
	prev := ""
	for i := range records {
		r := records[i]
		if r.PrevHash != prev {
			return fmt.Errorf("audit: record %d prev_hash mismatch: got %s want %s", r.Seq, r.PrevHash, prev)
		}
		sigBytes, err := base64.StdEncoding.DecodeString(r.Sig)
		if err != nil {
			return fmt.Errorf("audit: record %d bad signature encoding: %w", r.Seq, err)
		}
		if !ed25519.Verify(pub, recordSigningBytes(&r), sigBytes) {
			return fmt.Errorf("audit: record %d signature invalid", r.Seq)
		}
		prev = chainHash(&r)
	}
	return nil
}

// GenerateKey returns a fresh Ed25519 keypair suitable for signing
// records.
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// KeyFromEnv parses a base64-encoded 32-byte seed from $GEMBA_AUDIT_KEY
// into a full Ed25519 private key.
func KeyFromEnv() (ed25519.PrivateKey, error) {
	v := os.Getenv(KeyEnv)
	if v == "" {
		return nil, fmt.Errorf("audit: %s not set", KeyEnv)
	}
	seed, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		seed, err = base64.URLEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("audit: %s not valid base64: %w", KeyEnv, err)
		}
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("audit: seed length %d != %d", len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// EncodeSeed returns the base64-encoded 32-byte seed for k.
func EncodeSeed(k ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(k.Seed())
}

// --- internal --------------------------------------------------------

// scan walks the file and updates l.seq and l.prevHash to point at the
// end of the chain. Empty file => seq=0, prevHash="".
func (l *Logger) scan() error {
	f, err := os.Open(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return fmt.Errorf("audit: scan: line %d: %w", l.seq+1, err)
		}
		l.seq = r.Seq
		l.prevHash = chainHash(&r)
	}
	return sc.Err()
}

// payloadBytes accepts json.RawMessage, []byte, or any json-marshalable
// value and returns the raw JSON form.
func payloadBytes(payload any) (json.RawMessage, error) {
	if payload == nil {
		return nil, nil
	}
	switch v := payload.(type) {
	case json.RawMessage:
		return v, nil
	case []byte:
		return v, nil
	default:
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("audit: marshal payload: %w", err)
		}
		return b, nil
	}
}

// recordSigningBytes returns the canonical bytes that get signed for a
// record. The signature itself is excluded; everything else is included.
func recordSigningBytes(r *Record) []byte {
	cp := *r
	cp.Sig = ""
	b, _ := json.Marshal(&cp)
	return b
}

// chainHash computes the prev_hash that the next record should carry.
// It hashes the record's signing bytes plus its signature so a forged
// signature would also break the chain.
func chainHash(r *Record) string {
	h := sha256.New()
	h.Write(recordSigningBytes(r))
	h.Write([]byte(r.Sig))
	return hex.EncodeToString(h.Sum(nil))
}

func sign(key ed25519.PrivateKey, msg []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, msg))
}
