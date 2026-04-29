// gm-vch2: WitnessLister projects witness-pipeline findings into
// core.EscalationRequest rows so the gemba walk agenda surfaces
// "the witness rig flagged something I should look at" alongside
// other escalation flavours.
//
// Approach (chosen for gm-vch2):
//
// Witness findings ride mail today (per gm-vch2 source notes —
// no in-tree mail abstraction yet, only the Gas Town mail surface
// per docs). Rather than hard-code a Gas-Town-specific dependency
// here, this lister takes a typed WitnessFindingSource: a small
// interface every adaptor that exposes witness findings can
// implement. The default in-tree implementation is the no-op
// emptyWitnessSource so a deployment without a witness rig still
// builds a coherent agenda.
//
// Once a witness adaptor lands in-tree (or the orchestration plane
// emits escalation.raised events with Source=EscalationWitnessFinding,
// in which case OrchestrationPlaneEscalationLister picks them up
// natively), the explicit upstream injection will go away. Until
// then, the upstream change is a small one: implement
// WitnessFindingSource against whatever mail/feed the witness rig
// publishes.

package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// WitnessFinding is the projection the lister consumes. Every
// witness adaptor maps its native finding shape onto this struct
// — kept minimal so adaptors don't need to know about
// core.EscalationRequest internals.
type WitnessFinding struct {
	// ID is the witness rig's own finding id. Used as the natural
	// key for the synthesized EscalationRequest.ID; if the upstream
	// re-emits the same finding the agenda dedupes on it.
	ID string
	// Title is the one-line summary the operator sees in the
	// agenda. Empty falls back to "Witness finding".
	Title string
	// Prompt is the detail body. Empty is fine — operators can
	// follow the link to the witness UI for full context.
	Prompt string
	// WorkItemID, when non-empty, scopes the finding to a bead so
	// the agenda dedupes against any bead-flavoured row for the
	// same work item.
	WorkItemID core.WorkItemID
	// AgentID, when non-empty, attributes the finding to the agent
	// it pertains to (typically the worker whose output the
	// witness scrutinised).
	AgentID core.AgentID
	// Workspace, when non-empty, scopes the finding to a workspace
	// so the lister's per-workspace filter narrows correctly.
	Workspace string
	// Urgency mirrors core.EscalationUrgency. Empty defaults to
	// UrgencyAdvisory — witness findings are advisory-by-default
	// (operator review, not session block) until a downstream
	// rule promotes them.
	Urgency core.EscalationUrgency
	// CreatedAt is the wall-clock the finding was emitted; falls
	// back to time.Now() in the synthesized EscalationRequest.
	CreatedAt time.Time
}

// WitnessFindingSource is the upstream surface the lister reads.
// Adaptors that surface witness findings implement this in their
// own package; gm-vch2 ships an empty default so a deployment
// without a witness rig builds a non-erroring agenda.
type WitnessFindingSource interface {
	ListOpenWitnessFindings(ctx context.Context, workspace string) ([]WitnessFinding, error)
}

// WitnessFindingSourceFunc is the convenience adapter so callers
// can pass a closure where an interface is wanted.
type WitnessFindingSourceFunc func(ctx context.Context, workspace string) ([]WitnessFinding, error)

// ListOpenWitnessFindings satisfies WitnessFindingSource.
func (f WitnessFindingSourceFunc) ListOpenWitnessFindings(ctx context.Context, ws string) ([]WitnessFinding, error) {
	return f(ctx, ws)
}

// EmptyWitnessSource is the explicit no-op source. Use this when
// the deployment hasn't wired a witness adaptor yet — the lister
// becomes a no-op without dropping every other agenda source.
type EmptyWitnessSource struct{}

// ListOpenWitnessFindings returns an empty slice.
func (EmptyWitnessSource) ListOpenWitnessFindings(context.Context, string) ([]WitnessFinding, error) {
	return nil, nil
}

// WitnessLister translates WitnessFinding rows into the canonical
// core.EscalationRequest shape so the walk's EscalationLister
// composition can include witness alongside the OrchestrationPlane
// surface.
//
// Construct via NewWitnessLister.
type WitnessLister struct {
	src WitnessFindingSource
}

// NewWitnessLister wires the lister. nil src returns nil so the
// composition path stays uniform with the other typed listers.
func NewWitnessLister(src WitnessFindingSource) *WitnessLister {
	if src == nil {
		return nil
	}
	return &WitnessLister{src: src}
}

// ListOpenEscalations satisfies walk.EscalationLister.
func (l *WitnessLister) ListOpenEscalations(ctx context.Context, workspace string) ([]core.EscalationRequest, error) {
	if l == nil || l.src == nil {
		return nil, nil
	}
	findings, err := l.src.ListOpenWitnessFindings(ctx, workspace)
	if err != nil {
		return nil, err
	}
	out := make([]core.EscalationRequest, 0, len(findings))
	for _, f := range findings {
		title := f.Title
		if title == "" {
			title = "Witness finding"
		}
		urgency := f.Urgency
		if urgency == "" {
			urgency = core.UrgencyAdvisory
		}
		createdAt := f.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		id := f.ID
		if id == "" {
			id = witnessFallbackID(title, f.Prompt, f.WorkItemID)
		}
		out = append(out, core.EscalationRequest{
			ID:         "witness-" + id,
			Source:     core.EscalationWitnessFinding,
			Urgency:    urgency,
			Title:      title,
			Prompt:     f.Prompt,
			WorkItemID: f.WorkItemID,
			AgentID:    f.AgentID,
			State:      core.EscalationOpen,
			CreatedAt:  createdAt,
		})
	}
	return out, nil
}

// witnessFallbackID is the stable-id fallback when the upstream
// finding doesn't carry one. Hashes (title, prompt, work-item) so
// the same content produces the same id across refreshes.
func witnessFallbackID(title, prompt string, wid core.WorkItemID) string {
	h := sha256.New()
	h.Write([]byte("witness|"))
	h.Write([]byte(title))
	h.Write([]byte("|"))
	h.Write([]byte(prompt))
	h.Write([]byte("|"))
	h.Write([]byte(string(wid)))
	return hex.EncodeToString(h.Sum(nil)[:8])
}
