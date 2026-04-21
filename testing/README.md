# `github.com/MikeBengtson/gemba/testing`

Conformance harness for Gemba adaptors. Import this package from your
adaptor's own `go test` suite to validate that your `WorkPlane` or
`OrchestrationPlaneAdaptor` implementation satisfies the contracts in
`docs/adaptors/workplane.md` and `docs/adaptors/orchestration.md` before
you ship.

Resolves DD-12, DD-15. Published early per the Foolery-spike lesson
(`docs/prior-art/foolery.md`): a contract is only real when external
authors can run the contract's own tests against their code.

## Why this package exists

Foolery's `BackendPort` is the canonical shape for "extension point you
actually treat as external": the contract type lives next to the tests
that bind the contract, and third-party backends can `go get` both to
validate their implementation in their own CI. Gemba follows the same
pattern — anything less leaves adaptor authors hand-rolling tests
against `docs/adaptors/*.md`, which rots the moment the contract tightens.

## Quick start

```go
package beads_test

import (
    "testing"

    "yourorg/beads" // your adaptor
    "github.com/MikeBengtson/gemba/internal/core"
    gembatesting "github.com/MikeBengtson/gemba/testing"
)

func TestBeadsConformance(t *testing.T) {
    impl := beads.New(ctx, beads.Config{...})
    gembatesting.RunWorkPlaneConformance(t, impl, &gembatesting.WorkPlaneFixture{
        KnownMissingID: core.WorkItemID("gemba/gemba/bd-does-not-exist"),
    })
}
```

Run it:

```console
$ go test -v -run TestBeadsConformance ./...
=== RUN   TestBeadsConformance
=== RUN   TestBeadsConformance/A_describe_returns_valid_manifest
=== RUN   TestBeadsConformance/A_manifest_round_trips_json
=== RUN   TestBeadsConformance/A_describe_is_idempotent
=== RUN   TestBeadsConformance/E_capability_denial_matches_manifest
=== RUN   TestBeadsConformance/F_not_found_is_tagged_adaptor_error
--- PASS: TestBeadsConformance (0.00s)
```

## Entry points

Two parallel APIs — same probes, same group layout — cover the two
contexts a conformance run happens in:

