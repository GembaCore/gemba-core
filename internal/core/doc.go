// Package core holds adaptor-agnostic domain types — the shared vocabulary
// every adapter, transport, and server handler speaks. Nothing in core may
// import internal/adapter, internal/transport, or internal/server.
//
// # Organizing hierarchy (gm-26n4)
//
// Gemba is organized into a single nested hierarchy:
//
//	Workspace (≡ Project)            — one .gemba/, one beads database
//	└── Repository[]                 — one or more git repos owned by the workspace
//	    └── WorkItem[]               — every bead is associated with exactly one repository
//
// "Workspace" and "project" are interchangeable terminology in code and docs;
// the codebase uses workspace because it predates the project concept and the
// organizing principle is the same. A WorkItem can span multiple repositories
// (gm-kdh3): WorkItem.RepositoryIDs lists every repo it touches and
// WorkItem.PrimaryRepositoryID is the spawn-cwd anchor. Existing beads filed
// before gm-26n4 use [RepositoryUnspecified] or empty fields; the spawn path
// rejects polecat work on those until the operator backfills.
//
// Contents:
//
//   - types.go          WorkItem, AgentRef, Relationship, Evidence (+ their enums)
//   - repository.go     Repository, RepositoryID, RepositoryRegistry (gm-26n4),
//                       BeadPrefix routing (gm-d2ts), auto-derive (gm-i4bd)
//   - state.go          StateCategory ∈ backlog|unstarted|started|completed|canceled
//   - derived.go        DerivedSignals + Derive(item, escalations, manifest) — pure
//   - dod.go            DefinitionOfDone (informational-only; never blocks)
//   - budget.go         Sprint, TokenBudget (three-tier inform/warn/stop)
//   - workplane.go      WorkPlane interface + CapabilityManifest (gm-e3.2)
//   - orchestration.go  OrchestrationPlaneAdaptor + OrchestrationCapabilityManifest (gm-e3.3)
//   - codegen.go        CoreTypesTS — source of truth emitted to web/src/types
//
// Regenerate the TypeScript mirror via `make gen`.
package core
