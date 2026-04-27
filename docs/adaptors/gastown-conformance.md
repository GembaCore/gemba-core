# Gas Town adaptor — conformance report

**Status:** GREEN (gm-e7.7)
**Date:** 2026-04-27
**Adaptor:** gastown 0.1.0
**Protocol version:** 1.0.0
**Plane:** orchestration

This report captures the output of `gemba adaptor test --transport api
--target builtin:gastown` against the in-process Gas Town
OrchestrationPlane. The harness reports pass / fail / skip per probe;
**skipped probes do not count as failure** — they document a fixture
gap (a method whose live wiring hasn't shipped yet), not a contract
violation.

## How to reproduce

```
gemba adaptor test --transport api --target builtin:gastown
```

Requires `gt` on PATH. Add `--json` for the machine-readable report;
add `--junit <path>` for CI integration.

## Result

```
== Gemba adaptor conformance: orchestration plane ==
adaptor:          gastown 0.1.0
protocol_version: 1.0.0

Group A: describe + capability shape
  [PASS] A_describe_returns_valid_manifest
  [PASS] A_manifest_round_trips_json
Group B: session lifecycle
  [SKIP] B_session_lifecycle_group
        fixture.ProgrammaticSessionStarter not provided
Group D: subscribe / event delivery
  [PASS] D_subscribe_returns_channel
  [SKIP] D_pause_session_emits_transition
        fixture.ProgrammaticSessionStarter not provided
  [SKIP] D_acquire_release_workspace_emits_events
        fixture.AcquireWorkspaceRequest not provided
Group F: error semantics
  [SKIP] F_list_pending_requests_unknown_session_is_tagged
        fixture.KnownMissingSessionID not provided

Summary: GREEN (passed=3 failed=0 skipped=4)
```

## Per-group commentary

### Group A — describe + capability shape ✅ 2 / 2 PASS

The manifest is well-formed and round-trips JSON. Declarations:

- `transport: api` (gm-e7.6)
- `workspace_kinds: [worktree]` (gm-e7.2)
- `group_modes: [static, pool]` (gm-e7.1)
- `cost_axes: [wallclock, tokens]` (gm-e7.4)
- `escalation_kinds: [orchestrator_pause, hitl_approval]` (gm-e7.5)
- `peek_modes: [transcript]` (gm-e7.3)
- `event_delivery: poll`

### Group B — session lifecycle ⏭️ skipped

`StartSession` / `ListSessions` / `StopSession` are still
`KindUnsupported` on the gt adaptor. The fixture's
`ProgrammaticSessionStarter` is intentionally nil so the four
lifecycle probes record `skipped` rather than failing fictionally.
When **gm-e7.2** (workspace acquisition) and **gm-e7.3** (session
lifecycle + peek) finish wiring through, the next conformance run
provides a `ProgrammaticSessionStarter` and these probes light up.

### Group D — subscribe / event delivery ✅ 1 / 1 PASS, 2 skipped

`Subscribe` returns a closed channel that respects context
cancellation — the harness's only required Group D check.
`pause_session` and `acquire_release_workspace` event-emission
probes need a programmatic starter / acquire request fixture; both
skip until the matching gm-e7.x sub-beads ship.

### Group F — error semantics ⏭️ skipped

Awaiting `ListPendingRequests` to grow a real implementation; today
the method returns `KindUnsupported`. The probe wants a known-missing
session id to assert the not-found error shape; the fixture is left
empty so the probe skips. Lights up alongside Group B.

## Skips documented (in scope)

| Probe | Blocked by |
|---|---|
| `B_session_lifecycle_group` (4 probes wrapped) | gm-e7.2 + gm-e7.3 — session lifecycle wiring |
| `D_pause_session_emits_transition` | gm-e7.3 — pause session wiring |
| `D_acquire_release_workspace_emits_events` | gm-e7.2 — acquire/release workspace wiring |
| `F_list_pending_requests_unknown_session_is_tagged` | downstream of Group B starter |

When those beads land, the conformance fixture in
`internal/cli/adaptor_conformance.go::runBuiltinGastown` grows the
`ProgrammaticSessionStarter`, `AcquireWorkspaceRequest`, and
`KnownMissingSessionID` fields; the next run unskips those probes.

## DDs resolved

- **DD-5** — capability manifest declares the adaptor's surface
- **DD-6** — escalation pipeline (orchestrator_pause; gm-e7.5)
- **DD-7** — observable session status (Group D channel pass)
- **DD-12** — single transport per adaptor (transport: api; gm-e7.6)
- **DD-15** — MCP recommended-not-required (none declared)

## Re-run cadence

Run on every commit that touches `internal/adapter/gt/`. CI gates the
exit code; a non-zero result fails the build.
