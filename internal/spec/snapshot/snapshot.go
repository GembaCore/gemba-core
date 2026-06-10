// Package snapshot writes content-addressed, frozen copies of a spec.md.
//
// Snapshots are the pre-apply safety net for `gemba spec apply`: before the
// reconciler mutates beads, the spec is frozen so we can diff/audit/revert.
// Filenames embed the spec_id, a nonce prefix, and a unix timestamp so each
// run produces an immutable, sortable artifact under <spec_dir>/.snapshots/.
package snapshot

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/GembaCore/gemba-core/internal/spec/parser"
)

// ErrMissingNonce is returned when the spec's frontmatter lacks a nonce.
// Snapshots without a nonce are uncorrelatable, so we refuse.
var ErrMissingNonce = errors.New("snapshot: spec frontmatter is missing 'nonce'")

// Take reads specPath, validates the spec has a nonce, and writes an atomic,
// frozen copy under <dir(specPath)>/.snapshots/. Returns the absolute path of
// the snapshot.
//
// Filename pattern: <spec_id>-<nonce-prefix>-<unix>.md
// When two snapshots collide within the same second (same spec_id+nonce), a
// short random suffix is appended: <...>-<unix>-<rand>.md.
func Take(specPath string) (string, error) {
	specPath, err := filepath.Abs(specPath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		return "", err
	}
	spec, err := parser.Parse(data)
	if err != nil {
		// parser already rejects missing nonce with ErrMissingNonce; normalize.
		if errors.Is(err, parser.ErrMissingNonce) {
			return "", ErrMissingNonce
		}
		return "", err
	}
	if spec.Frontmatter.Nonce == "" {
		return "", ErrMissingNonce
	}

	dir := filepath.Join(filepath.Dir(specPath), ".snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	specID := spec.Frontmatter.SpecID
	if specID == "" {
		specID = "spec"
	}
	noncePrefix := spec.Frontmatter.Nonce
	if len(noncePrefix) > 8 {
		noncePrefix = noncePrefix[:8]
	}
	ts := time.Now().Unix()
	base := fmt.Sprintf("%s-%s-%d.md", specID, noncePrefix, ts)
	out := filepath.Join(dir, base)

	// If a snapshot for this (spec_id, nonce, second) already exists, append
	// a short random tag so two snapshots taken inside the same second can
	// coexist as distinct artifacts.
	if _, err := os.Stat(out); err == nil {
		tag, err := randHex(3)
		if err != nil {
			return "", err
		}
		base = fmt.Sprintf("%s-%s-%d-%s.md", specID, noncePrefix, ts, tag)
		out = filepath.Join(dir, base)
	}

	if err := atomicWrite(out, data); err != nil {
		return "", err
	}
	return out, nil
}

// atomicWrite writes data to a sibling tmp file in the same directory, fsyncs,
// then renames into place. Safe across crashes on the same filesystem.
func atomicWrite(dest string, data []byte) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".snapshot-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Always clean the tmp file if we fail before the rename succeeds.
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, byteReader(data)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}

func byteReader(b []byte) io.Reader { return &readBuf{b: b} }

type readBuf struct {
	b []byte
	i int
}

func (r *readBuf) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
