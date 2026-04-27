package codeanalysis

import corecodeanalysis "github.com/MikeBengtson/gemba/internal/core/codeanalysis"

// init swaps the registry's gitnexus placeholder for the live
// adapter (gm-56z). A blank import of this package from the
// binary's main path is enough to wire the real implementation.
//
// Replace (rather than Register) is correct because gm-1ak
// pre-installs a placeholder factory under the same name —
// Register would refuse the collision; Replace overwrites it.
//
// We deliberately ignore the Replace error: the only way it
// returns non-nil is on an empty name or nil factory, both of
// which are compile-time impossibilities here.
func init() {
	_ = corecodeanalysis.Replace(corecodeanalysis.BackendGitNexus, Factory)
}
