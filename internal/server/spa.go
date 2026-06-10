// SPA serving lives here to keep router.go focused on route wiring. The
// actual //go:embed directive lives at the module root (embed.go) because
// Go's embed patterns cannot traverse upward with ".."; the SPA fs.FS is
// passed in through NewRouter.
//
// gm-e1.3: single-binary Go service — the built Vite SPA ships inside the
// gemba binary. Dev builds run without web/dist populated and serve a
// helpful 503 hint; production builds serve SPA routes and preserve /api
// 404s via the NotFound subrouter (gm-b2 / gm-xke).

package server

import (
	"net/http"
	"strings"
)

// serveSPA is the catch-all NotFound handler. The /api and /events
// subrouters register their own NotFound, so this only runs for paths
// outside those prefixes; the prefix check below is defense-in-depth
// against future routing changes.
//
// gm-root.17.4: when ColdStartRedirect is set and the client is
// requesting the SPA root ("/"), redirect to /new so a fresh install
// with no projects lands on the conversational project-creation flow
// instead of a broken empty dashboard.
func (r *Router) serveSPA(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/")

	if strings.HasPrefix(path, "api/") || path == "events" || strings.HasPrefix(path, "events/") {
		apiNotFound(w, req)
		return
	}

	// Cold-start redirect: only applies to the SPA root so that direct
	// navigation to /new (or any other route) passes through untouched.
	if r.cfg.ColdStartRedirect && (req.URL.Path == "/" || req.URL.Path == "") {
		http.Redirect(w, req, "/new", http.StatusFound)
		return
	}

	if r.spa == nil {
		spaUnbuilt(w)
		return
	}

	// Try the requested file first (assets, favicon, etc.)
	if path != "" {
		if f, err := r.spa.Open(path); err == nil {
			_ = f.Close()
			http.ServeFileFS(w, req, r.spa, path)
			return
		}
	}

	// Fall through to index.html for client-side routing.
	f, err := r.spa.Open("index.html")
	if err != nil {
		spaUnbuilt(w)
		return
	}
	_ = f.Close()
	http.ServeFileFS(w, req, r.spa, "index.html")
}

func spaUnbuilt(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`<!doctype html>
<html><body style="font-family:system-ui;max-width:40em;margin:3em auto;padding:1em">
<h1>Gemba frontend not built</h1>
<p>The <code>web/dist</code> directory is empty. Run:</p>
<pre>make build</pre>
<p>from the repo root, or run in dev mode:</p>
<pre>make dev</pre>
<p>The dev server (Vite on :5173) proxies API calls back to this binary.</p>
</body></html>`))
}
