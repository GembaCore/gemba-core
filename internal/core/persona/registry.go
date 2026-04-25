package persona

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

// fileExt is the persona file extension. Files in a persona directory
// that don't match are ignored, so a stray README.md or .gitignore
// doesn't break LoadDir.
const fileExt = ".toml"

// LoadFile reads a single persona TOML file and returns the parsed
// [Persona]. The file's basename (minus .toml) MUST match the file's
// declared `id` field — this keeps the on-disk filesystem the source
// of truth for persona identity.
func LoadFile(path string) (*Persona, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("persona: read %s: %w", path, err)
	}
	return decodeFile(path, b)
}

// LoadDir reads every *.toml file under dir and returns the parsed
// personas keyed by ID. Subdirectories are not descended; persona
// files live flat under .gemba/personas/.
//
// A missing dir is not an error: an empty map is returned. This lets
// the server start cleanly in workspaces that haven't authored any
// personas yet. Other I/O errors (permission denied, malformed file)
// are propagated.
func LoadDir(dir string) (map[string]*Persona, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]*Persona{}, nil
		}
		return nil, fmt.Errorf("persona: scan %s: %w", dir, err)
	}

	out := make(map[string]*Persona, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, fileExt) {
			continue
		}
		path := filepath.Join(dir, name)
		p, err := LoadFile(path)
		if err != nil {
			return nil, err
		}
		if existing, ok := out[p.ID]; ok {
			return nil, fmt.Errorf("persona: duplicate id %q (files %s and %s)",
				p.ID, existing.sourcePath, path)
		}
		p.sourcePath = path
		out[p.ID] = p
	}
	return out, nil
}

// decodeFile parses raw TOML bytes into a Persona, applies defaults,
// validates, and enforces the filename↔id invariant. Split out so
// LoadFile and tests can share the path; tests synthesize files in
// memory via the test helper, while LoadFile goes through disk.
func decodeFile(path string, raw []byte) (*Persona, error) {
	var p Persona
	if _, err := toml.Decode(string(raw), &p); err != nil {
		return nil, fmt.Errorf("persona: decode %s: %w", path, err)
	}
	p.normalize()
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("persona: %s: %w", path, err)
	}
	stem := strings.TrimSuffix(filepath.Base(path), fileExt)
	if p.ID != stem {
		return nil, fmt.Errorf(
			"persona: %s: id %q does not match filename stem %q",
			path, p.ID, stem)
	}
	p.sourcePath = path
	return &p, nil
}

// sourcePath is the filesystem path the persona was loaded from.
// Unexported and not part of the wire format; the registry surfaces
// it via [Registry.SourcePath] for diagnostics ("which file declared
// this?").
//
// Stored as a method on a side struct rather than a field on Persona
// to keep the JSON-encoded shape clean.
func (p *Persona) source() string { return p.sourcePath }

// Registry is the in-memory lookup table the dispatcher consults at
// request time. It is built once at startup (or on workspace switch)
// and treated as read-only thereafter.
//
// Registry methods are safe for concurrent reads. Registration is
// not concurrent-safe; do all registrations on a single goroutine
// before serving requests.
type Registry struct {
	personas map[string]*Persona
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{personas: make(map[string]*Persona)}
}

// LoadDir constructs a Registry from every persona TOML file under
// dir. See package-level LoadDir for I/O semantics; this method
// merely wraps the result in a Registry for convenience.
func LoadRegistry(dir string) (*Registry, error) {
	personas, err := LoadDir(dir)
	if err != nil {
		return nil, err
	}
	return &Registry{personas: personas}, nil
}

// Register adds p to the registry. Returns an error when p is nil,
// p.ID is empty, or another persona is already registered under
// p.ID. Re-registering the same pointer is a no-op so init paths
// can be defensive without erroring out.
func (r *Registry) Register(p *Persona) error {
	if p == nil {
		return errors.New("persona: Register: persona is nil")
	}
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("persona: Register: id is empty")
	}
	if existing, ok := r.personas[p.ID]; ok {
		if existing == p {
			return nil
		}
		return fmt.Errorf("persona: Register: id %q already registered", p.ID)
	}
	r.personas[p.ID] = p
	return nil
}

// Get returns the persona registered under id. The bool is false
// when no persona is registered under that id.
func (r *Registry) Get(id string) (*Persona, bool) {
	p, ok := r.personas[id]
	return p, ok
}

// List returns the registered persona IDs in ascending order. The
// `/insights/personas` view consumes this to render its menu.
func (r *Registry) List() []string {
	out := make([]string, 0, len(r.personas))
	for id := range r.personas {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// SourcePath returns the on-disk file the persona was loaded from,
// or "" when the persona was registered programmatically (tests).
func (r *Registry) SourcePath(id string) string {
	p, ok := r.personas[id]
	if !ok {
		return ""
	}
	return p.source()
}
