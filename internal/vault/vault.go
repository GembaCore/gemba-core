// Package vault is the local-only secrets vault that backs Gemba Remote
// M3 (gm-o9t8.3.7). It stores per-workspace secrets at rest using an
// envelope-encryption scheme: a 256-bit key-encryption key (KEK) wraps a
// per-workspace data-encryption key (DEK), and the DEK encrypts each
// secret value via AES-256-GCM. Plaintext is never persisted and is
// zeroized after the caller's copy is returned.
//
// The API is intentionally small and synchronous. A future KMS-backed
// backend will satisfy the same interface; this v1 is bolt-on-disk only.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ErrNotFound is returned from Get when the (wsid, key) tuple has no
// stored value.
var ErrNotFound = errors.New("vault: key not found")

// Auditor is the injection seam the real audit log binds onto. The
// function is invoked for Put, Delete, and user-initiated Get calls
// (system gets pass actor="system" and are skipped). A nil Auditor on
// Options is fine — vault.New replaces it with a no-op.
type Auditor func(action, wsid, key string)

// Options configures vault construction. Path is required for production
// use (an in-memory option may follow later); KEK may be nil, in which
// case New reads the hex-encoded master key from GEMBA_VAULT_KEY. If
// neither is provided, New returns an error so the operator can choose
// the ephemeral-key path explicitly at the call site.
type Options struct {
	// Path is the on-disk path the bolt database lives at. The parent
	// directory is created with 0700 if missing.
	Path string

	// KEK is the 32-byte master key used to wrap per-workspace DEKs.
	// When nil, New reads GEMBA_VAULT_KEY as a 64-char hex string.
	KEK []byte

	// Auditor receives action/wsid/key tuples for non-system mutations
	// and reads. Optional; nil → noop.
	Auditor Auditor
}

// Vault is the API contract locked in decision gm-o9t8.3.7. Implementations
// must be safe for concurrent use.
type Vault interface {
	Put(ctx context.Context, wsid, key string, value []byte) error
	Get(ctx context.Context, wsid, key string) ([]byte, error)
	Delete(ctx context.Context, wsid, key string) error
	List(ctx context.Context, wsid string) ([]string, error)
	Inject(ctx context.Context, wsid string) (map[string]string, error)
}

// Constants for bolt bucket names + crypto parameters. The leading
// underscore on _meta/_deks prevents collision with legitimate wsid
// names (no operator names a workspace "_meta").
const (
	bucketMeta = "_meta"
	bucketDEKs = "_deks"

	kekSize   = 32 // AES-256
	dekSize   = 32
	nonceSize = 12 // GCM standard nonce size
)

// systemActor marks Get calls that originate from the server itself
// (Inject). Audit hooks should skip these so the audit log doesn't fill
// with noise; today the auditor doesn't get an actor argument, so we
// surface the distinction by suppressing the call entirely at the
// `system: true` path.
type ctxKey struct{}

var systemCtxKey ctxKey

// WithSystemActor flags ctx as a system-internal call. Audit hooks
// suppress Get notifications when ctx carries this marker. Put and
// Delete always emit (they always represent operator-visible writes).
func WithSystemActor(ctx context.Context) context.Context {
	return context.WithValue(ctx, systemCtxKey, true)
}

func isSystemActor(ctx context.Context) bool {
	v, _ := ctx.Value(systemCtxKey).(bool)
	return v
}

// boltVault is the on-disk implementation. The mutex covers DEK cache
// reads + writes; bolt's own locking serializes filesystem updates.
type boltVault struct {
	db      *bolt.DB
	kek     cipher.AEAD
	auditor Auditor

	mu   sync.Mutex
	deks map[string]cipher.AEAD // wsid → decrypted DEK AEAD
}

// New constructs a vault using the supplied options. The bolt file is
// created with 0600 permissions if it does not already exist. The
// caller is responsible for the lifecycle of the returned Vault —
// callers that need to close the underlying bolt handle should type-
// assert to io.Closer.
func New(opts Options) (Vault, error) {
	kek := opts.KEK
	if kek == nil {
		raw := os.Getenv("GEMBA_VAULT_KEY")
		if raw == "" {
			return nil, errors.New("vault: KEK not provided and GEMBA_VAULT_KEY unset")
		}
		decoded, err := hex.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("vault: GEMBA_VAULT_KEY hex decode: %w", err)
		}
		kek = decoded
	}
	if len(kek) != kekSize {
		return nil, fmt.Errorf("vault: KEK must be %d bytes, got %d", kekSize, len(kek))
	}

	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("vault: aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault: gcm: %w", err)
	}

	if opts.Path == "" {
		return nil, errors.New("vault: Path is required")
	}
	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o700); err != nil {
		return nil, fmt.Errorf("vault: mkdir parent: %w", err)
	}
	db, err := bolt.Open(opts.Path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("vault: bolt open: %w", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketMeta)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketDEKs)); err != nil {
			return err
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("vault: bootstrap buckets: %w", err)
	}

	auditor := opts.Auditor
	if auditor == nil {
		auditor = func(string, string, string) {}
	}
	return &boltVault{
		db:      db,
		kek:     aead,
		auditor: auditor,
		deks:    make(map[string]cipher.AEAD),
	}, nil
}

// Close releases the underlying bolt handle. Provided so tests and the
// server shutdown path can release the file lock without reflection.
func (v *boltVault) Close() error {
	return v.db.Close()
}

