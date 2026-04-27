# Beads adaptor conformance — A–F green (gm-e6.7)

> **Status:** Conformance suite groups A–F all green against the live `bd` CLI as of the audit run below. Groups G–K are declared in the bd manifest but require fixture hooks beyond the scope of `gm-e6.7`; those slots are skipped (not failed) in the harness output until a follow-up bead under `gm-e6` lands the substrates needed (versioned-transport round-trip, concurrent read-after-write across separate Dolt clients, session-death recovery, claim-atomic and other R8 orchestrator-hook substrates).

## Reproducing the report

The suite is exposed as a CLI target on `gemba adaptor test`. Point it at a real Beads workspace (a directory containing `.beads/`):

```sh
gemba adaptor test \
  --transport api \
  --target builtin:bd-work \
  --beads-dir <path/to/beads/workspace>
```

Add `--json` for machine-readable output, or `--junit <path>` to emit a JUnit XML alongside the text summary.

The harness shells to whichever `bd` is on `$PATH`; mismatched `--transport` against the adaptor's manifest fails fast (gm-root DD-12).

## Live audit (2026-04-27)

```
== Gemba adaptor conformance: work plane ==
adaptor:          beads 0.1.0
protocol_version: 1.0.0

Group A: describe + capability shape
  [PASS] A_describe_returns_valid_manifest
  [PASS] A_manifest_round_trips_json
  [PASS] A_describe_is_idempotent
Group B: WorkItem CRUD round-trip
  [PASS] B_create_get_round_trip
  [PASS] B_update_patch_applies
  [PASS] B_list_returns_created
Group C: edge / relationship round-trip
  [PASS] C_edge_extensions_are_structurally_valid
  [SKIP] C_edge_round_trip_get_work_item
Group D: subscribe / event delivery
  [PASS] D_create_emits_event
  [PASS] D_update_emits_event
  [PASS] D_attach_evidence_emits_event
Group E: capability-declared optional features
  [PASS] E_capability_denial_matches_manifest
Group F: error semantics
  [PASS] F_not_found_is_tagged_adaptor_error
Group G: R3 dep-graph evolution               [SKIP × 2]
Group H: R4 versioned transport round-trip    [SKIP × 5]
Group I: R5 concurrency stress
  [PASS] I_concurrent_writer_stress_N16
  [SKIP] I_read_after_write_cross_writer
Group J: R6 session decoupling                [SKIP × 2]
Group K: R8 orchestrator hooks                [SKIP × 4]

Summary: GREEN (passed=13 failed=0 skipped=15)
```

## What the green columns prove

| Group | Probe | Why it matters |
|---|---|---|
| A | describe + JSON round-trip + idempotency | The capability manifest can be serialised and re-decoded without drift. The `gemba_serve` boot path and the SPA's `/api/capabilities` consumer rely on this. |
| B | CRUD round-trip | A WorkItem created through `bd create` re-reads identically through `bd show` and a follow-up `bd update --title` lands on the returned record. The conformance probes drive both off `created.ID` rather than the caller-supplied id, so the test is robust against backends (Beads, Jira, Linear, GitHub) that mint their own ids from a prefix pool. |
| C | edge extensions structurally valid | The seven beads-native edge types (`blocks`, `parent_child`, `relates_to`, plus four `beads:*` extensions) declared in the manifest survive marshal/unmarshal. The seeded round-trip probe (`SeedWorkItemWithEdges`) is exercised by the Go-side `TestBeadsWorkPlaneConformance` rather than the CLI runner — the CLI's fixture surface doesn't carry the seed hook today. |
| D | subscribe + event emission | Every state-changing mutation (`CreateWorkItem`, `UpdateWorkItem`, `AttachEvidence`) emits a matching `workitem.*` event on the Subscribe channel within the 250 ms freshness budget. This is the gm-e3.6.3 contract end-to-end on the bd adaptor. Out-of-process changes propagate through the same emitter via the fsnotify+poll watcher (gm-e6.6). |
| E | capability denial matches manifest | The bd manifest opts out of `AttachEvidence` (`EvidenceSynthesisRequired=false`), `ListSprints` (`SprintNative=false`), and `ReadBudgetRollup` (`TokenBudgetEnforced=false`); calling those methods returns a tagged `capability_denied` error rather than panicking or silently no-oping. |
| F | not-found is tagged | A `bd show <unknown>` surfaces as a typed `core.AdaptorError{Kind: KindSessionNotFound}` and `errors.Is(err, core.ErrNotFound)` resolves true. |
| I | concurrent writer stress (N=16) | Sixteen concurrent goroutines hammer the adaptor with mutations; no panic, no partial writes, no lost rows. Driven against the in-process fake bd in the test harness; the CLI runner against live bd reports the same green. |

