package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

// gm-e12.21.1 — directory-listing endpoint backing the SPA's
// <FilePicker>. The Create-project modal uses two pickers:
//   - "pick .beads/" — must navigate to a directory containing a
//     .beads/ subdir (has_beads_db=true marks those rows)
//   - "pick repo root" — any directory under the operator's home or
//     the configured projects dir
//
// The endpoint is metadata-only — no file contents are read. Path
// traversal is rejected by symlink-resolving the requested path and
// re-asserting it lives under one of the allow-list roots ($HOME and
// the configured default projects dir).

// fsListEntry is one row in the response. Mirrors the shape the
// SPA's FilePicker consumes.
type fsListEntry struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"` // "dir" | "file"
	HasBeadsDB bool   `json:"has_beads_db,omitempty"`
}

// fsInfoEnvelope is the response body of GET /v1/fs/info — the
// metadata-only sibling of /v1/fs/list. Returns just the allow-list
// roots so the picker can hydrate its "Home" / "Projects" shortcuts
// without first navigating.
type fsInfoEnvelope struct {
	Home        string `json:"home"`
	ProjectsDir string `json:"projects_dir,omitempty"`
}

// infoFs handles GET /api/v1/fs/info. Always returns 200 — even when
// the operator has no $HOME (which would surface as an empty string
// and let the picker degrade to manual text input).
func (r *Router) infoFs(w http.ResponseWriter, _ *http.Request) {
	_, home, projectsDir := allowedRoots()
	writeJSON(w, http.StatusOK, fsInfoEnvelope{
		Home:        home,
		ProjectsDir: projectsDir,
	})
}

// fsListEnvelope is the response body of GET /v1/fs/list.
type fsListEnvelope struct {
	// Path is the absolute, symlink-resolved path actually listed.
	// Echoes back so the SPA can render breadcrumbs without re-
	// computing canonical form.
	Path string `json:"path"`
	// Parent is the absolute parent of Path, or empty when Path is
	// at the top of an allow-list root.
	Parent string `json:"parent,omitempty"`
	// Home is the operator's $HOME — surfaced so the picker can
	// render a "Home" shortcut without a second round-trip.
	Home string `json:"home"`
	// ProjectsDir is the configured default projects directory —
	// surfaced for a "Projects" shortcut. Empty when unconfigured.
	ProjectsDir string `json:"projects_dir,omitempty"`
	// Entries is the sorted directory listing. Files are included
	// (rendered disabled by the picker) so the operator can see what
	// they're navigating toward; only directories are descendable.
	Entries []fsListEntry `json:"entries"`
}

