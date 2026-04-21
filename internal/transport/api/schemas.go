// Package api: schemas.go exposes the boundary decoders the chi-based
// HTTP handlers (gm-e4.1) call before invoking any WorkPlane or
// OrchestrationPlane method. Per gm-io4 (t3code audit): decode and
// validate live at the boundary; adaptors trust their inputs.
//
// Every mutation handler MUST pipe its request body through one of these
// decoders. The decoder either returns a typed, validated core value or
// a *core.AdaptorError{Kind: validation} with a structured ValidationIssue
// — the HTTP surface maps that straight to a 400 response with the
// AdaptorError JSON envelope. No adaptor method runs on undecoded input.
//
// These functions are intentionally thin wrappers around the shared
// internal/transport.Decode* helpers: one source of truth for validation,
// three transport-specific entry points.
package api

import (
	"io"
	"net/http"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/transport"
)

// readBody is the shared preamble for every HTTP mutation handler: pull
// the body, cap it, and surface any read failure as a validation error
// so the downstream handler never has to branch on io errors.
//
// A 0-byte body is a validation failure, not a transport failure: the
// caller asked us to mutate something and forgot the payload.
func readBody(r *http.Request, op string) ([]byte, error) {
	if r.Body == nil {
		return nil, core.NewValidationError(core.ValidationIssue{
			Path:   op,
			Code:   "empty_body",
			Reason: "request body is required",
		})
	}
	// 1 MiB cap. Boundary payloads are small structured shapes, not
	// arbitrary blobs — an oversized body is almost certainly a caller
	// bug or a probe and should be rejected before the decoder spends
	// memory on it.
	limited := http.MaxBytesReader(nil, r.Body, 1<<20)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, core.NewValidationError(core.ValidationIssue{
			Path:   op,
			Code:   "body_read",
			Reason: err.Error(),
		})
	}
	if len(raw) == 0 {
		return nil, core.NewValidationError(core.ValidationIssue{
			Path:   op,
			Code:   "empty_body",
			Reason: "request body is required",
		})
	}
	return raw, nil
}

// DecodeCreateWorkItem reads the HTTP request body and returns the
// validated WorkItem to hand to WorkPlane.CreateWorkItem.
func DecodeCreateWorkItem(r *http.Request) (core.WorkItem, error) {
	raw, err := readBody(r, "create_work_item")
	if err != nil {
		return core.WorkItem{}, err
	}
	return transport.DecodeCreateWorkItem(raw)
}

// DecodeUpdateWorkItem reads the HTTP request body and returns the
// (id, patch) pair to hand to WorkPlane.UpdateWorkItem.
func DecodeUpdateWorkItem(r *http.Request) (core.WorkItemID, core.WorkItemPatch, error) {
	raw, err := readBody(r, "update_work_item")
	if err != nil {
		return "", core.WorkItemPatch{}, err
	}
	return transport.DecodeUpdateWorkItem(raw)
}

// DecodeAttachEvidence reads the HTTP request body and returns the
// (id, evidence) pair to hand to WorkPlane.AttachEvidence.
func DecodeAttachEvidence(r *http.Request) (core.WorkItemID, core.Evidence, error) {
	raw, err := readBody(r, "attach_evidence")
	if err != nil {
		return "", core.Evidence{}, err
	}
	return transport.DecodeAttachEvidence(raw)
}

// DecodeStartSession reads the HTTP request body for StartSession.
func DecodeStartSession(r *http.Request) (string, core.SessionPrompt, error) {
	raw, err := readBody(r, "start_session")
	if err != nil {
		return "", core.SessionPrompt{}, err
	}
	return transport.DecodeStartSession(raw)
}

// DecodePauseSession reads the HTTP request body for PauseSession.
func DecodePauseSession(r *http.Request) (string, core.ConfirmNonce, error) {
	raw, err := readBody(r, "pause_session")
	if err != nil {
		return "", "", err
	}
	return transport.DecodePauseResume(raw, "pause_session")
}

// DecodeResumeSession reads the HTTP request body for ResumeSession.
func DecodeResumeSession(r *http.Request) (string, core.ConfirmNonce, error) {
	raw, err := readBody(r, "resume_session")
	if err != nil {
		return "", "", err
	}
	return transport.DecodePauseResume(raw, "resume_session")
}

// DecodeEndSession reads the HTTP request body for EndSession.
func DecodeEndSession(r *http.Request) (string, core.SessionEndMode, core.ConfirmNonce, error) {
	raw, err := readBody(r, "end_session")
	if err != nil {
		return "", "", "", err
	}
	return transport.DecodeEndSession(raw)
}

// DecodeAcquireWorkspace reads the HTTP request body for AcquireWorkspace.
func DecodeAcquireWorkspace(r *http.Request) (core.WorkspaceRequest, error) {
	raw, err := readBody(r, "acquire_workspace")
	if err != nil {
		return core.WorkspaceRequest{}, err
	}
	return transport.DecodeAcquireWorkspace(raw)
}

// DecodeReleaseWorkspace reads the HTTP request body for ReleaseWorkspace.
func DecodeReleaseWorkspace(r *http.Request) (string, error) {
	raw, err := readBody(r, "release_workspace")
	if err != nil {
		return "", err
	}
	return transport.DecodeReleaseByID(raw, "release_workspace")
}

// DecodeReleaseReservation reads the HTTP request body for ReleaseReservation.
func DecodeReleaseReservation(r *http.Request) (string, error) {
	raw, err := readBody(r, "release_reservation")
	if err != nil {
		return "", err
	}
	return transport.DecodeReleaseByID(raw, "release_reservation")
}

// DecodeResolveEscalation reads the HTTP request body for ResolveEscalation.
func DecodeResolveEscalation(r *http.Request) (string, core.EscalationResolution, core.ConfirmNonce, error) {
	raw, err := readBody(r, "resolve_escalation")
	if err != nil {
		return "", core.EscalationResolution{}, "", err
	}
	return transport.DecodeResolveEscalation(raw)
}
