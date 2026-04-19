// Package gemba is the module root. It exists primarily to host the
// embed directive for the Vite SPA build output, which cannot live
// inside cmd/gemba because embed paths cannot traverse upward with "..".
//
// Consumers (cmd/gemba) call SPA() to get the embedded filesystem.
package gemba

import (
	"embed"
	"io/fs"
)

// webDist holds the built Vite SPA. See the package doc and web/dist/.keep.
//
//go:embed all:web/dist
var webDist embed.FS

// SPA returns a sub-FS rooted at web/dist, or nil if the build output is
// missing. Callers use the nil case to show a helpful "run make build"
// hint instead of serving 404s.
func SPA() fs.FS {
	sub, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
