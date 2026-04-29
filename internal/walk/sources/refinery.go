// gm-vch2: RefineryRejectionLister projects refinery merge-rejection
// signals into core.EscalationRequest rows so the gemba walk agenda
// surfaces "this PR was rejected and needs a human" alongside other
// escalation flavours.
//
// Approach (chosen for gm-vch2):
//
// The canonical wire is for the refinery rig to publish through the
// OrchestrationPlane carrying core.EscalationKind=
// EscalationRefineryRejection. When that wiring lands, the live
// OrchestrationPlaneEscalationLister already picks them up — this
// typed lister exists so:
//
//  1. The constant is reachable from one canonical location
//     (core.EscalationRefineryRejection).
//  2. A side-channel source (a shim that polls a refinery feed
//     directly) can be plugged in without a refinery → adaptor
//     re-architecture: implement RefineryRejectionSource against
//     whatever the refinery rig actually publishes today.
//  3. The default in-tree wiring is the no-op EmptyRefinerySource
//     so a deployment without a refinery rig still builds a
//     coherent agenda.
//
// Once refinery publishes natively through the OrchestrationPlane
// this lister can be retired (or stay as a redundant typed-filter
// surface) without churning the EscalationRequest shape.
//
// Upstream change required (filed as a follow-up to gm-vch2): the
// refinery rig's reject path must emit an OrchestrationEvent with
// kind=escalation.raised + payload.escalation_kind="refinery_rejection"
// and the canonical EscalationRequest fields. Until that lands,
// callers wire a custom RefineryRejectionSource if they want
// rejections in the walk; otherwise the lister contributes zero.

package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// RefineryRejection is the projection the lister consumes. Adaptors
// publishing refinery rejections map their native shape onto this
// minimal struct.
type RefineryRejection struct {
	// ID is the refinery rig's own rejection id (e.g. the PR number
	// or a refinery-internal handle). Used as the natural key for
	// the synthesized EscalationRequest.ID.
	ID string
	// Title is the one-line summary the operator sees. Empty falls
	// back to "Refinery rejection".
	Title string
	// Prompt is the rejection reason. Often the refinery rig's
	// blocker note plus a link to the rejected PR.
	Prompt string
	// WorkItemID, when non-empty, scopes the rejection to the bead
	// the PR was meant to close.
	WorkItemID core.WorkItemID
	// AgentID, when non-empty, attributes the rejection to the
	// worker whose PR was rejected.
	AgentID core.AgentID
	// Workspace scopes the rejection. Empty means workspace-agnostic.
	Workspace string
	// CreatedAt is the rejection timestamp.
	CreatedAt time.Time
}

// RefineryRejectionSource is the upstream surface the lister reads.
type RefineryRejectionSource interface {
	ListOpenRefineryRejections(ctx context.Context, workspace string) ([]RefineryRejection, error)
}

// RefineryRejectionSourceFunc adapts a closure to the interface.
type RefineryRejectionSourceFunc func(ctx context.Context, workspace string) ([]RefineryRejection, error)

// ListOpenRefineryRejections satisfies RefineryRejectionSource.
func (f RefineryRejectionSourceFunc) ListOpenRefineryRejections(ctx context.Context, ws string) ([]RefineryRejection, error) {
	return f(ctx, ws)
}

// EmptyRefinerySource is the explicit no-op source.
type EmptyRefinerySource struct{}

// ListOpenRefineryRejections returns an empty slice.
func (EmptyRefinerySource) ListOpenRefineryRejections(context.Context, string) ([]RefineryRejection, error) {
	return nil, nil
}

// RefineryRejectionLister synthesizes core.EscalationRequest rows
// with Source=core.EscalationRefineryRejection from a
// RefineryRejectionSource. Refinery rejections are blocking — a
// rejected PR halts the bead's progress until a human looks at it
// — so Urgency is hard-coded to UrgencyBlocking.
type RefineryRejectionLister struct {
	src RefineryRejectionSource
}

// NewRefineryRejectionLister wires the lister. nil src returns nil
// so the composition path stays uniform with the other typed
// listers.
func NewRefineryRejectionLister(src RefineryRejectionSource) *RefineryRejectionLister {
	if src == nil {
		return nil
	}
	return &RefineryRejectionLister{src: src}
}

// ListOpenEscalations satisfies walk.EscalationLister.
func (l *RefineryRejectionLister) ListOpenEscalations(ctx context.Context, workspace string) ([]core.EscalationRequest, error) {
	if l == nil || l.src == nil {
		return nil, nil
	}
	rejs, err := l.src.ListOpenRefineryRejections(ctx, workspace)
	if err != nil {
		return nil, err
	}
	out := make([]core.EscalationRequest, 0, len(rejs))
	for _, r := range rejs {
		title := r.Title
		if title == "" {
			title = "Refinery rejection"
		}
		createdAt := r.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		id := r.ID
		if id == "" {
			id = refineryFallbackID(title, r.Prompt, r.WorkItemID)
		}
		out = append(out, core.EscalationRequest{
			ID:         "refinery-" + id,
			Source:     core.EscalationRefineryRejection,
			Urgency:    core.UrgencyBlocking,
			Title:      title,
			Prompt:     r.Prompt,
			WorkItemID: r.WorkItemID,
			AgentID:    r.AgentID,
			State:      core.EscalationOpen,
			CreatedAt:  createdAt,
		})
	}
	return out, nil
}

// refineryFallbackID hashes the fields when the upstream omits an
// id, so repeated refreshes produce the same row.
func refineryFallbackID(title, prompt string, wid core.WorkItemID) string {
	h := sha256.New()
	h.Write([]byte("refinery|"))
	h.Write([]byte(title))
	h.Write([]byte("|"))
	h.Write([]byte(prompt))
	h.Write([]byte("|"))
	h.Write([]byte(string(wid)))
	return hex.EncodeToString(h.Sum(nil)[:8])
}