// dekFor returns the AEAD for wsid, creating a new DEK on first use.
// The DEK is sealed with the KEK and stored in the _deks bucket; the
// plaintext DEK is cached in memory for subsequent calls.
func (v *boltVault) dekFor(wsid string, create bool) (cipher.AEAD, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if aead, ok := v.deks[wsid]; ok {
		return aead, nil
	}

	var dek []byte
	err := v.db.Update(func(tx *bolt.Tx) error {
		deks := tx.Bucket([]byte(bucketDEKs))
		if deks == nil {
			return errors.New("vault: _deks bucket missing")
		}
		sealed := deks.Get([]byte(wsid))
		if sealed != nil {
			pt, err := openSealed(v.kek, sealed)
			if err != nil {
				return fmt.Errorf("decrypt DEK: %w", err)
			}
			dek = pt
			return nil
		}
		if !create {
			return errNoDEK
		}
		dek = make([]byte, dekSize)
		if _, err := io.ReadFull(rand.Reader, dek); err != nil {
			return err
		}
		s, err := sealValue(v.kek, dek)
		if err != nil {
			return err
		}
		return deks.Put([]byte(wsid), s)
	})
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(dek)
	zero(dek)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	v.deks[wsid] = aead
	return aead, nil
}

// errNoDEK is a private sentinel for the "no DEK yet, don't create"
// branch in dekFor. Callers that hit it treat it as ErrNotFound (a
// workspace with no DEK has no secrets).
var errNoDEK = errors.New("vault: no DEK for workspace")

// Put stores value under (wsid, key). The first Put for a workspace
// generates the DEK lazily. value is sealed with the DEK and committed
// to the wsid's bucket.
func (v *boltVault) Put(ctx context.Context, wsid, key string, value []byte) error {
	if err := validatePathParts(wsid, key); err != nil {
		return err
	}
	aead, err := v.dekFor(wsid, true)
	if err != nil {
		return err
	}
	sealed, err := sealValue(aead, value)
	if err != nil {
		return err
	}
	err = v.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(wsid))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), sealed)
	})
	if err != nil {
		return err
	}
	v.auditor("put", wsid, key)
	return nil
}

// Get returns the plaintext value for (wsid, key). The returned slice
// is a fresh allocation; the intermediate plaintext buffer is zeroized
// after the copy.
func (v *boltVault) Get(ctx context.Context, wsid, key string) ([]byte, error) {
	if err := validatePathParts(wsid, key); err != nil {
		return nil, err
	}
	aead, err := v.dekFor(wsid, false)
	if err != nil {
		if errors.Is(err, errNoDEK) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var sealed []byte
	err = v.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(wsid))
		if b == nil {
			return ErrNotFound
		}
		v := b.Get([]byte(key))
		if v == nil {
			return ErrNotFound
		}
		sealed = append([]byte(nil), v...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	pt, err := openSealed(aead, sealed)
	if err != nil {
		return nil, err
	}
	// Return a copy and zeroize the working buffer so the plaintext
	// has a single live representation in caller memory.
	out := append([]byte(nil), pt...)
	zero(pt)
	if !isSystemActor(ctx) {
		v.auditor("get", wsid, key)
	}
	return out, nil
}

// Delete removes (wsid, key). Delete is idempotent — missing tuples
// return nil. The audit hook fires even on the no-op path so an audit
// reader can correlate operator intent regardless of state.
func (v *boltVault) Delete(ctx context.Context, wsid, key string) error {
	if err := validatePathParts(wsid, key); err != nil {
		return err
	}
	err := v.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(wsid))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
	if err != nil {
		return err
	}
	v.auditor("delete", wsid, key)
	return nil
}

// List returns the keys stored under wsid, sorted lexicographically. It
// never returns values — the operator surface for "what secrets exist"
// is keys-only by design (decision gm-o9t8.3.7).
func (v *boltVault) List(ctx context.Context, wsid string) ([]string, error) {
	if err := validateWSID(wsid); err != nil {
		return nil, err
	}
	var keys []string
	err := v.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(wsid))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, _ []byte) error {
			keys = append(keys, string(k))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	return keys, nil
}

// Inject returns the (key → value) map a process spawner can splat into
// the child environment. Implemented in terms of List + Get so a future
// backend doesn't need to re-derive the join semantics. Marked as a
// system-actor call so the audit hook does not log a Get per key.
func (v *boltVault) Inject(ctx context.Context, wsid string) (map[string]string, error) {
	keys, err := v.List(ctx, wsid)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(keys))
	ctx = WithSystemActor(ctx)
	for _, k := range keys {
		val, err := v.Get(ctx, wsid, k)
		if err != nil {
			return nil, err
		}
		out[k] = string(val)
		zero(val)
	}
	return out, nil
}

// --- helpers ---------------------------------------------------------------

// sealValue encrypts pt under aead with a fresh nonce, returning
// nonce||ciphertext||tag.
func sealValue(aead cipher.AEAD, pt []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := aead.Seal(nil, nonce, pt, nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

// openSealed reverses sealValue.
func openSealed(aead cipher.AEAD, sealed []byte) ([]byte, error) {
	if len(sealed) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("vault: sealed value too short")
	}
	nonce := sealed[:aead.NonceSize()]
	ct := sealed[aead.NonceSize():]
	return aead.Open(nil, nonce, ct, nil)
}

// zero wipes b's contents. Used after returning copies of plaintext so
// the working buffer doesn't linger in Go's heap.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// validateWSID rejects empty / reserved workspace ids. The two
// underscore-prefixed names are reserved for vault metadata buckets.
func validateWSID(wsid string) error {
	if wsid == "" {
		return errors.New("vault: wsid required")
	}
	if wsid == bucketMeta || wsid == bucketDEKs {
		return fmt.Errorf("vault: wsid %q is reserved", wsid)
	}
	return nil
}

// validatePathParts is the combined wsid+key guard the per-tuple
// methods share.
func validatePathParts(wsid, key string) error {
	if err := validateWSID(wsid); err != nil {
		return err
	}
	if key == "" {
		return errors.New("vault: key required")
	}
	return nil
}
