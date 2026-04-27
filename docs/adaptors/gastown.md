# Gas Town adaptor

Status: shipping (gm-e7.x epic)

## Transport

The Gas Town adaptor declares `transport: "api"` (HTTP+JSON
request/response per gm-root DD-12). v1 the API channel is the
local `gt` CLI invoked with `--json`:

```
gt rig list --json     →  []rig{...}
gt polecat list --json →  []polecat{...}
gt mail inbox --json   →  []mail{...}
gt session ...
```

Every adaptor method is a one-shot JSON request/response cycle.
Functionally equivalent to a remote HTTP+JSON service — the
adaptor body just happens to reach the local `gt` process via
exec. A future remote shim that POSTs the same payloads to a
Gas Town daemon (`gemba adaptor register --transport api --target
https://gastown.example.com`) can replace the exec runner with a
fetcher and the adaptor body stays unchanged.

No MCP bridge required. MCP is recommended-not-required per
DD-15; the adaptor has no MCP surface today.

## Manifest

```toml
adaptor_id              = "gastown"
adaptor_version         = "0.1.0"
transport               = "api"
workspace_kinds         = ["worktree"]
default_workspace_kind  = "worktree"
group_modes             = ["static", "pool"]
cost_axes               = ["wallclock"]   # gm-e7.4 expands this
escalation_kinds        = ["hitl_approval"] # gm-e7.5 expands this
peek_modes              = ["transcript"]
event_delivery          = "poll"
```

The cost axes and escalation kinds are intentionally small in v1
— gm-e7.4 (cost meter synthesis) and gm-e7.5 (escalation mapping)
expand them as those beads land.

## Operator commands

```
# Register the adaptor
gemba adaptor register --transport api --target gastown

# Inspect manifest
gemba adaptor describe gastown

# Watch live polecat status
gt polecat list --all --json | jq .
```

## Conformance

Group A (Describe + Capability) passes today. Groups B–F land as
the rest of the gm-e7.x sub-beads ship; gm-e7.7 is the conformance
gate.

## See also

- gm-e7.1 — list_agents / list_groups
- gm-e7.2 — start_session / stop_session / claim_next_ready
- gm-e7.3 — session lifecycle + tmux capture-pane peek
- gm-e7.4 — cost meter synthesis
- gm-e7.5 — escalation mapping (`gt escalate` → EscalationRequest)
- gm-e7.6 — this file: transport: api declaration
- gm-e7.7 — conformance suite
