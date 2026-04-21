// Package transport: schemas.go defines the adaptor-agnostic request
// shapes every transport decodes into at the boundary (gm-io4). Each
// per-transport package (api, jsonl, mcp) exports thin wrappers around
// these so the three wire shims share one source of truth for:
//
//   - which mutations are bindable over the boundary,
//   - what field-level invariants the decoder enforces before the
//     adaptor method runs,
//   - what ValidationIssue shape the UI receives when input is bad.
//
// Keeping these here (rather than duplicating across api/, jsonl/, mcp/)
// is the whole point of the t3code lesson: per-adaptor validation drift
// is what we are preventing. One decoder, three exposures.
package transport

import (
	"github.com/MikeBengtson/gemba/internal/core"
)

// WorkPlane mutation requests -------------------------------------------

// CreateWorkItemRequest is the boundary shape for WorkPlane.CreateWorkItem.
// It mirrors core.WorkItem with the server-assigned fields removed: ID
// and the two timestamps are populated by the adaptor backend, not the
// caller. Decoding rejects any caller-supplied value for those fields.
type CreateWorkItemRequest struct {
	Item core.WorkItem `json:"item"`
}

// DecodeCreateWorkItem decodes a raw request body into a validated
// WorkItem. Every required field (Kind, Title, Status, StateCategory) is
// checked; server-only fields (ID, CreatedAt, UpdatedAt) MUST be zero.
//
// Returns a *core.AdaptorError{Kind: validation} with a structured
// ValidationIssue on any failure — the transport host hands that error
// straight back to the caller without adaptor involvement.
func DecodeCreateWorkItem(raw []byte) (core.WorkItem, error) {
	var req CreateWorkItemRequest
	if err := DecodeJSON(raw, &req, "create_work_item"); err != nil {
		return core.WorkItem{}, err
	}
	wi := req.Item
	if err := RequireNonEmpty(wi.Kind, "create_work_item.item.kind"); err != nil {
		return core.WorkItem{}, err
	}
	if err := RequireNonEmpty(wi.Title, "create_work_item.item.title"); err != nil {
		return core.WorkItem{}, err
	}
	if err := RequireNonEmpty(wi.Status, "create_work_item.item.status"); err != nil {
		return core.WorkItem{}, err
	}
	if !wi.StateCategory.Valid() {
		return core.WorkItem{}, core.NewValidationError(core.ValidationIssue{
			Path:   "create_work_item.item.state_category",
			Code:   "enum",
			Reason: "must be a valid StateCategory (backlog|unstarted|started|completed|canceled)",
		})
	}
	if wi.ID != "" {
		return core.WorkItem{}, core.NewValidationError(core.ValidationIssue{
			Path:   "create_work_item.item.id",
			Code:   "server_assigned",
			Reason: "id is server-assigned; callers must not supply it on create",
		})
	}
	if !wi.CreatedAt.IsZero() || !wi.UpdatedAt.IsZero() {
		return core.WorkItem{}, core.NewValidationError(core.ValidationIssue{
			Path:   "create_work_item.item.created_at",
			Code:   "server_assigned",
			Reason: "created_at and updated_at are server-assigned; callers must not supply them",
		})
	}
	return wi, nil
}

// UpdateWorkItemRequest is the boundary shape for WorkPlane.UpdateWorkItem.
type UpdateWorkItemRequest struct {
	ID    core.WorkItemID    `json:"id"`
	Patch core.WorkItemPatch `json:"patch"`
}

// DecodeUpdateWorkItem validates the id field and the patch consistency
// invariants (Status and StateCategory, if both set, must be internally
// coherent — the adaptor's StateMap resolves the ambiguity, but the
// boundary rejects the degenerate "both nil / both empty" case).
func DecodeUpdateWorkItem(raw []byte) (core.WorkItemID, core.WorkItemPatch, error) {
	var req UpdateWorkItemRequest
	if err := DecodeJSON(raw, &req, "update_work_item"); err != nil {
		return "", core.WorkItemPatch{}, err
	}
	if err := RequireNonEmpty(string(req.ID), "update_work_item.id"); err != nil {
		return "", core.WorkItemPatch{}, err
	}
	// A patch with no fields set is a no-op and almost always a bug on
	// the caller side (empty body posted, missing merge before encode).
	// Reject at the boundary so the adaptor never has to.
	if isZeroPatch(req.Patch) {
		return "", core.WorkItemPatch{}, core.NewValidationError(core.ValidationIssue{
			Path:   "update_work_item.patch",
			Code:   "empty",
			Reason: "patch must set at least one field",
		})
	}
	if req.Patch.StateCategory != nil && !req.Patch.StateCategory.Valid() {
		return "", core.WorkItemPatch{}, core.NewValidationError(core.ValidationIssue{
			Path:   "update_work_item.patch.state_category",
			Code:   "enum",
			Reason: "must be a valid StateCategory",
		})
	}
	return req.ID, req.Patch, nil
}

