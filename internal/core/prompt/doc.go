// Package prompt holds PromptEnvelope — the cross-cutting composition
// layer that produces every persona/skill invocation's final prompt from
// separately-owned pieces. See envelope.go for the layer ordering,
// budgeting model, and Compose contract.
//
// The package is pure: no I/O, no randomness, no network. Given the same
// Envelope, ComposeOptions, and TokenCounter, Compose produces the same
// Composed value byte-for-byte. Callers feed PromptEnvelope the pieces
// (project values, workspace values, persona system prompt, skill shape,
// context chunks, user input); PromptEnvelope returns the assembled text.
//
// PromptEnvelope is the integration point every Skill invocation flows
// through (gm-3d6) so that values, guardrails, goals, and context
// providers are applied consistently rather than re-implemented per
// persona.
package prompt