// listFs handles GET /api/v1/fs/list?path=/abs/dir. The path query
// parameter is required and MUST be absolute; relative paths are a
// 400. Symlinks are resolved before allow-list enforcement so a
// crafted symlink can't escape the operator's home / projects dir.
func (r *Router) listFs(w http.ResponseWriter, req *http.Request) {
	pathRaw := req.URL.Query().Get("path")
	if pathRaw == "" {
		httperr.Write(w, http.StatusBadRequest, "fs_list_error", "missing required ?path query parameter")
		return
	}
	if !filepath.IsAbs(pathRaw) {
		httperr.Write(w, http.StatusBadRequest, "fs_list_error", "?path must be an absolute filesystem path")
		return
	}

	resolved, err := filepath.EvalSymlinks(pathRaw)
	if err != nil {
		// Distinguish not-found from permission-denied so the picker
		// can render an inline message rather than a generic 500.
		if errors.Is(err, os.ErrNotExist) {
			httperr.Write(w, http.StatusNotFound, "fs_list_error", "path does not exist: "+pathRaw)
			return
		}
		if errors.Is(err, os.ErrPermission) {
			httperr.Write(w, http.StatusForbidden, "fs_list_error", "permission denied: "+pathRaw)
			return
		}
		httperr.Write(w, http.StatusInternalServerError, "fs_list_error", "resolve path: "+err.Error())
		return
	}

	roots, home, projectsDir := allowedRoots()
	if !pathUnderRoots(resolved, roots) {
		// 403 rather than 400: the picker shows this as "we can't list
		// outside your home / projects dir." Operator can still type a
		// path; they'll learn from this signal which roots are valid.
		httperr.Write(w, http.StatusForbidden, "fs_list_error",
			"path is outside the allowed roots ($HOME and the configured projects dir)")
		return
	}

	stat, err := os.Stat(resolved)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError, "fs_list_error", "stat: "+err.Error())
		return
	}
	if !stat.IsDir() {
		httperr.Write(w, http.StatusBadRequest, "fs_list_error", "path is not a directory: "+resolved)
		return
	}

	dirEntries, err := os.ReadDir(resolved)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			httperr.Write(w, http.StatusForbidden, "fs_list_error", "permission denied reading "+resolved)
			return
		}
		httperr.Write(w, http.StatusInternalServerError, "fs_list_error", "read dir: "+err.Error())
		return
	}

	entries := make([]fsListEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		// Skip dotfiles by default — operators rarely pick a dotfile
		// as a project root, and showing every .DS_Store / .git noise
		// crowds the list. The .beads marker is detected as a sibling
		// hint on the parent dir, not as its own row.
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		kind := "file"
		hasBeads := false
		if de.IsDir() {
			kind = "dir"
			// Cheap "does this child contain .beads/" probe — one
			// extra Stat per directory entry. The picker uses the
			// hint to render a small badge so the operator can spot
			// existing beads workspaces without descending.
			beadsPath := filepath.Join(resolved, de.Name(), ".beads")
			if st, err := os.Stat(beadsPath); err == nil && st.IsDir() {
				hasBeads = true
			}
		}
		entries = append(entries, fsListEntry{
			Name:       de.Name(),
			Kind:       kind,
			HasBeadsDB: hasBeads,
		})
	}
	// Stable: directories first (so navigation lands at the top of the
	// list), then files, both alphabetical. The picker doesn't re-
	// sort; it renders in the order received.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "dir"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	parent := filepath.Dir(resolved)
	// Hide the parent slot when the operator is already at one of the
	// allow-list roots — they can't go above HOME or projects dir.
	if parent == resolved || !pathUnderRoots(parent, roots) {
		parent = ""
	}

	writeJSON(w, http.StatusOK, fsListEnvelope{
		Path:        resolved,
		Parent:      parent,
		Home:        home,
		ProjectsDir: projectsDir,
		Entries:     entries,
	})
}

// allowedRoots resolves the set of paths the picker is allowed to
// list under. Today: $HOME and the configured projects dir (if set).
// Tests can monkey-patch via the userConfigOverride hook on the
// caller (none yet — keep the surface narrow until needed).
//
// Returns:
//   - roots: list of canonical absolute paths the picker may list
//     under (each already symlink-resolved when possible)
//   - home: the operator's $HOME, surfaced verbatim to the SPA
//   - projectsDir: the configured default projects dir, surfaced
//     verbatim to the SPA (empty when unset)
func allowedRoots() (roots []string, home string, projectsDir string) {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		if resolved, err := filepath.EvalSymlinks(h); err == nil {
			roots = append(roots, resolved)
			home = resolved
		} else {
			roots = append(roots, h)
			home = h
		}
	}
	if cfg, err := config.LoadUserConfig(""); err == nil {
		if dir, err := config.ResolveDefaultDir(cfg); err == nil && dir != "" {
			if resolved, err := filepath.EvalSymlinks(dir); err == nil {
				roots = append(roots, resolved)
				projectsDir = resolved
			} else {
				roots = append(roots, dir)
				projectsDir = dir
			}
		}
	}
	return
}

// pathUnderRoots reports whether p is inside any of the allow-list
// roots (or equal to one). All paths are assumed canonical (already
// EvalSymlinks'd) so a string-prefix check is sound here.
func pathUnderRoots(p string, roots []string) bool {
	for _, root := range roots {
		if p == root {
			return true
		}
		// Append the separator so /Users/op-imposter doesn't match
		// /Users/op as a prefix. filepath.Clean ensures consistent
		// trailing-slash behavior on both sides.
		rootSep := strings.TrimRight(filepath.Clean(root), string(filepath.Separator)) + string(filepath.Separator)
		if strings.HasPrefix(p, rootSep) {
			return true
		}
	}
	return false
}
