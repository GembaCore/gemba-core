// Embedded persona seed (gm-root.24).
//
// The three TOML files under embedded_personas/ are bundled into the
// gemba binary at build time so a freshly-ratified project can land
// with a non-empty .gemba/personas/ directory without shelling out to
// discover the source on disk. These mirror the three top-of-repo
// .gemba/personas/*.toml that ship for development; keep them in sync
// when the canonical seed roster changes.

package server

import "embed"

//go:embed embedded_personas/*.toml
var embeddedPersonas embed.FS
