package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Repository is one git repository associated with a workspace
// (gm-26n4). A workspace (≡ project) holds one beads database and
// ONE-OR-MORE repositories. Every [WorkItem] is associated with one
// or more [Repository]s via [WorkItem.RepositoryIDs] (gm-kdh3); the
// spawn path reads [WorkItem.PrimaryRepositoryID] to pick the working
// directory and worktree pool for an agent session.
//
// Repository is the on-disk reality (path, branch, origin); the
// abstract notion of "what work this is" stays on WorkItem. Splitting
// the two lets one repository back many work items without storing
// per-bead path/branch metadata, and lets the same WorkItem schema
// project across adaptors that have nothing like a filesystem
// (LangGraph runs, Jira issues with no linked repo) by leaving
// RepositoryID = [RepositoryUnspecified].
type Repository struct {
	// ID is the slug ("gemba", "frontend", "infra"). Matches the
	// filename stem of the `.gemba/repositories/<id>.toml` file the
	// repository was loaded from. Appears as an entry in
	// [WorkItem.RepositoryIDs] and (when applicable) as the value of
	// [WorkItem.PrimaryRepositoryID].
	ID RepositoryID `toml:"id" json:"id"`

	// Path is the absolute filesystem path to the canonical worktree
	// root. Required + must be absolute so spawn paths are
	// reproducible across sessions.
	Path string `toml:"path" json:"path"`

	// DefaultBranch is the branch new work checks out from when no
	// other branch is specified. Conventionally "main".
	DefaultBranch string `toml:"default_branch" json:"default_branch"`

	// URL is the origin URL (https or ssh form). Optional — the spawn
	// path uses Path; URL is metadata for the UI ("View on GitHub").
	URL string `toml:"url,omitempty" json:"url,omitempty"`

	// WorktreesDir is the directory new git worktrees are created in
	// when polecats run in parallel against this repo. Optional;
	// empty means "spawn in Path directly" (single-worktree mode,
	// suitable for solo work). Relative paths resolve against Path.
	WorktreesDir string `toml:"worktrees_dir,omitempty" json:"worktrees_dir,omitempty"`

	// BeadPrefix is the bead-id prefix that identifies this
	// repository in cross-repo contexts (gm-d2ts). With multi-repo
	// beads (gm-kdh3) and N repos in one workspace, a bare ID like
	// `gm-e3` is no longer unique without a repo qualifier; the
	// prefix carries that identity directly:
	//
	//	gemba    → "gm"  (gm-e3, gm-518)
	//	frontend → "fe"  (fe-e3, fe-bug-44)
	//	backend  → "be"  (be-e3, be-perf-12)
	//
	// Validate enforces 2–8 chars of [a-z0-9-] and uniqueness across
	// the workspace's [RepositoryRegistry]. The bd adaptor uses
	// [RepositoryRegistry.GetByPrefix] to route an inbound bead to
	// its repository when the operator hasn't explicitly tagged it
	// with a `repo:*` label.
	BeadPrefix string `toml:"bead_prefix" json:"bead_prefix"`

	// sourcePath is the filesystem path the repository was loaded
	// from, mirroring [internal/core/persona.Persona.sourcePath].
	// Unexported so it never leaks into JSON.
	sourcePath string `toml:"-" json:"-"`
}

