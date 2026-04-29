// Package mcp: schemas.go exposes the boundary decoders the MCP tool-
// call dispatcher (gm-e3.5) calls before invoking any WorkPlane or
// OrchestrationPlane method. Per gm-io4 (t3code audit): decode and
// validate live at the boundary; adaptors trust their inputs.
//
// MCP tool calls carry their arguments as a structured JSON object.
// The dispatcher marshals those arguments to bytes and hands them to
// one of these decoders, which either returns a typed core value or a
// *core.AdaptorError{Kind: validation}. The validation error round-trips
// back to the MCP peer as a tool-call error response with isError=true —
// the bound adaptor never runs on malformed input.
package mcp

import (
	"encoding/json"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/transport"
)

// decodeArgs is the MCP-specific preamble: tool-call arguments arrive as
// a typed map[string]any (or equivalent), but the shared decoders expect
// canonical JSON bytes. We re-marshal here so one code path validates
// every transport. The cost is one extra marshal per mutation; the gain
// is a single source of truth for field-level invariants.
func decodeArgs(args map[string]any) ([]byte, error) {
	if args == nil {
		// A nil args map is indistinguishable from an empty frame on the
		// wire; surface it as a validation error so the MCP client gets
		// a structured ValidationIssue back instead of a silent decode.
		return nil, core.NewValidationError(core.ValidationIssue{
			Code:   "empty_arguments",
			Reason: "tool-call arguments are required",
		})
	}
	return json.Marshal(args)
}

// DecodeCreateWorkItem decodes MCP tool-call arguments for
// work.create_work_item.
func DecodeCreateWorkItem(args map[string]any) (core.WorkItem, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return core.WorkItem{}, err
	}
	return transport.DecodeCreateWorkItem(raw)
}

// DecodeUpdateWorkItem decodes MCP tool-call arguments for
// work.update_work_item.
func DecodeUpdateWorkItem(args map[string]any) (core.WorkItemID, core.WorkItemPatch, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return "", core.WorkItemPatch{}, err
	}
	return transport.DecodeUpdateWorkItem(raw)
}

// DecodeAttachEvidence decodes MCP tool-call arguments for
// work.attach_evidence.
func DecodeAttachEvidence(args map[string]any) (core.WorkItemID, core.Evidence, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return "", core.Evidence{}, err
	}
	return transport.DecodeAttachEvidence(raw)
}

// DecodeStartSession decodes MCP tool-call arguments for
// orch.start_session.
func DecodeStartSession(args map[string]any) (string, core.SessionPrompt, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return "", core.SessionPrompt{}, err
	}
	return transport.DecodeStartSession(raw)
}

// DecodePauseSession decodes MCP tool-call arguments for
// orch.pause_session.
func DecodePauseSession(args map[string]any) (string, core.ConfirmNonce, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return "", "", err
	}
	return transport.DecodePauseResume(raw, "pause_session")
}

// DecodeResumeSession decodes MCP tool-call arguments for
// orch.resume_session.
func DecodeResumeSession(args map[string]any) (string, core.ConfirmNonce, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return "", "", err
	}
	return transport.DecodePauseResume(raw, "resume_session")
}

// DecodeEndSession decodes MCP tool-call arguments for
// orch.end_session.
func DecodeEndSession(args map[string]any) (string, core.SessionEndMode, core.ConfirmNonce, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return "", "", "", err
	}
	return transport.DecodeEndSession(raw)
}

// DecodeAcquireWorkspace decodes MCP tool-call arguments for
// orch.acquire_workspace.
func DecodeAcquireWorkspace(args map[string]any) (core.WorkspaceRequest, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return core.WorkspaceRequest{}, err
	}
	return transport.DecodeAcquireWorkspace(raw)
}

// DecodeReleaseWorkspace decodes MCP tool-call arguments for
// orch.release_workspace.
func DecodeReleaseWorkspace(args map[string]any) (string, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return "", err
	}
	return transport.DecodeReleaseByID(raw, "release_workspace")
}

// DecodeReleaseReservation decodes MCP tool-call arguments for
// orch.release_reservation.
func DecodeReleaseReservation(args map[string]any) (string, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return "", err
	}
	return transport.DecodeReleaseByID(raw, "release_reservation")
}

// DecodeResolveEscalation decodes MCP tool-call arguments for
// orch.resolve_escalation.
func DecodeResolveEscalation(args map[string]any) (string, core.EscalationResolution, core.ConfirmNonce, error) {
	raw, err := decodeArgs(args)
	if err != nil {
		return "", core.EscalationResolution{}, "", err
	}
	return transport.DecodeResolveEscalation(raw)
}