func isZeroPatch(p core.WorkItemPatch) bool {
	return p.Title == nil && p.Description == nil && p.Status == nil &&
		p.StateCategory == nil && p.Priority == nil && p.Owner == nil &&
		p.Assignee == nil && len(p.Labels) == 0 && p.DoD == nil &&
		p.SprintID == nil && len(p.Custom) == 0
}

// AttachEvidenceRequest is the boundary shape for WorkPlane.AttachEvidence.
type AttachEvidenceRequest struct {
	ID       core.WorkItemID `json:"id"`
	Evidence core.Evidence   `json:"evidence"`
}

// DecodeAttachEvidence enforces work-item id presence and evidence-kind
// enumeration at the boundary.
func DecodeAttachEvidence(raw []byte) (core.WorkItemID, core.Evidence, error) {
	var req AttachEvidenceRequest
	if err := DecodeJSON(raw, &req, "attach_evidence"); err != nil {
		return "", core.Evidence{}, err
	}
	if err := RequireNonEmpty(string(req.ID), "attach_evidence.id"); err != nil {
		return "", core.Evidence{}, err
	}
	if err := RequireEnum(req.Evidence.Kind, validEvidenceKinds, "attach_evidence.evidence.kind"); err != nil {
		return "", core.Evidence{}, err
	}
	if err := RequireNonEmpty(req.Evidence.Source, "attach_evidence.evidence.source"); err != nil {
		return "", core.Evidence{}, err
	}
	return req.ID, req.Evidence, nil
}

var validEvidenceKinds = map[core.EvidenceKind]struct{}{
	core.EvidenceCommit:     {},
	core.EvidenceLog:        {},
	core.EvidenceTestResult: {},
	core.EvidenceURL:        {},
	core.EvidenceFile:       {},
	core.EvidenceCustom:     {},
}

// OrchestrationPlane mutation requests ----------------------------------

// StartSessionRequest is the boundary shape for
// OrchestrationPlaneAdaptor.StartSession.
type StartSessionRequest struct {
	AssignmentID string             `json:"assignment_id"`
	Prompt       core.SessionPrompt `json:"prompt"`
}

// DecodeStartSession validates the assignment id. SessionPrompt is
// free-form (text + extension) so no shape check beyond JSON decode.
func DecodeStartSession(raw []byte) (string, core.SessionPrompt, error) {
	var req StartSessionRequest
	if err := DecodeJSON(raw, &req, "start_session"); err != nil {
		return "", core.SessionPrompt{}, err
	}
	if err := RequireNonEmpty(req.AssignmentID, "start_session.assignment_id"); err != nil {
		return "", core.SessionPrompt{}, err
	}
	return req.AssignmentID, req.Prompt, nil
}

// EndSessionRequest is the boundary shape for
// OrchestrationPlaneAdaptor.EndSession.
type EndSessionRequest struct {
	SessionID string              `json:"session_id"`
	Mode      core.SessionEndMode `json:"mode"`
	Nonce     core.ConfirmNonce   `json:"nonce"`
}

// DecodeEndSession validates the session id, the mode enumeration, and
// the nonce presence. Nonce is required at the boundary because
// EndSession is idempotent under the same nonce (domain.md §3.7 /
// conformance B.4) — a missing nonce means replay safety is gone.
func DecodeEndSession(raw []byte) (string, core.SessionEndMode, core.ConfirmNonce, error) {
	var req EndSessionRequest
	if err := DecodeJSON(raw, &req, "end_session"); err != nil {
		return "", "", "", err
	}
	if err := RequireNonEmpty(req.SessionID, "end_session.session_id"); err != nil {
		return "", "", "", err
	}
	if err := RequireEnum(req.Mode, validSessionEndModes, "end_session.mode"); err != nil {
		return "", "", "", err
	}
	if err := RequireNonEmpty(string(req.Nonce), "end_session.nonce"); err != nil {
		return "", "", "", err
	}
	return req.SessionID, req.Mode, req.Nonce, nil
}

var validSessionEndModes = map[core.SessionEndMode]struct{}{
	core.SessionEndCompleted: {},
	core.SessionEndFailed:    {},
	core.SessionEndCanceled:  {},
}

// PauseResumeRequest is the boundary shape shared by PauseSession and
// ResumeSession — both take (session_id, nonce).
type PauseResumeRequest struct {
	SessionID string            `json:"session_id"`
	Nonce     core.ConfirmNonce `json:"nonce"`
}

