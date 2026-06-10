# Adaptors

Gemba pairs exactly one **WorkPlane adaptor** (the data plane — work
tracker) with at most one **OrchestrationPlane adaptor** (the agent
runtime). Each adaptor declares a capability manifest at boot;
the SPA renders only what the manifest exposes.

## Authoring guides

- **[WorkPlane authoring guide](workplane)** — the core surface every
  WorkPlane adaptor must implement, plus the conformance harness an
  adaptor passes to be considered ready.
- **[OrchestrationPlane authoring guide](orchestration)** — the
  scope-bounded session lifecycle and the optional capability axes
  every OrchestrationPlane adaptor declares.

## Reference adaptors

- **[Beads](beads)** — the out-of-the-box WorkPlane adaptor (`bd` CLI
  + direct Dolt SQL modes). [Conformance report](beads-conformance).
- **[Native](native)** — the bundled OrchestrationPlane that drives
  tmux / iTerm2 / Terminal.app sessions directly without an external
  daemon.
- **[Gas Town](gastown)** — optional OrchestrationPlane wrapping the
  Gas Town agent runtime. [Conformance report](gastown-conformance).

## Where next

- Architectural decisions about plane boundaries: see [Design](../design/).
- The capability manifest contract is in the SPA spec: see
  [UI spec](../ui-spec).
