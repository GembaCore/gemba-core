// Package core holds adaptor-agnostic domain types — the shared vocabulary
// every adapter, transport, and server handler speaks. Nothing in core may
// import internal/adapter, internal/transport, or internal/server.
//
// Contents:
//
//   - types.go          WorkItem, AgentRef, Relationship, Evidence (+ their enums)
//   - state.go          StateCategory ∈ backlog|unstarted|started|completed|canceled
//   - dod.go            DefinitionOfDone (informational-only; never blocks)
//   - budget.go         Sprint, TokenBudget (three-tier inform/warn/stop)
//   - workplane.go      WorkPlane interface + CapabilityManifest (gm-e3.2)
//   - orchestration.go  OrchestrationPlaneAdaptor + OrchestrationCapabilityManifest (gm-e3.3)
//   - codegen.go        CoreTypesTS — source of truth emitted to web/src/types
//
// Regenerate the TypeScript mirror via `make gen`.
package core
