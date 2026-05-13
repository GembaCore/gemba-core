// Package lockfile defines the spec ↔ bead mapping file written next to a
// spec.md. It is the source of truth the reconciler consults to decide
// which anchors map to which bd IDs.
//
// File layout: <spec_dir>/.spec.lock.json
//
//   {
//     "spec_id":   "SP-001",
//     "nonce":     "uuid v4",
//     "applied_at":"2026-05-13T00:00:00Z",
//     "mappings": [
//       {"anchor":"M-01","bead_id":"gm-v0sp.7","content_hash":"sha256:..."}
//     ]
//   }
//
// Writes are atomic (temp file + rename) so the file is never observed in
// a half-written state.
package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/spec/parser"
)

// Errors.
var (
	ErrMissingNonce = errors.New("lockfile: missing nonce")
	ErrEmpty        = errors.New("lockfile: empty or unreadable")
)

// Mapping links one spec anchor to a bd bead.
//
// Resolution is an optional operator annotation used by the closure gate
// (gm-v0sp.12). Empty string means "no annotation" (default; treated as
// open). Recognized values:
//
//	"wontdo" — story was deliberately abandoned; closure gate treats it
//	           as resolved even if the bd bead is still in an open status.
//	"done"   — informational; the gate prefers the bd bead status itself.
//
// The field is omitempty so older lockfiles (pre-gm-v0sp.12) continue to
// load and serialize identically. New writers SHOULD NOT emit Resolution
// unless an operator has set one.
type Mapping struct {
	Anchor      string `json:"anchor"`
	BeadID      string `json:"bead_id"`
	ContentHash string `json:"content_hash"`
	Resolution  string `json:"resolution,omitempty"`
}

// Lock is the on-disk representation.
type Lock struct {
	SpecID    string    `json:"spec_id"`
	Nonce     string    `json:"nonce"`
	AppliedAt time.Time `json:"applied_at"`
	Mappings  []Mapping `json:"mappings"`
}

// Load reads a lockfile from path. Returns ErrMissingNonce if nonce is
// blank (drift-detection safeguard).
func Load(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrEmpty
	}
	var lock Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("lockfile: unmarshal: %w", err)
	}
	if strings.TrimSpace(lock.Nonce) == "" {
		return nil, ErrMissingNonce
	}
	return &lock, nil
}

// Save writes lock to path atomically (temp file + rename).
func Save(path string, lock *Lock) error {
	if strings.TrimSpace(lock.Nonce) == "" {
		return ErrMissingNonce
	}
	if lock.Mappings == nil {
		lock.Mappings = []Mapping{}
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".spec.lock.json.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
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
	return os.Rename(tmpName, path)
}

// Diff compares a parsed spec against an existing lock and partitions
// mappings into three sets:
//
//   creates: anchors present in spec but absent from lock (need new beads)
//   updates: anchors present in both but whose content hash changed
//   orphans: anchors in lock but absent from spec (candidates for close)
//
// `creates` and `updates` use the spec's current content hash; `orphans`
// echo back the stored lock mapping unchanged.
func Diff(spec *parser.Spec, lock *Lock) (creates, updates, orphans []Mapping) {
	specAnchors := map[string]string{} // anchor -> content hash
	for _, m := range spec.Milestones {
		specAnchors[m.Anchor] = HashMilestone(m)
	}
	for _, e := range spec.Epics {
		specAnchors[e.Anchor] = HashEpic(e)
	}
	for _, s := range spec.Stories {
		specAnchors[s.Anchor] = HashStory(s)
	}

	lockByAnchor := map[string]Mapping{}
	if lock != nil {
		for _, m := range lock.Mappings {
			lockByAnchor[m.Anchor] = m
		}
	}

	// Iterate spec anchors in a stable order.
	for _, anchor := range orderedAnchors(spec) {
		hash := specAnchors[anchor]
		if existing, ok := lockByAnchor[anchor]; ok {
			if existing.ContentHash != hash {
				updates = append(updates, Mapping{
					Anchor:      anchor,
					BeadID:      existing.BeadID,
					ContentHash: hash,
				})
			}
			continue
		}
		creates = append(creates, Mapping{Anchor: anchor, ContentHash: hash})
	}

	// Orphans: anchors only in lock.
	for _, m := range lockByAnchor {
		if _, ok := specAnchors[m.Anchor]; !ok {
			orphans = append(orphans, m)
		}
	}
	return creates, updates, orphans
}

func orderedAnchors(spec *parser.Spec) []string {
	out := make([]string, 0, len(spec.Milestones)+len(spec.Epics)+len(spec.Stories))
	for _, m := range spec.Milestones {
		out = append(out, m.Anchor)
	}
	for _, e := range spec.Epics {
		out = append(out, e.Anchor)
	}
	for _, s := range spec.Stories {
		out = append(out, s.Anchor)
	}
	return out
}

// HashStory computes a stable content hash for a story. It includes the
// title, status, parent, priority, AC list, and body so that any
// substantive edit triggers an update.
func HashStory(s parser.Story) string {
	h := sha256.New()
	fmt.Fprintf(h, "title=%s\nstatus=%s\nparent=%s\npriority=%s\n", s.Title, s.Status, s.Parent, s.Priority)
	for _, ac := range s.AC {
		fmt.Fprintf(h, "ac=%s\n", ac)
	}
	fmt.Fprintf(h, "body=%s", s.Body)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// HashEpic computes a content hash for an epic.
func HashEpic(e parser.Epic) string {
	h := sha256.New()
	fmt.Fprintf(h, "title=%s\nbody=%s", e.Title, e.Body)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// HashMilestone computes a content hash for a milestone.
func HashMilestone(m parser.Milestone) string {
	h := sha256.New()
	fmt.Fprintf(h, "title=%s\nbody=%s", m.Title, m.Body)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
