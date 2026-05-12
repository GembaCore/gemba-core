// Image verification for Firecracker VM artifacts (gm-o9t8.3.2.2).
//
// A "VM image" in Gemba is the triple (vmlinux, rootfs.ext4, manifest.json).
// The kernel + rootfs are produced by the Buildroot pipeline in
// `vm-image/` on a Linux build host; the manifest is a small JSON file
// describing the artifacts and their sha256 hashes.
//
// VerifyImage is called by the Firecracker supervisor before booting any
// VM to refuse-to-start on a corrupted or tampered image. This is the
// runtime half of the supply-chain story; the build half (signing) lives
// in `vm-image/scripts/`.
//
// This file is plain Go — no Linux-only build tags — so it compiles on
// every dev host (macOS/Windows/Linux). The fallback supervisor on
// non-Linux hosts simply never calls into it (no real image to verify).
package firecracker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Manifest is the on-disk schema for `manifest.json` shipped alongside
// the kernel + rootfs. KernelSHA256 / RootfsSHA256 are lower-case hex
// digests. BuiltAt is RFC 3339. KernelVersion is the upstream Linux
// version string (e.g. "6.1.123"). RootfsSizeBytes is the byte length
// of the rootfs file at build time and is informational only — the
// authoritative integrity check is the sha256.
//
// Signature is optional; when present it is the base64-encoded ed25519
// signature over the canonical JSON of the *unsigned* manifest (i.e.
// the manifest with Signature emptied). The signing pubkey distribution
// model lives in `docs/server/vm-image.md` and is out of scope for v1.
type Manifest struct {
	KernelSHA256    string `json:"kernel_sha256"`
	RootfsSHA256    string `json:"rootfs_sha256"`
	BuiltAt         string `json:"built_at"`
	Builder         string `json:"builder"`
	KernelVersion   string `json:"kernel_version"`
	RootfsSizeBytes int64  `json:"rootfs_size_bytes"`
	Signature       string `json:"signature,omitempty"`
}

// LoadManifest reads and JSON-decodes the manifest at path. It performs
// no integrity checking against the kernel/rootfs — that's VerifyImage's
// job — but does sanity-check that the two hash fields look like hex.
func LoadManifest(path string) (*Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("firecracker: read manifest %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("firecracker: parse manifest %s: %w", path, err)
	}
	if _, err := hex.DecodeString(m.KernelSHA256); err != nil || len(m.KernelSHA256) != 64 {
		return nil, fmt.Errorf("firecracker: manifest %s: kernel_sha256 is not a 64-char hex digest", path)
	}
	if _, err := hex.DecodeString(m.RootfsSHA256); err != nil || len(m.RootfsSHA256) != 64 {
		return nil, fmt.Errorf("firecracker: manifest %s: rootfs_sha256 is not a 64-char hex digest", path)
	}
	return &m, nil
}

// VerifyImage recomputes the sha256 of kernelPath and rootfsPath and
// returns nil iff both match the digests recorded in manifestPath.
// Any I/O error or mismatch is surfaced as a non-nil error. Callers
// (the Firecracker supervisor at boot) treat a non-nil return as a
// hard refusal to start the VM.
func VerifyImage(kernelPath, rootfsPath, manifestPath string) error {
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return err
	}

	kSum, err := sha256File(kernelPath)
	if err != nil {
		return fmt.Errorf("firecracker: hash kernel: %w", err)
	}
	if kSum != m.KernelSHA256 {
		return fmt.Errorf("firecracker: kernel sha256 mismatch: got %s, manifest %s", kSum, m.KernelSHA256)
	}

	rSum, err := sha256File(rootfsPath)
	if err != nil {
		return fmt.Errorf("firecracker: hash rootfs: %w", err)
	}
	if rSum != m.RootfsSHA256 {
		return fmt.Errorf("firecracker: rootfs sha256 mismatch: got %s, manifest %s", rSum, m.RootfsSHA256)
	}
	return nil
}

// sha256File streams the file at path through sha256 and returns the
// lower-case hex digest.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// DefaultImagePaths looks for a (kernel, rootfs, manifest) triple in the
// well-known locations a gemba server install might place them, in
// priority order:
//
//  1. $GEMBA_VM_DIR (operator override)
//  2. ~/.gemba/vm/   (single-user / dev install)
//  3. /usr/local/share/gemba/vm/  (system-wide install)
//
// The first directory containing all three files wins. ok is false if
// none of the candidates contain a full set; in that case the supervisor
// is expected to surface a clear "no VM image installed" error before
// any dispatch attempt.
func DefaultImagePaths() (kernel, rootfs, manifest string, ok bool) {
	var candidates []string
	if env := os.Getenv("GEMBA_VM_DIR"); env != "" {
		candidates = append(candidates, env)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".gemba", "vm"))
	}
	candidates = append(candidates, "/usr/local/share/gemba/vm")

	for _, dir := range candidates {
		k := filepath.Join(dir, "vmlinux")
		r := filepath.Join(dir, "rootfs.ext4")
		m := filepath.Join(dir, "manifest.json")
		if fileExists(k) && fileExists(r) && fileExists(m) {
			return k, r, m, true
		}
	}
	return "", "", "", false
}

// fileExists reports whether path names a regular file that can be
// stat'd. Symlinks are followed (the production deploy may symlink
// versioned artifacts into the well-known dir).
func fileExists(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.Mode().IsRegular()
}

// ErrNoImageInstalled is returned by helpers that need a default image
// triple but DefaultImagePaths found none. The supervisor maps this to
// a user-facing "install the gemba VM image" message.
var ErrNoImageInstalled = errors.New("firecracker: no VM image found in $GEMBA_VM_DIR, ~/.gemba/vm, or /usr/local/share/gemba/vm")
