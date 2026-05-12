package audit

// Event is a string alias for the audit "event name" — a finer-grained
// label than Kind that handlers stamp onto records so log readers can
// filter by surface (egress, vault, …) without parsing the payload.
//
// Records that carry an Event embed it inside their JSON payload under
// "event". Kind remains the coarse category required by Record.
type Event string

// Egress rule audit events (gm-o9t8.3.6.1). Emitted by the
// /api/v1/workspaces/{wsid}/egress-rules POST/DELETE handlers so
// operators can reconstruct who added or removed a network-policy
// rule. Runtime enforcement traffic events (allow/deny decisions)
// land in gm-o9t8.3.6.2 — those are intentionally NOT modeled here.
const (
	EventEgressRuleCreate Event = "egress.rule.create"
	EventEgressRuleDelete Event = "egress.rule.delete"
)

// VM lifecycle audit events (gm-o9t8.3.2.7). Emitted by the firecracker
// supervisor on successful Start (EventVMSpawn) and on Stop completion
// (EventVMDestroy). Payload carries vm_id + wsid + the boot parameters
// on Spawn; vm_id + duration_ms + exit_status on Destroy.
//
// EventVMEgressDenied is the runtime egress-deny event that
// gm-o9t8.3.6.2 will emit when it lands; the constant is declared
// here so the supervisor (this bead) and the runtime enforcer (the
// next bead) share the same identifier without one importing the
// other.
const (
	EventVMSpawn        Event = "vm.spawn"
	EventVMDestroy      Event = "vm.destroy"
	EventVMEgressDenied Event = "vm.egress.denied"
)

// KindVMLifecycle is the coarse Kind used by VM lifecycle records.
// Declared in audit (the package that owns Kind) rather than the
// supervisor so log readers can filter by kind without a string
// literal duplicated across surfaces.
const KindVMLifecycle Kind = "vm_lifecycle"
