// OperationalContext(session_id) read API (gm-s47n.2.5).
//
// Composes AgentRef + Session + Workspace + Assignment + SessionProfile
// + SessionHealth into the single struct gm-s47n.2.1 declared. Each
// dependency is a narrow lookup interface so the planner can be
// wired with whatever subset an adaptor actually exposes today —
// missing pieces leave the corresponding pointer nil rather than
// erroring (spec §4 Layer 1: "planner reads degrade gracefully").

package planner

import (
	"context"
	"errors"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// SessionLookup is the per-session subset of core.OrchestrationPlane
// the planner reads. Narrow interface so a fake test fixture is one
// screen of code.
//
// Why not the full OrchestrationPlane: the bead's read path needs
// exactly one method. Coupling the planner to the whole interface
// would force every test to stub two dozen methods it doesn't touch.
type SessionLookup interface {
	// FindSession returns the session by id. Implementations should
	// return (nil, nil) for "not found" so callers can distinguish
	// transient errors from genuine absence; an error here is treated
	// as fatal by ReadOperationalContext.
	FindSession(ctx context.Context, sessionID string) (*core.Session, error)
}

// AgentLookup mirrors core.OrchestrationPlane.ReadAgent.
type AgentLookup interface {
	ReadAgent(ctx context.Context, id core.AgentID) (*core.AgentRef, error)
}

// AssignmentLookup is the missing piece: there is no GetAssignment
// on core.OrchestrationPlane today. Adaptors that index assignments
// internally implement this directly; the rest can derive it from
// ObservedState() and filter (a future helper). Either way, an
// implementation lives outside this package — the planner just
// declares what it needs.
type AssignmentLookup interface {
	FindAssignment(ctx context.Context, assignmentID string) (*core.Assignment, error)
}

// WorkspaceLookup mirrors core.OrchestrationPlane.InspectWorkspace.
type WorkspaceLookup interface {
	InspectWorkspace(ctx context.Context, workspaceID string) (core.Workspace, error)
}

// ProfileLookup reads SessionProfile rows from the dolt store. The
// concrete implementation lives in gm-s47n.2.4 (Profile read API);
// declaring the interface here lets gm-s47n.2.5 ship before .2.4
// without a circular dependency.
type ProfileLookup interface {
	GetProfile(ctx context.Context, sessionID string) (*SessionProfile, error)
}

// OperationalContextReaders bundles the dependencies for
// ReadOperationalContext. Any nil reader means "skip that
// component" — the resulting OperationalContext leaves the
// corresponding pointer nil. Callers compose only the readers
// they have wired today.
//
// Now is injected so SessionHealth.TimeOnTask is deterministic in
// tests; production wires time.Now.
type OperationalContextReaders struct {
	Sessions    SessionLookup
	Agents      AgentLookup
	Assignments AssignmentLookup
	Workspaces  WorkspaceLookup
	Profiles    ProfileLookup
	// BeadConcepts is the optional gm-s47n.5.1 concept-drift input.
	// When wired, ComputeHealth uses it to derive the last-N concept
	// vector from profile.LastBeads and compute concept_drift; nil
	// leaves drift at zero. The concrete impl ships with gm-s47n.1
	// WorkItem-concept enrichment.
	BeadConcepts BeadConceptLookup
	Now          func() time.Time
}

// ErrSessionLookupRequired is returned when the Sessions reader is
// nil — every other reader is optional, but without a Session there
// is nothing to populate.
var ErrSessionLookupRequired = errors.New(
	"planner.ReadOperationalContext: Sessions reader is required",
)

// ErrSessionNotFound is returned when SessionLookup.FindSession
// returns (nil, nil) for the requested id. Distinguishes "lookup
// succeeded, no row" from "lookup errored".
var ErrSessionNotFound = errors.New("planner: session not found")

// ReadOperationalContext is the join point. Resolution order:
//
//  1. Session (required; the rest is meaningless without it).
//  2. Agent (from session.AgentID via Agents.ReadAgent).
//  3. Assignment (from session.AssignmentID).
//  4. Workspace (from assignment.WorkspaceID; nil if no assignment
//     or the assignment doesn't carry a workspace yet).
//  5. Profile (from session.ID).
//  6. Health (derived from session + profile snapshots).
//
// Errors from steps 2..5 are SWALLOWED — the partial context is
// returned with the unresolved component nil. The Sessions step is
// the only one whose error propagates; without a Session, callers
// can't do anything useful with the result anyway. This matches
// the spec §4 Layer 1 "graceful degradation" requirement: a planner
// must keep working even if one of its data sources is degraded.
func ReadOperationalContext(
	ctx context.Context,
	sessionID string,
	r OperationalContextReaders,
) (*OperationalContext, error) {
	if r.Sessions == nil {
		return nil, ErrSessionLookupRequired
	}
	sess, err := r.Sessions.FindSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, ErrSessionNotFound
	}

	out := &OperationalContext{Session: sess}

	if r.Agents != nil && sess.AgentID != "" {
		// Tolerate ReadAgent errors: a missing agent on a live session
		// is a degradation but not a planner-stopping fault. Production
		// callers may wrap this reader to log; this code stays silent
		// because graceful-degradation is the contract.
		if a, err := r.Agents.ReadAgent(ctx, sess.AgentID); err == nil && a != nil {
			out.Agent = a
		}
	}

	if r.Assignments != nil && sess.AssignmentID != "" {
		if a, err := r.Assignments.FindAssignment(ctx, sess.AssignmentID); err == nil && a != nil {
			out.Assignment = a
		}
	}

	if r.Workspaces != nil && out.Assignment != nil && out.Assignment.WorkspaceID != "" {
		if ws, err := r.Workspaces.InspectWorkspace(ctx, out.Assignment.WorkspaceID); err == nil {
			wsCopy := ws
			out.Workspace = &wsCopy
		}
	}

	if r.Profiles != nil {
		if p, err := r.Profiles.GetProfile(ctx, sessionID); err == nil && p != nil {
			out.Profile = p
		}
	}

	// ComputeHealth (gm-s47n.5.1) handles all three numbers in one
	// call: ContextPressure off profile.ContextPct, TimeOnTask off
	// now() - sess.StartedAt, and ConceptDrift via the optional
	// BeadConcepts lookup. Errors from the bead-concept lookup get
	// swallowed at this layer to keep the OperationalContext read
	// path's graceful-degradation contract; the returned snapshot
	// is best-effort.
	if h, err := ComputeHealth(ctx, sess, out.Profile, r.BeadConcepts, r.Now); err == nil {
		out.Health = h
	} else {
		// ComputeHealth ran into a non-recoverable lookup error;
		// fall back to the no-drift snapshot so the caller still
		// gets ContextPressure + TimeOnTask.
		if h, _ := ComputeHealth(ctx, sess, out.Profile, nil, r.Now); h != nil {
			out.Health = h
		}
	}
	return out, nil
}
