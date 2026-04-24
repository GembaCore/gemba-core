// Package skills bundles the default coaching + manager skill files
// the gemba-bridge installer copies into a fresh worktree
// (gm-native.18). embed.FS keeps the binary self-contained — no
// runtime dependency on a skills directory at gemba's install path.
//
// The actual content of these skill markdown files is downstream of
// the persona work in gm-518 (PM persona MVP) + gm-858 (PM use-case
// catalog) + gm-4p6 (PM persona system prompt). Until those land, the
// bundled files are minimal placeholders that document the slot —
// enough that the install round-trip works end-to-end and operators
// see "we know about this directory, we're not going to clobber yours."

package skills

import (
	"embed"
	"io/fs"

	"github.com/MikeBengtson/gemba/internal/adapter/native/install"
)

//go:embed coaching/* manager/*
var raw embed.FS

// FS returns the bundled skills tree rooted so the installer can walk
// it as `coaching/<file>` / `manager/<file>` (matching the layout the
// installer copies into .claude/skills/).
func FS() fs.FS {
	sub, err := fs.Sub(raw, ".")
	if err != nil {
		panic("skills: fs.Sub: " + err.Error())
	}
	return sub
}

func init() {
	install.SetSkillsFS(FS())
}
