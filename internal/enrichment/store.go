package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is the per-bead enrichment surface. The CLI / planner /
// future LLM-extraction path read and write through this interface
// so the production backend (today: file; tomorrow: bd extras when
// gm-s47n.1.1 lands) can swap without touching call sites.
//
// Implementations MUST be safe for concurrent use; the planner reads
// in a fan-out and the CLI may run in parallel from a script.
type Store interface {
	// Load returns the enrichment for beadID. ErrNotFound when the
	// bead has no recorded enrichment yet — callers treat this as
	// "empty Enrichment for that bead", same as the bd-extras
	// backend will when the field is absent.
	Load(ctx context.Context, beadID string) (Enrichment, error)

	// Save replaces the enrichment for beadID. UpdatedAt is stamped
	// by the implementation if the input leaves it zero. An empty
	// Enrichment (no targets, no concepts) is still persisted so a
	// later Load returns the empty value rather than ErrNotFound —
	// that distinguishes "explicitly cleared" from "never set."
	Save(ctx context.Context, e Enrichment) error

	// List returns every bead-id that has a recorded enrichment, in
	// stable order. The CLI's "list all" surface uses this; the
	// planner's bulk read also goes through it.
	List(ctx context.Context) ([]string, error)
}

// ErrNotFound is the typed sentinel Store.Load returns when a bead
// has no recorded enrichment. CLI callers branch on errors.Is so
// the message stays consistent across implementations.
var ErrNotFound = errors.New("enrichment: not found")

// MemoryStore is the in-memory implementation. Production-grade
// for tests and CLI dry-runs; the file-backed [FileStore] adds
// persistence without changing the contract.
type MemoryStore struct {
	mu     sync.RWMutex
	byBead map[string]Enrichment
	now    func() time.Time
}

// NewMemoryStore returns an empty store. Optional now hook lets tests
// pin UpdatedAt; production leaves it nil (defaults to time.Now().UTC()).
func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemoryStore{byBead: make(map[string]Enrichment), now: now}
}

// Load implements [Store].
func (s *MemoryStore) Load(_ context.Context, beadID string) (Enrichment, error) {
	if beadID == "" {
		return Enrichment{}, fmt.Errorf("enrichment: Load requires bead id")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byBead[beadID]
	if !ok {
		return Enrichment{}, ErrNotFound
	}
	return e.copy(), nil
}

// Save implements [Store].
func (s *MemoryStore) Save(_ context.Context, e Enrichment) error {
	if e.BeadID == "" {
		return fmt.Errorf("enrichment: Save requires BeadID")
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = s.now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byBead[e.BeadID] = e.copy()
	return nil
}

// List implements [Store].
func (s *MemoryStore) List(_ context.Context) ([]string, error) {
	s.mu.RLock()
	ids := make([]string, 0, len(s.byBead))
	for id := range s.byBead {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	sort.Strings(ids)
	return ids, nil
}

// FileStore persists enrichments under <root>/.gemba/enrichment/.
// One file per bead so concurrent edits to different beads don't
// fight each other; the bd id's '/' separator is escaped to '__' so
// workspace-prefixed ids round-trip across filesystems.
type FileStore struct {
	root string
	now  func() time.Time
	mu   sync.Mutex // serializes writes; reads are unlocked (atomic rename)
}

// NewFileStore returns a store rooted at <workspace>/.gemba/enrichment/.
// The directory is created lazily on the first Save call so a
// fresh workspace doesn't carry an empty enrichment dir.
func NewFileStore(workspace string, now func() time.Time) *FileStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FileStore{
		root: filepath.Join(workspace, ".gemba", "enrichment"),
		now:  now,
	}
}

// Load implements [Store]. ErrNotFound when the file doesn't exist.
func (s *FileStore) Load(_ context.Context, beadID string) (Enrichment, error) {
	if beadID == "" {
		return Enrichment{}, fmt.Errorf("enrichment: Load requires bead id")
	}
	path := s.path(beadID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Enrichment{}, ErrNotFound
		}
		return Enrichment{}, fmt.Errorf("enrichment: read %s: %w", path, err)
	}
	var e Enrichment
	if err := json.Unmarshal(data, &e); err != nil {
		return Enrichment{}, fmt.Errorf("enrichment: parse %s: %w", path, err)
	}
	return e, nil
}

// Save implements [Store] with an atomic write (tmp + rename) so a
// crash mid-write never leaves a half-serialized JSON blob.
func (s *FileStore) Save(_ context.Context, e Enrichment) error {
	if e.BeadID == "" {
		return fmt.Errorf("enrichment: Save requires BeadID")
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = s.now()
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("enrichment: mkdir %s: %w", s.root, err)
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("enrichment: marshal: %w", err)
	}
	path := s.path(e.BeadID)
	tmp := path + ".tmp"

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("enrichment: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("enrichment: rename %s: %w", path, err)
	}
	return nil
}

// List implements [Store].
func (s *FileStore) List(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("enrichment: read %s: %w", s.root, err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		safe := strings.TrimSuffix(e.Name(), ".json")
		ids = append(ids, unsafeID(safe))
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *FileStore) path(beadID string) string {
	return filepath.Join(s.root, safeID(beadID)+".json")
}

// safeID escapes the bd id's '/' separator so workspace-prefixed
// ids (gemba/gemba/gm-1) become file-safe (gemba__gemba__gm-1).
// Round-trips via [unsafeID].
func safeID(id string) string {
	return strings.ReplaceAll(id, "/", "__")
}

func unsafeID(safe string) string {
	return strings.ReplaceAll(safe, "__", "/")
}
