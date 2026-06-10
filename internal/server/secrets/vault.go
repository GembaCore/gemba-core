package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/hkdf"
)

// MasterKeyEnv is the environment variable that holds the base64-encoded
// 32-byte server master key. Required for any Vault.Open call.
const MasterKeyEnv = "GEMBA_VAULT_KEY"

// MasterKeyLen is the required raw length of the server master key.
const MasterKeyLen = 32

// nonceLen is the AES-GCM nonce length in bytes.
const nonceLen = 12

// secretsFileName is the on-disk filename for a workspace's encrypted blob.
const secretsFileName = "secrets.enc"

// ErrNotFound is returned by Inject when the named secret does not exist.
var ErrNotFound = errors.New("secrets: secret not found")

// ErrMasterKeyMissing is returned when the master key cannot be resolved.
var ErrMasterKeyMissing = errors.New("secrets: master key missing (set " + MasterKeyEnv + ")")

// ErrMasterKeyMalformed is returned when the master key is set but malformed.
var ErrMasterKeyMalformed = errors.New("secrets: master key malformed (must be base64-encoded 32 bytes)")

// ErrTampered is returned when the on-disk blob fails AEAD verification.
var ErrTampered = errors.New("secrets: vault blob failed integrity check")

// Vault is a per-workspace secret store. All methods are safe for
// concurrent use. The zero value is unusable; obtain a Vault via Open.
type Vault struct {
	path         string
	workspaceID  string
	workspaceKey [32]byte

	mu sync.Mutex
}

