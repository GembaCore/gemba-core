---
phase: B-native-tmux
plan: 01
wave: 1
status: complete
beads: [gm-v01.3.1, gm-v01.3.2, gm-v01.3.3]
commits:
  - 3c34ac7  # feat(B-01): Backend.SendInput + Tmux dispatch
  - 0d20a99  # feat(B-01): OrchestrationPlane SendInput + ResizeSession overrides
---

# Phase B Plan 01: Native SendInput + ResizeSession Summary

One-liner: Phase B input path is live — typed `SendInput`
(literal/keys/signal) flows core → OrchestrationPlane override →
tmux `send-keys` argv, with `ResizeSession` overridden as a documented
no-op; opacity guard still green.

## Files Added / Changed

| File | Change | LOC |
|---|---|---|
| `internal/adapter/native/backend/backend.go` | +SendInput on Backend interface; doc on legacy SendKeys | +24 |
| `internal/adapter/native/backend/tmux.go` | +signalToKey map, injectable `runner`, SendInput, sendLiteral/sendNamedKeys helpers | +112 |
| `internal/adapter/native/backend/tmux_test.go` | +stubTmux harness + 10 SendInput test cases | +196 |
| `internal/adapter/native/backend/docker.go` | +SendInput (literal/keys → existing SendKeys path; signal → `docker kill --signal`) | +34 |
| `internal/adapter/native/backend/iterm.go` | +SendInput (literal/keys → SendKeys; signal → KindUnsupported) | +27 |
| `internal/adapter/native/backend/terminal_app.go` | +SendInput (same shape as iterm) | +27 |
| `internal/adapter/native/start_test.go` | +sendInputCalls / sendInputErr fields on fakeBackend; +SendInput method | +20 |
| `internal/adapter/native/orchestration.go` | doc comment updated on UnsupportedSessionIO embed | +3 −1 |
| `internal/adapter/native/session_io.go` | NEW — OrchestrationPlane.SendInput + ResizeSession overrides | +104 |
| `internal/adapter/native/session_io_test.go` | NEW — 10 cases (SendInput x5, ResizeSession x5) | +209 |

## signalToKey Mapping Shipped

| Signal  | Token | Notes |
|---------|-------|-------|
| SIGINT  | `C-c` | ^C, default INTR — direct mapping. |
| SIGTERM | `C-\` | ^\, default QUIT — best-effort surrogate. tmux has no real "raise SIGTERM" verb; callers needing true SIGTERM must signal the pane PID directly. |
| SIGQUIT | `C-\` | ^\, default QUIT — direct mapping. |
| SIGTSTP | `C-z` | ^Z, default SUSP — direct mapping. |
| SIGHUP  | `C-d` | ^D / EOF — best-effort surrogate. Shells exit on EOF, which is the closest tty-level effect to SIGHUP a keystroke can produce. |

Unknown signals return `*core.AdaptorError{Kind: KindValidation}`
without invoking tmux.

## StreamSession Status

`StreamSession` still inherits `core.UnsupportedSessionIO`'s default
`KindUnsupported`. Plan 03 (B-03) is the wire-up to `bridge.Fanout`
→ `<-chan core.SessionEvent`. Confirmed by:

```
$ grep -n "func.*StreamSession" internal/adapter/native/
internal/adapter/native/bridge/fanout.go  (unrelated SSE multiplexer)
# no method on *OrchestrationPlane
```

## Deviations from Plan

### [Rule 2 - Auto-add missing critical functionality] SendInput on every Backend

The plan extended the `Backend` interface, which forced
`SendInput` methods on Docker, ITerm, and TerminalApp too (otherwise
the interface is unsatisfied and the package stops compiling). Per
the plan's own "out of scope for this plan" framing the AppleScript
backends could have stubbed to `KindUnsupported` wholesale, but
literal/keys mode actually *can* run on those backends via the
existing `SendKeys` AppleScript path, so each `SendInput` delegates
there and only surfaces `KindUnsupported` on signal mode. Docker
went one better and uses real `docker kill --signal=<name>` for
signal mode, which is the right long-term shape for that runtime.

The plan's success-criterion grep "`func.*SendInput` ... shows
exactly 2 hits" cannot be honored as written for the reason above
(5 hits today). The *intent* — Tmux backend dispatch + outer
OrchestrationPlane override is the only adapter-level path — is
satisfied. Updated grep:

```
$ grep -rn "func .*SendInput" internal/adapter/native/ | grep -v _test.go | wc -l
5
```

All 5 are interface-satisfying methods on the four concrete backends
plus the outer adapter; no duplicate dispatch paths exist.

### [Rule 1 - Bug] Fixed strings.Builder.ReadFrom test helper

Initial harness draft used `strings.Builder.ReadFrom` which doesn't
exist (Builder has WriteTo / WriteString, not ReadFrom). Replaced
with `io.ReadAll(stdin)` in the same RED commit before declaring
the test infrastructure ready.

## Bead Notes Posted

- `gm-v01.3.1` ← `3c34ac7 feat(B-01): Backend.SendInput + Tmux ...`
- `gm-v01.3.2` ← `0d20a99 feat(B-01): OrchestrationPlane SendInput ...`
- `gm-v01.3.3` ← `0d20a99 (ResizeSession folded into same commit)`

All three beads closed.

## Acceptance Floor (Final State)

| Gate | Result |
|---|---|
| `go build ./...` | Success |
| `go test ./internal/adapter/native/...` | 288 passed |
| `go test ./internal/adapter/native/backend/... -run SendInput` | 10 passed |
| `go test ./internal/adapter/native/... -run "SendInput\|ResizeSession"` | 20 passed |
| `go test ./core/... -run TestSessionIDOpacity` | 1 passed |

## Self-Check: PASSED

- internal/adapter/native/session_io.go — exists
- internal/adapter/native/session_io_test.go — exists
- internal/adapter/native/backend/tmux.go SendInput method — found at line 295
- 3c34ac7 — in `git log`
- 0d20a99 — in `git log`