- `RunWorkPlaneConformance(t, impl, fixture)` / `RunOrchestrationConformance`
  — drive probes from a `*testing.T` (e.g., a `TestXxxConformance`
  test in your adaptor's Go test suite).
- `RunWorkPlaneProbes(impl, fixture)` / `RunOrchestrationProbes` —
  programmatic, testing-free entry points returning a structured
  `*Report`. Used by the `gemba adaptor test` CLI (gm-e3.5) and by any
  CI system that would rather consume JSON than parse `go test` output.

### `RunWorkPlaneConformance(t, impl, fixture)`

Exercises the probes in `docs/adaptors/workplane.md`:

| Group | Probe                                       | Asserts                                                                               |
| ----- | ------------------------------------------- | ------------------------------------------------------------------------------------- |
| A     | `describe_returns_valid_manifest`           | `Describe()` returns; `CapabilityManifest.Validate()` passes; `ProtocolVersion` matches core. |
| A     | `manifest_round_trips_json`                 | Manifest re-decodes byte-identical through `encoding/json`.                            |
| A     | `describe_is_idempotent`                    | Two consecutive `Describe()` calls return equal manifests.                             |
| E     | `capability_denial_matches_manifest`        | Gated ops (`attach_evidence`, `list_sprints`, `read_budget_rollup`) opt-out via manifest → adaptor returns `capability_denied` `*core.AdaptorError`. |
| F     | `not_found_is_tagged_adaptor_error`         | `GetWorkItem(KnownMissingID)` returns a tagged error that also satisfies `errors.Is(err, core.ErrNotFound)`. Skipped when `KnownMissingID` is empty. |

### `RunOrchestrationConformance(t, impl, fixture)`

Exercises the probes in `docs/adaptors/orchestration.md`:

| Group | Probe                                                     | Asserts                                                                               |
| ----- | --------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| A     | `describe_returns_valid_manifest`                         | Manifest has non-empty `workspace_kinds`, `group_modes`, `cost_axes`, `escalation_kinds`, `peek_modes` and a valid `Transport`; `OrchestrationAPIVersion` matches core. |
| A     | `manifest_round_trips_json`                               | Manifest re-decodes byte-identical through `encoding/json`.                            |
| B     | `list_pending_requests_returns_empty_slice`               | For a freshly-started session, `ListPendingRequests` returns a non-nil empty slice.    |
| B     | `end_session_populates_close_reason`                      | First terminal close populates `Session.CloseReason` with a valid `SessionCloseReason`. |
| B     | `end_session_idempotent_under_same_nonce`                 | Second `EndSession` with the same `(sessionID, nonce)` is a no-op — no error.          |
| B     | `end_session_terminal_absorbing`                          | `EndSession` under a fresh nonce on an already-terminal session is a no-op — no error. |
| F     | `list_pending_requests_unknown_session_is_tagged`         | `ListPendingRequests` on an unknown id returns a tagged `KindSessionNotFound` error. Skipped when `KnownMissingSessionID` is empty. |

The Group B probes need a live session. Supply `OrchestrationFixture.SessionStarter`:

```go
gembatesting.RunOrchestrationConformance(t, impl, &gembatesting.OrchestrationFixture{
    KnownMissingSessionID: "sess-does-not-exist",
    SessionStarter: func(t *testing.T, adaptor core.OrchestrationPlaneAdaptor) (string, func()) {
        id, err := myAdaptor.MintFixtureSession(ctx)
        if err != nil { t.Fatal(err) }
        return id, func() { myAdaptor.DestroyFixtureSession(id) }
    },
})
```

### `RunWorkPlaneProbes(impl, fixture) *Report` / `RunOrchestrationProbes`

The programmatic runner. Same probes as above, but failures accumulate
into a `*Report` rather than failing a `*testing.T`. The CLI path:

```
gemba adaptor test --transport jsonl --target builtin:noop-work
gemba adaptor test --transport jsonl --target builtin:noop-work --json
gemba adaptor test --transport jsonl --target builtin:noop-work --junit out.xml
```

`--target builtin:noop-work` / `builtin:noop-orch` exercises the
in-process reference adaptors. Remote targets (URL/socket/cmd) require
a transport wire client (gm-e4.x) — they fail fast with a structured
not-yet-implemented until those land.

The orchestration fixture used by the CLI path supplies
`ProgrammaticSessionStarter` (a `*testing.T`-free counterpart to
`SessionStarter`); it is called once per Group B probe because those
probes individually close the session they receive.

## Fixture contract

| Field                                 | Required? | Used by                                                                |
| ------------------------------------- | --------- | ---------------------------------------------------------------------- |
| `WorkPlaneFixture.KnownMissingID`     | optional  | Group F not-found probe (`GetWorkItem`).                               |
| `OrchestrationFixture.KnownMissingSessionID` | optional  | Group F unknown-session probe (`ListPendingRequests`).           |
| `OrchestrationFixture.SessionStarter` | optional  | All Group B lifecycle probes (end session, close reason, idempotency). |

Probes with no fixture data are skipped — they will not fail your suite
silently, but the corresponding `t.Run` subtests will not execute.

## Stability

Signatures are stable across minor versions. New probe subtests MAY land
as part of minor releases; passing adaptors MUST keep passing. Breaking
changes to probe semantics land with a bump of `core.ProtocolVersion`.

## Reference implementation

`internal/adapter/noop/` ships a minimal in-memory adaptor that passes
both harnesses (see `internal/adapter/noop/conformance_test.go`). Clone
the test file as the starting point for your own adaptor suite.
