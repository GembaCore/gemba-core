// /api/repositories handler (gm-uipx.8). Read-only listing of the
// workspace's RepositoryRegistry, sourced from
// `<BeadsDir-or-cwd>/.gemba/repositories/*.toml`. Drives the
// `/project/config` Workspace-repos section; the listing is
// advisory and never blocks render — a missing directory or a
// malformed registry returns an empty list with a non-fatal
// `notice` field so the SPA shows the helpful empty state.

package server

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/GembaCore/gemba-core/core"
)

// listRepositories handles GET /api/repositories.
//
// Discovery rule: load `<workspaceDir>/.gemba/repositories/`. The
// workspace dir is `cfg.BeadsDir` when set, otherwise the gemba
// process cwd — same source-of-truth as the rest of the server.
func (r *Router) listRepositories(w http.ResponseWriter, _ *http.Request) {
	dir := r.repositoriesDir()
	// Surface a "no .gemba/repositories/ here" hint as a notice so
	// the SPA's empty state can point operators at the config file.
	// LoadRepositoryRegistry treats missing dir as empty (no error),
	// so we test for it ourselves.
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		writeJSON(w, http.StatusOK, map[string]any{
			"repositories": []core.Repository{},
			"notice":       "no .gemba/repositories/ directory in workspace; configure repositories there",
		})
		return
	}
	reg, err := core.LoadRepositoryRegistry(dir)
	if err != nil {
		// Graceful: still return 200 + empty list with a notice the
		// SPA can surface. The Workspace-repos section is advisory,
		// not load-bearing for any other route.
		writeJSON(w, http.StatusOK, map[string]any{
			"repositories": []core.Repository{},
			"notice":       err.Error(),
		})
		return
	}
	out := make([]core.Repository, 0, len(reg.List()))
	for _, id := range reg.List() {
		repo, ok := reg.Get(id)
		if !ok || repo == nil {
			continue
		}
		out = append(out, *repo)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repositories": out,
	})
}

func (r *Router) repositoriesDir() string {
	base := r.cfg.BeadsDir
	if base == "" {
		base = "."
	}
	return filepath.Join(base, ".gemba", "repositories")
}