// DecodePauseResume validates (session_id, nonce). Used by both pause
// and resume; the op name is provided so Path is accurate in the issue.
func DecodePauseResume(raw []byte, op string) (string, core.ConfirmNonce, error) {
	var req PauseResumeRequest
	if err := DecodeJSON(raw, &req, op); err != nil {
		return "", "", err
	}
	if err := RequireNonEmpty(req.SessionID, op+".session_id"); err != nil {
		return "", "", err
	}
	if err := RequireNonEmpty(string(req.Nonce), op+".nonce"); err != nil {
		return "", "", err
	}
	return req.SessionID, req.Nonce, nil
}

// AcquireWorkspaceRequest is the boundary shape for AcquireWorkspace.
type AcquireWorkspaceRequest struct {
	Request core.WorkspaceRequest `json:"request"`
}

// DecodeAcquireWorkspace validates assignment_id, repository, and (when
// set) preferred_kind enum membership.
func DecodeAcquireWorkspace(raw []byte) (core.WorkspaceRequest, error) {
	var req AcquireWorkspaceRequest
	if err := DecodeJSON(raw, &req, "acquire_workspace"); err != nil {
		return core.WorkspaceRequest{}, err
	}
	if err := RequireNonEmpty(req.Request.AssignmentID, "acquire_workspace.request.assignment_id"); err != nil {
		return core.WorkspaceRequest{}, err
	}
	if err := RequireNonEmpty(req.Request.Repository, "acquire_workspace.request.repository"); err != nil {
		return core.WorkspaceRequest{}, err
	}
	if req.Request.PreferredKind != "" {
		if err := RequireEnum(req.Request.PreferredKind, validWorkspaceKinds, "acquire_workspace.request.preferred_kind"); err != nil {
			return core.WorkspaceRequest{}, err
		}
	}
	return req.Request, nil
}

var validWorkspaceKinds = map[core.WorkspaceKind]struct{}{
	core.WorkspaceWorktree:   {},
	core.WorkspaceContainer:  {},
	core.WorkspaceK8sPod:     {},
	core.WorkspaceVM:         {},
	core.WorkspaceExec:       {},
	core.WorkspaceSubprocess: {},
}

// ResolveEscalationRequest is the boundary shape for ResolveEscalation.
type ResolveEscalationRequest struct {
	EscalationID string                    `json:"escalation_id"`
	Resolution   core.EscalationResolution `json:"resolution"`
	Nonce        core.ConfirmNonce         `json:"nonce"`
}

// DecodeResolveEscalation validates escalation_id, resolution kind enum,
// resolved_by presence, and nonce. The resolved_at timestamp is free-
// form (adaptor may stamp its own) so we tolerate zero.
func DecodeResolveEscalation(raw []byte) (string, core.EscalationResolution, core.ConfirmNonce, error) {
	var req ResolveEscalationRequest
	if err := DecodeJSON(raw, &req, "resolve_escalation"); err != nil {
		return "", core.EscalationResolution{}, "", err
	}
	if err := RequireNonEmpty(req.EscalationID, "resolve_escalation.escalation_id"); err != nil {
		return "", core.EscalationResolution{}, "", err
	}
	if err := RequireEnum(req.Resolution.Kind, validResolutionKinds, "resolve_escalation.resolution.kind"); err != nil {
		return "", core.EscalationResolution{}, "", err
	}
	if err := RequireNonEmpty(string(req.Resolution.ResolvedBy), "resolve_escalation.resolution.resolved_by"); err != nil {
		return "", core.EscalationResolution{}, "", err
	}
	if err := RequireNonEmpty(string(req.Nonce), "resolve_escalation.nonce"); err != nil {
		return "", core.EscalationResolution{}, "", err
	}
	return req.EscalationID, req.Resolution, req.Nonce, nil
}

var validResolutionKinds = map[core.EscalationResolutionKind]struct{}{
	core.ResolutionApprove: {},
	core.ResolutionDeny:    {},
	core.ResolutionModify:  {},
	core.ResolutionDefer:   {},
}

// ReleaseByIDRequest is the shape shared by any mutation whose only
// input is a single opaque id (ReleaseReservation, ReleaseWorkspace).
type ReleaseByIDRequest struct {
	ID string `json:"id"`
}

// DecodeReleaseByID validates the id field for the two release calls.
// The op string becomes the Path prefix.
func DecodeReleaseByID(raw []byte, op string) (string, error) {
	var req ReleaseByIDRequest
	if err := DecodeJSON(raw, &req, op); err != nil {
		return "", err
	}
	if err := RequireNonEmpty(req.ID, op+".id"); err != nil {
		return "", err
	}
	return req.ID, nil
}
