// Package jsonl: schemas.go exposes the boundary decoders the stdio
// frame pump (gm-e3.5) calls before invoking any WorkPlane or
// OrchestrationPlane method. Per gm-io4 (t3code audit): decode and
// validate live at the boundary; adaptors trust their inputs.
//
// Frames on the wire are single-line JSON envelopes carrying a method
// name and a params object. The pump strips the envelope and passes the
// params bytes to one of these decoders — the returned typed value is
// what gets handed to the bound adaptor. A validation failure is
// rendered straight back to the peer as an AdaptorError response frame
// with Kind=validation, bypassing the adaptor entirely.
package jsonl

import (
	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/transport"
)

// DecodeCreateWorkItem decodes the params of a create_work_item frame.
func DecodeCreateWorkItem(params []byte) (core.WorkItem, error) {
	return transport.DecodeCreateWorkItem(params)
}

// DecodeUpdateWorkItem decodes the params of an update_work_item frame.
func DecodeUpdateWorkItem(params []byte) (core.WorkItemID, core.WorkItemPatch, error) {
	return transport.DecodeUpdateWorkItem(params)
}

// DecodeAttachEvidence decodes the params of an attach_evidence frame.
func DecodeAttachEvidence(params []byte) (core.WorkItemID, core.Evidence, error) {
	return transport.DecodeAttachEvidence(params)
}

// DecodeStartSession decodes the params of a start_session frame.
func DecodeStartSession(params []byte) (string, core.SessionPrompt, error) {
	return transport.DecodeStartSession(params)
}

// DecodePauseSession decodes the params of a pause_session frame.
func DecodePauseSession(params []byte) (string, core.ConfirmNonce, error) {
	return transport.DecodePauseResume(params, "pause_session")
}

// DecodeResumeSession decodes the params of a resume_session frame.
func DecodeResumeSession(params []byte) (string, core.ConfirmNonce, error) {
	return transport.DecodePauseResume(params, "resume_session")
}

// DecodeEndSession decodes the params of an end_session frame.
func DecodeEndSession(params []byte) (string, core.SessionEndMode, core.ConfirmNonce, error) {
	return transport.DecodeEndSession(params)
}

// DecodeAcquireWorkspace decodes the params of an acquire_workspace frame.
func DecodeAcquireWorkspace(params []byte) (core.WorkspaceRequest, error) {
	return transport.DecodeAcquireWorkspace(params)
}

// DecodeReleaseWorkspace decodes the params of a release_workspace frame.
func DecodeReleaseWorkspace(params []byte) (string, error) {
	return transport.DecodeReleaseByID(params, "release_workspace")
}

// DecodeReleaseReservation decodes the params of a release_reservation frame.
func DecodeReleaseReservation(params []byte) (string, error) {
	return transport.DecodeReleaseByID(params, "release_reservation")
}

// DecodeResolveEscalation decodes the params of a resolve_escalation frame.
func DecodeResolveEscalation(params []byte) (string, core.EscalationResolution, core.ConfirmNonce, error) {
	return transport.DecodeResolveEscalation(params)
}
