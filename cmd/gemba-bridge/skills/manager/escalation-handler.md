# Escalation handler skill

A manager skill that responds to escalation requests surfaced by the
worker pool. Acknowledges, scopes, decides (or escalates further),
and emits a typed resolution back to the originating session.

## When to use

- A worker emits an EscalationRequest and the operator can't (or
  shouldn't) handle it directly.
- Multi-worker coordination: an escalation that affects more than
  one in-flight session needs a single decision routed back to all
  of them.

## What it produces

A typed `EscalationResolution` (approve / deny / defer / re-route)
plus an optional explanation that lands as a comment on the bead the
escalation originated against.

## Status

Placeholder content. Full escalation-handler logic is downstream of
`gm-518` (PM persona MVP) and `gm-e11.3` (escalation pipeline). Bundled
here so the installer's "copy when absent" path ships a concrete
file; replaced when the persona content lands.
