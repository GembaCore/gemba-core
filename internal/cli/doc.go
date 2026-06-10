// Package cli builds the Cobra command tree for the gemba binary. The
// cmd/gemba entry point delegates to Execute so subcommand wiring stays
// out of package main and can be tested directly.
package cli
