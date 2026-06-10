// Package walk_summary implements the Documentarian persona's
// walk-end artifact skill (gm-77u). It runs after Store.End marks a
// walk terminal and produces a markdown summary committed under
// docs/walks/<YYYY-MM-DD>-<slug>.md.
//
// Unlike epic_order, walk_summary is deterministic: the v1 narrative
// is templated from the walk's structured fields (decision counts,
// agenda totals, cost). LLM-authored narrative is a follow-up; a
// deterministic v1 ships today so the artifact path exists and the
// HTTP /end handler has something to record.
//
// Skill shape:
//
//	Generate(Input) → Output       — pure; markdown body + relative path
//	Applier.Apply(ctx, Output)     — file I/O; writes under a root
//	Skill.Run(ctx, Walk, Now)      — Generate + (optional) Apply, the
//	                                 single entry point /api/v1/walks/{id}/end calls
//
// Skill is intentionally not registered with persona.SkillRegistry —
// that registry is for LLM-driven consult skills (SkillPrompt +
// OutputTool). walk_summary is invoked directly from the End
// handler; the Documentarian persona's TOML lists it for future
// cross-reference and operator-facing skill enumeration.
package walk_summary
