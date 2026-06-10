// Package native implements core.OrchestrationPlaneAdaptor by talking
// directly to shell sessions on the operator's host (tmux panes,
// iTerm2 tabs, Terminal.app tabs). MVP scope is defined by bead
// gm-native.
//
// This file is the package scaffold (gm-native.2). Every method on
// OrchestrationPlane returns core.ErrUnsupported-typed errors until
// the dependent beads (gm-native.4 tmux backend, gm-native.9
// StartSession, …) fill them in. The manifest is real today; the
// Describe() return shape is what the server will surface to the
// SPA.
package native