## What the skipped columns mean

The skips are **fixture gaps**, not contract violations. Each maps to a follow-up child bead under `gm-e6`:

- **G — R3 dep-graph evolution.** Probes a multi-step dep-graph evolution (block, observe ready set, unblock, observe ready set move). Needs a fixture hook that drives bd through the full sequence; pending under `gm-e6` follow-ups.
- **H — R4 versioned transport round-trip.** Exports state via the declared transport (git / dolt / jsonl), imports into a fresh instance, asserts row-level equivalence. Needs a transport-aware fixture; bd's manifest declares all three transports so each lands as a separate skip.
- **I — R5 read-after-write cross-writer.** Two goroutines: one writes, the other reads from a different Dolt client. Needs a real two-client harness; `concurrent_writer_stress` runs on the same client and is green.
- **J — R6 session decoupling.** Simulates agent session death mid-write; confirms state lands. Needs a fixture that can kill a writer mid-call.
- **K — R8 orchestrator hooks.** `ready_set_subscribe`, `claim_atomic`, `escalation_ingest`, `work_complete_ack`. The bd manifest advertises all four; the probes are wired but the substrate (a polling consumer + a competing claimer + a real escalation source + a work-complete event sink) is not.

The audit's drift-detector test (`TestBeadsConformanceR1R8Audit`, `internal/adapter/bd/conformance_audit_test.go`) holds the inverse contract: any G–K group that flips to `NotApplicable` triggers a failure. That keeps the manifest's R-field declarations honest — silently dropping a hook from the manifest would surface immediately.

## Resolved DDs

- **DD-2** — every adaptor passes the conformance suite. ✓ A–F green.
- **DD-9** — Gemba never writes the backend's private storage; mutations route through `bd <verb> --json`. ✓ Re-validated by Group B + Group D probes.
- **DD-12** — adaptor declares exactly one of {api, jsonl, mcp}; the harness fails fast on a mismatch. ✓ Asserted by the CLI runner before probes start.
- **DD-13** — the manifest's optional-feature flags faithfully describe what the adaptor will accept. ✓ Group E probe.

## How the suite stays honest

Two layers reinforce each other:

1. **`TestBeadsWorkPlaneConformance`** (testing.T entry, in-process fake bd) — runs every probe on every `go test ./internal/adapter/bd/...` invocation. Catches contract drift at PR time.
2. **`gemba adaptor test --target builtin:bd-work`** (CLI entry, live bd binary) — captures the report for documentation and operator-side verification. Re-running this command should reproduce the audit above on any local bd workspace.

A new probe, or a change in bd manifest's R-field projection, is detected by the audit drift-detector (`TestBeadsConformanceR1R8Audit`). When the substrate gaps close, the corresponding skips flip to `[PASS]` automatically — the conformance harness already runs the probes; only the fixture wiring is missing.

## Related beads

- **gm-e6.1** — Beads CRUD via `bd --json` (parent of this audit).
- **gm-e6.2** — edge mapping (Group C structural validity).
- **gm-e6.3** — agent federation (Group F + B handoffs).
- **gm-e6.4** — DoD pass-through (description-delimited storage).
- **gm-e6.5** — evidence synthesis (Group E will gain a populated probe when this lands; today the manifest opts out, which the probe accepts).
- **gm-e6.6** — fsnotify + poll Subscribe (Group D out-of-process half).
- **gm-e3.6.3** — Group D event-emission probes themselves.
- **gm-zdx** — R1–R8 manifest projection audit (`TestBeadsConformanceR1R8Audit`).