// Open returns the Vault for a workspace, creating an empty encrypted
// blob if one does not yet exist. The dataDir argument is the gemba
// server's workspace root (e.g. /var/lib/gemba/workspaces); workspaceID
// is the unique workspace identifier. masterKey is the 32-byte server
// master key — obtain it via MasterKeyFromEnv.
func Open(dataDir, workspaceID string, masterKey [MasterKeyLen]byte) (*Vault, error) {
	if workspaceID == "" {
		return nil, errors.New("secrets: workspaceID is required")
	}
	wsDir := filepath.Join(dataDir, workspaceID)
	if err := os.MkdirAll(wsDir, 0o700); err != nil {
		return nil, fmt.Errorf("secrets: create workspace dir: %w", err)
	}
	v := &Vault{
		path:        filepath.Join(wsDir, secretsFileName),
		workspaceID: workspaceID,
	}
	if err := deriveKey(masterKey, workspaceID, v.workspaceKey[:]); err != nil {
		return nil, err
	}
	// Touch an empty vault if missing, so callers don't have to check.
	if _, err := os.Stat(v.path); errors.Is(err, os.ErrNotExist) {
		if err := v.write(map[string][]byte{}); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// Put stores name=value in the workspace vault. value bytes are encrypted
// at rest and never written to disk in plaintext.
func (v *Vault) Put(name string, value []byte) error {
	if name == "" {
		return errors.New("secrets: name is required")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	cur, err := v.read()
	if err != nil {
		return err
	}
	cur[name] = append([]byte(nil), value...)
	return v.write(cur)
}

// Delete removes the named secret. Returns nil even if the name did not
// exist (idempotent).
func (v *Vault) Delete(name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	cur, err := v.read()
	if err != nil {
		return err
	}
	delete(cur, name)
	return v.write(cur)
}

// List returns the names of all secrets in the workspace. Values are not
// returned by design.
func (v *Vault) List() ([]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	cur, err := v.read()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cur))
	for k := range cur {
		out = append(out, k)
	}
	return out, nil
}

// Inject returns a copy of the named secret. The caller MUST clear the
// returned slice (e.g. via Zero) after use. This is the only API that
// reveals secret material and exists to be called from the dispatch
// boundary, not from API handlers.
func (v *Vault) Inject(name string) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	cur, err := v.read()
	if err != nil {
		return nil, err
	}
	raw, ok := cur[name]
	if !ok {
		return nil, ErrNotFound
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out, nil
}

// Zero wipes the contents of b. Use after Inject to reduce the lifetime
// of secret material in process memory.
func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// MasterKeyFromEnv parses the base64-encoded master key from $GEMBA_VAULT_KEY.
func MasterKeyFromEnv() ([MasterKeyLen]byte, error) {
	var out [MasterKeyLen]byte
	v := os.Getenv(MasterKeyEnv)
	if v == "" {
		return out, ErrMasterKeyMissing
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		// Try URL-safe encoding too.
		raw, err = base64.URLEncoding.DecodeString(v)
		if err != nil {
			return out, ErrMasterKeyMalformed
		}
	}
	if len(raw) != MasterKeyLen {
		return out, ErrMasterKeyMalformed
	}
	copy(out[:], raw)
	return out, nil
}

// GenerateMasterKey returns a freshly generated 32-byte master key.
// Intended for tests and the `gemba secrets gen-master-key` CLI helper.
func GenerateMasterKey() ([MasterKeyLen]byte, error) {
	var out [MasterKeyLen]byte
	if _, err := io.ReadFull(rand.Reader, out[:]); err != nil {
		return out, err
	}
	return out, nil
}

// EncodeMasterKey is the inverse of MasterKeyFromEnv's parsing step.
func EncodeMasterKey(k [MasterKeyLen]byte) string {
	return base64.StdEncoding.EncodeToString(k[:])
}

// --- internal --------------------------------------------------------

// read decrypts the workspace blob and returns the plaintext secret map.
// Caller must hold v.mu.
func (v *Vault) read() (map[string][]byte, error) {
	blob, err := os.ReadFile(v.path)
	if err != nil {
		return nil, fmt.Errorf("secrets: read blob: %w", err)
	}
	if len(blob) < nonceLen+16 { // nonce + min tag
		// New, empty blob written by Open.
		if len(blob) == 0 {
			return map[string][]byte{}, nil
		}
		return nil, ErrTampered
	}
	aead, err := v.aead()
	if err != nil {
		return nil, err
	}
	nonce := blob[:nonceLen]
	ct := blob[nonceLen:]
	pt, err := aead.Open(nil, nonce, ct, []byte(v.workspaceID))
	if err != nil {
		return nil, ErrTampered
	}
	out := map[string][]byte{}
	if err := json.Unmarshal(pt, &out); err != nil {
		return nil, fmt.Errorf("secrets: decode plaintext: %w", err)
	}
	return out, nil
}

// write atomically replaces the encrypted blob with an encryption of m.
// Caller must hold v.mu.
func (v *Vault) write(m map[string][]byte) error {
	pt, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("secrets: encode plaintext: %w", err)
	}
	aead, err := v.aead()
	if err != nil {
		return err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("secrets: nonce: %w", err)
	}
	ct := aead.Seal(nil, nonce, pt, []byte(v.workspaceID))
	blob := make([]byte, 0, len(nonce)+len(ct))
	blob = append(blob, nonce...)
	blob = append(blob, ct...)
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("secrets: write tmp: %w", err)
	}
	if err := os.Rename(tmp, v.path); err != nil {
		return fmt.Errorf("secrets: rename: %w", err)
	}
	return nil
}

func (v *Vault) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(v.workspaceKey[:])
	if err != nil {
		return nil, fmt.Errorf("secrets: aes init: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: gcm init: %w", err)
	}
	return aead, nil
}

// deriveKey expands the server master key into a per-workspace key
// using HKDF-SHA256, with the workspace ID as the info parameter so
// keys are deterministic per workspace but unrelated across workspaces.
func deriveKey(master [MasterKeyLen]byte, workspaceID string, out []byte) error {
	h := hkdf.New(sha256.New, master[:], []byte("gemba-secrets-v1"), []byte(workspaceID))
	if _, err := io.ReadFull(h, out); err != nil {
		return fmt.Errorf("secrets: derive workspace key: %w", err)
	}
	return nil
}