// Validate checks the structural invariants the loader and spawn
// paths rely on. Does NOT check that Path exists on disk — a repo
// can be declared before it is cloned, and the caller (spawn) is the
// right place to surface "checkout missing" errors.
func (r Repository) Validate() error {
	if strings.TrimSpace(string(r.ID)) == "" {
		return errors.New("repository: id must not be empty")
	}
	if r.ID == RepositoryUnspecified {
		return fmt.Errorf("repository: id must not be %q (reserved sentinel)", RepositoryUnspecified)
	}
	if strings.ContainsAny(string(r.ID), `/\`) {
		return fmt.Errorf("repository: id %q must not contain path separators", r.ID)
	}
	if strings.TrimSpace(r.Path) == "" {
		return fmt.Errorf("repository %q: path must not be empty", r.ID)
	}
	if !filepath.IsAbs(r.Path) {
		return fmt.Errorf("repository %q: path %q must be absolute", r.ID, r.Path)
	}
	if strings.TrimSpace(r.DefaultBranch) == "" {
		return fmt.Errorf("repository %q: default_branch must not be empty", r.ID)
	}
	if err := validateBeadPrefix(r.BeadPrefix, r.ID); err != nil {
		return err
	}
	return nil
}

// validateBeadPrefix enforces the gm-d2ts shape rules: required,
// 2–8 chars of [a-z0-9-]. Uniqueness is enforced at registry-load
// time via [RepositoryRegistry.Register], not here — a Repository
// in isolation can't know about its peers.
func validateBeadPrefix(prefix string, repoID RepositoryID) error {
	if prefix == "" {
		return fmt.Errorf("repository %q: bead_prefix must not be empty", repoID)
	}
	if n := len(prefix); n < 2 || n > 8 {
		return fmt.Errorf(
			"repository %q: bead_prefix %q must be 2-8 characters (got %d)",
			repoID, prefix, n)
	}
	for i := 0; i < len(prefix); i++ {
		c := prefix[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			// ok
		default:
			return fmt.Errorf(
				"repository %q: bead_prefix %q contains invalid character %q (allow [a-z0-9-])",
				repoID, prefix, string(c))
		}
	}
	return nil
}

// ResolveWorktreesDir returns the absolute path to the worktree pool
// for r. Returns r.Path when WorktreesDir is empty (single-worktree
// mode); otherwise joins r.Path with WorktreesDir for relative
// values.
func (r Repository) ResolveWorktreesDir() string {
	if r.WorktreesDir == "" {
		return r.Path
	}
	if filepath.IsAbs(r.WorktreesDir) {
		return r.WorktreesDir
	}
	return filepath.Join(r.Path, r.WorktreesDir)
}

// SourcePath returns the on-disk file the Repository was loaded
// from, or "" when constructed programmatically (tests).
func (r Repository) SourcePath() string { return r.sourcePath }

// LoadRepositoryFile reads a single `.gemba/repositories/<id>.toml`
// file. The file's basename (minus .toml) MUST match the file's
// declared `id` field — same invariant the persona loader enforces.
func LoadRepositoryFile(path string) (*Repository, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("repository: read %s: %w", path, err)
	}
	var r Repository
	if _, err := toml.Decode(string(b), &r); err != nil {
		return nil, fmt.Errorf("repository: decode %s: %w", path, err)
	}
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("repository: %s: %w", path, err)
	}
	stem := strings.TrimSuffix(filepath.Base(path), repoFileExt)
	if string(r.ID) != stem {
		return nil, fmt.Errorf(
			"repository: %s: id %q does not match filename stem %q",
			path, r.ID, stem)
	}
	r.sourcePath = path
	return &r, nil
}

// LoadRepositoriesDir reads every *.toml file under dir and returns
// the parsed repositories keyed by ID. A missing dir yields an empty
// map (so a fresh workspace boots cleanly); other I/O errors
// propagate. Mirrors [internal/core/persona.LoadDir] semantics.
func LoadRepositoriesDir(dir string) (map[RepositoryID]*Repository, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[RepositoryID]*Repository{}, nil
		}
		return nil, fmt.Errorf("repository: scan %s: %w", dir, err)
	}
	out := make(map[RepositoryID]*Repository, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, repoFileExt) {
			continue
		}
		path := filepath.Join(dir, name)
		r, err := LoadRepositoryFile(path)
		if err != nil {
			return nil, err
		}
		if existing, ok := out[r.ID]; ok {
			return nil, fmt.Errorf("repository: duplicate id %q (files %s and %s)",
				r.ID, existing.sourcePath, path)
		}
		out[r.ID] = r
	}
	return out, nil
}

// RepositoryRegistry is the in-memory lookup table. Built once at
// startup (or on workspace switch) and treated as read-only for
// concurrent reads thereafter — same contract as
// [internal/core/persona.Registry].
type RepositoryRegistry struct {
	repos      map[RepositoryID]*Repository
	byPrefix   map[string]*Repository // gm-d2ts: prefix → Repository
}

// NewRepositoryRegistry returns an empty RepositoryRegistry.
func NewRepositoryRegistry() *RepositoryRegistry {
	return &RepositoryRegistry{
		repos:    make(map[RepositoryID]*Repository),
		byPrefix: make(map[string]*Repository),
	}
}

// LoadRepositoryRegistry constructs a RepositoryRegistry from every
// repository TOML file under dir. Uses Register so prefix uniqueness
// is enforced as files load (a workspace with two repos sharing a
// prefix is unbootable — the second registration fails).
func LoadRepositoryRegistry(dir string) (*RepositoryRegistry, error) {
	repos, err := LoadRepositoriesDir(dir)
	if err != nil {
		return nil, err
	}
	reg := NewRepositoryRegistry()
	// Register in deterministic order so the error message naming
	// the "first" winner of a prefix collision is reproducible.
	ids := make([]RepositoryID, 0, len(repos))
	for id := range repos {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := reg.Register(repos[id]); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

// Register adds r to the registry. Returns an error when r is nil,
// r.ID is empty, r.Validate fails, another repo is already
// registered under r.ID, or another repo has already claimed
// r.BeadPrefix (gm-d2ts). Re-registering the same pointer is a
// no-op so init paths can be defensive without erroring out.
func (r *RepositoryRegistry) Register(repo *Repository) error {
	if repo == nil {
		return errors.New("repository: Register: repository is nil")
	}
	if err := repo.Validate(); err != nil {
		return err
	}
	if existing, ok := r.repos[repo.ID]; ok {
		if existing == repo {
			return nil
		}
		return fmt.Errorf("repository: Register: id %q already registered", repo.ID)
	}
	if existing, ok := r.byPrefix[repo.BeadPrefix]; ok && existing != repo {
		return fmt.Errorf(
			"repository: Register: bead_prefix %q already claimed by repository %q (cannot also belong to %q)",
			repo.BeadPrefix, existing.ID, repo.ID)
	}
	r.repos[repo.ID] = repo
	r.byPrefix[repo.BeadPrefix] = repo
	return nil
}

// Get returns the repository registered under id. The bool is false
// when no repository is registered under that id.
func (r *RepositoryRegistry) Get(id RepositoryID) (*Repository, bool) {
	repo, ok := r.repos[id]
	return repo, ok
}

// GetByPrefix returns the repository whose [Repository.BeadPrefix]
// equals prefix (gm-d2ts). The bool is false when no registered
// repository claims that prefix. Used by the bd adaptor to route an
// inbound bead like `fe-e3` to the "frontend" repository when the
// operator has not yet (or chooses not to) tag the bead with an
// explicit `repo:*` label.
func (r *RepositoryRegistry) GetByPrefix(prefix string) (*Repository, bool) {
	repo, ok := r.byPrefix[prefix]
	return repo, ok
}

// List returns the registered repository IDs in ascending order.
func (r *RepositoryRegistry) List() []RepositoryID {
	out := make([]RepositoryID, 0, len(r.repos))
	for id := range r.repos {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

const repoFileExt = ".toml"
