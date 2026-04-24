package bridge

import (
	"encoding/json"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Translator converts a raw bridge Frame into zero-or-more
// core.OrchestrationEvents. The zero return is intentional — not
// every hook fire is a Gemba-visible event (e.g. an unmatched
// UserPromptSubmit is just context for correlation, not its own
// event). Per-agent-type translators live in this package.
type Translator func(f Frame) []core.OrchestrationEvent

// ForAgent selects a translator by agent type. Unknown types get
// the passthrough translator, which still emits a generic
// session_transition event on SessionStart/Stop so Gemba doesn't go
// dark just because an agent type hasn't been explicitly wired in.
func ForAgent(agentType string) Translator {
	switch agentType {
	case "claude":
		return translateClaude
	case "shell-only":
		return translateShell
	default:
		return translatePassthrough
	}
}

// parseTS parses the bridge's ISO timestamp, falling back to now()
// if the frame is missing or malformed (rare — bridge stamps
// every frame).
func parseTS(f Frame) time.Time {
	if f.TS == "" {
		return time.Now()
	}
	t, err := time.Parse(time.RFC3339Nano, f.TS)
	if err != nil {
		return time.Now()
	}
	return t
}

// translateClaude maps Claude Code's hook names into
// OrchestrationEvent kinds. Kinds are stringly-typed because the
// OrchestrationEvent.Kind field is a string on the wire; we keep
// the set small and documented here so the SPA translator stays in
// sync.
func translateClaude(f Frame) []core.OrchestrationEvent {
	base := core.OrchestrationEvent{
		ID:        f.EventID,
		At:        parseTS(f),
		SessionID: f.SessionID,
		Payload:   rawPayload(f),
	}
	switch f.Hook {
	case "GembaState":
		// Deterministic state-transition signal written by the
		// gemba-state CLI (gm-cdph). Payload carries {state, bead_id,
		// note}. The native OrchestrationPlane subscribes to this
		// event kind to mutate Session.Status.
		base.Kind = "session_state_reported"
		return []core.OrchestrationEvent{base}
	case "GembaAsk":
		// Coach/Manager skill called the gemba-ask CLI (gm-97w7.1).
		// Payload carries {kind, role, text, mode, bead_id?, title?}.
		// Translate into an escalation_opened event with the kind,
		// channel, urgency, and body fields already stamped so the
		// escalation index can insert without re-classifying.
		return translateGembaAsk(f, base)

	case "SessionStart":
		base.Kind = "session_transition"
		base.Payload["status"] = "running"
		return []core.OrchestrationEvent{base}
	case "Stop", "SubagentStop":
		base.Kind = "session_transition"
		// Default to provider_exit; the adaptor's EndSession path
		// refines this when the operator asked to stop.
		base.Payload["status"] = "completed"
		base.Payload["close_reason"] = "provider_exit"
		return []core.OrchestrationEvent{base}
	case "Notification":
		base.Kind = "escalation_opened"
		base.Payload["escalation_kind"] = classifyNotification(f)
		return []core.OrchestrationEvent{base}
	case "UserPromptSubmit":
		// Correlation with open escalations happens in the adaptor's
		// in-memory index; the translator emits a generic
		// "user_message" event so the correlator can react without
		// re-parsing Claude hook shape.
		base.Kind = "user_message"
		return []core.OrchestrationEvent{base}
	case "PreToolUse":
		base.Kind = "tool_use"
		base.Payload["phase"] = "pre"
		return []core.OrchestrationEvent{base}
	case "PostToolUse":
		base.Kind = "tool_use"
		base.Payload["phase"] = "post"
		return []core.OrchestrationEvent{base}
	default:
		// Unknown hook — emit a generic event so downstream logs
		// show it without dropping.
		base.Kind = "unknown_hook"
		base.Payload["hook"] = f.Hook
		return []core.OrchestrationEvent{base}
	}
}

// translateShell handles the `prompt_command` hook profile. One
// frame per shell prompt → one user_message event so the adaptor's
// bd-correlation path (gm-native.14) can inspect the cmd field.
func translateShell(f Frame) []core.OrchestrationEvent {
	base := core.OrchestrationEvent{
		ID:        f.EventID,
		At:        parseTS(f),
		SessionID: f.SessionID,
		Kind:      "user_message",
		Payload:   rawPayload(f),
	}
	base.Payload["agent_type"] = "shell-only"
	return []core.OrchestrationEvent{base}
}

// translatePassthrough is the fallback for unknown agent types. Only
// SessionStart/Stop + GembaState produce events; everything else is
// dropped (logged by caller).
func translatePassthrough(f Frame) []core.OrchestrationEvent {
	base := core.OrchestrationEvent{
		ID:        f.EventID,
		At:        parseTS(f),
		SessionID: f.SessionID,
		Payload:   rawPayload(f),
	}
	switch f.Hook {
	case "GembaState":
		// gemba-state CLI is agent-type agnostic — fire for shell-only
		// and any future agent that hasn't been explicitly wired.
		base.Kind = "session_state_reported"
		return []core.OrchestrationEvent{base}
	case "GembaAsk":
		// gemba-ask is likewise agent-type agnostic.
		return translateGembaAsk(f, base)
	case "SessionStart":
		base.Kind = "session_transition"
		base.Payload["status"] = "running"
		return []core.OrchestrationEvent{base}
	case "Stop":
		base.Kind = "session_transition"
		base.Payload["status"] = "completed"
		base.Payload["close_reason"] = "provider_exit"
		return []core.OrchestrationEvent{base}
	}
	return nil
}

// translateGembaAsk converts a GembaAsk frame (written by the
// gemba-ask CLI) into a fully-stamped escalation_opened event. The
// CLI is authoritative about kind + role + text + mode; the
// translator calls Classify to turn (kind, mode) into urgency, and
// drops the event when the mode forbids surfacing (dangerous mode —
// the CLI rejects this too, but a second check here keeps the
// pipeline safe against stale / hand-crafted frames).
func translateGembaAsk(f Frame, base core.OrchestrationEvent) []core.OrchestrationEvent {
	var p struct {
		Kind   string `json:"kind"`
		Role   string `json:"role"`
		Text   string `json:"text"`
		Mode   string `json:"mode"`
		BeadID string `json:"bead_id"`
		Title  string `json:"title"`
	}
	if len(f.Payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		return nil
	}
	if p.Kind == "" || p.Text == "" {
		return nil
	}

	var kind core.EscalationKind
	switch p.Kind {
	case "question":
		kind = core.EscalationQuestion
	case "blocker":
		kind = core.EscalationBlocker
	default:
		return nil
	}
	if p.Role == "coach" && kind == core.EscalationBlocker {
		// Coaches never block. The CLI already rejects this; drop the
		// frame if somehow it got through (e.g. crafted by hand).
		return nil
	}

	urgency, emit := Classify(kind, InteractionMode(p.Mode))
	if !emit {
		return nil
	}

	base.Kind = "escalation_opened"
	base.Payload["escalation_kind"] = string(kind)
	base.Payload["channel"] = string(core.ChannelToolCall)
	base.Payload["urgency"] = string(urgency)
	base.Payload["role"] = p.Role
	base.Payload["text"] = p.Text
	if p.BeadID != "" {
		base.Payload["bead_id"] = p.BeadID
	}
	if p.Title != "" {
		base.Payload["title"] = p.Title
	}
	return []core.OrchestrationEvent{base}
}

// rawPayload decodes the structured payload field into a map so the
// OrchestrationEvent.Payload (map[string]any) keeps working. Falls
// back to a single-key wrap of the raw string for non-JSON frames.
func rawPayload(f Frame) map[string]any {
	out := make(map[string]any)
	if len(f.Payload) > 0 {
		_ = json.Unmarshal(f.Payload, &out)
	}
	if f.PayloadRaw != "" {
		out["raw"] = f.PayloadRaw
	}
	return out
}

// classifyNotification inspects a Notification payload and decides
// whether to surface it as permission_prompt (tool gating) or
// hitl_approval (elicitation). Best-effort — Claude's notification
// schema varies; anything unrecognized defaults to permission_prompt.
func classifyNotification(f Frame) string {
	var probe struct {
		Type string `json:"type"`
		Kind string `json:"kind"`
	}
	if len(f.Payload) > 0 {
		_ = json.Unmarshal(f.Payload, &probe)
	}
	kind := probe.Type
	if kind == "" {
		kind = probe.Kind
	}
	switch kind {
	case "elicitation", "hitl", "approval":
		return string(core.EscalationHITLApproval)
	default:
		return string(core.EscalationPermissionPrompt)
	}
}
