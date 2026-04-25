// Package epic_order implements the Project Manager's first Skill:
// rank a set of candidate Epics for staging into a sprint.
//
// The skill is a pure programmatic boundary — caller hands in a fully
// derived EpicOrderInput (workspace name, sprint context, candidate
// Epics with derived signals, optional guidance) and gets back a
// JSONL-shaped stream of strategy/recommendation/warning/deferred/summary
// lines. The persona dispatcher invokes Claude via the `emit_ordering`
// structured-output tool; this package owns the schemas and validators
// that gate the tool's input and output.
//
// What this package owns:
//
//   - Request types (EpicOrderInput + nested) and ValidateInput
//   - Output line types (StrategyLine, RecommendationLine, etc.) and
//     ValidateOutputLine — invoked once per emitted array element
//   - The emit_ordering ToolSpec with its JSON Schema
//   - The skill-specific prompt fragment spliced into the persona's
//     prompt envelope at LayerSkill (gm-3d6)
//
// What this package does NOT own:
//
//   - Provider invocation (anthropic.go)
//   - HTTP routing (/api/v1/consult)
//   - Persona authorization (PersonaCanInvoke)
//   - Audit-log persistence
//
// The Skill interface implementation registers itself via the Register
// helper; main wires this into the SkillRegistry at startup.
package epic_order
