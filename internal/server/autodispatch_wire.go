// Auto-dispatch daemon adapter wiring (gm-s47n.12, spec §6).
//
// Implements the four interfaces planner.autodispatch.Daemon requires
// against the bound OrchestrationPlane + WorkPlane:
//
//   - IdleSessionLister  (§6.1) — projects SessionReady members of a pool
//   - LiveSessionLister  (§6.2) — projects SessionWorking/Prompting
//                                 across ALL pools (conflict graph wants
//                                 a global view)
//   - ReadySetReader     (§6.3) — filters the bd ready set to beads bound
//                                 to this daemon's persona via the
//                                 three-layer routing cascade
//   - SessionDispatcher  (§6.3) — calls StartSession with the gemba:*
//                                 prompt extension (reuse_pane_id +
//                                 persona_id + autodispatch=1)
//   - SessionRecycler    (§6.4) — calls OrchestrationPlane.RecycleSession,
//                                 tolerating KindUnsupported as a no-op
//                                 (gt today; native + noop ship the call)

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/planner/autodispatch"
)

// poolWiring bundles the dependencies one daemon's worth of adapter
// implementations need. Constructed by NewPoolWiring; the four exposed
// methods (IdleLister, LiveLister, ReadyReader, Dispatcher, Recycler)
// satisfy the planner's interfaces.
type poolWiring struct {
	op       core.OrchestrationPlaneAdaptor
	wp       core.WorkPlane
	pool     config.ResolvedPool
	cfg      config.PoolConfig
	readers  planner.OperationalContextReaders
	agentTyp string
}

// PoolWiring is the public alias the cli package uses to construct a
// daemon's adaptors. Produced by NewPoolWiring.
type PoolWiring = poolWiring

// NewPoolWiring constructs a PoolWiring bound to one (rig, persona)
// pool. The returned struct's methods plug into autodispatch.Daemon's
// interface fields. agentType is derived from the persona's TOML
// configuration upstream and threaded into the SessionDispatcher's
// prompt extension as `gemba:agent_type` so the native adaptor's
// pane-reuse path can find a slot.
func NewPoolWiring(
	op core.OrchestrationPlaneAdaptor,
	wp core.WorkPlane,
	pool config.ResolvedPool,
	cfg config.PoolConfig,
	readers planner.OperationalContextReaders,
	agentType string,
) *poolWiring {
	return &poolWiring{
		op:       op,
		wp:       wp,
		pool:     pool,
		cfg:      cfg,
		readers:  readers,
		agentTyp: agentType,
	}
}

// IdleLister returns the daemon's IdleSessionLister implementation.
func (w *poolWiring) IdleLister() autodispatch.IdleSessionLister {
	return idleSessionLister{w: w}
}

// LiveLister returns the daemon's LiveSessionLister implementation.
func (w *poolWiring) LiveLister() autodispatch.LiveSessionLister {
	return liveSessionLister{w: w}
}

// ReadyReader returns the daemon's ReadySetReader implementation.
func (w *poolWiring) ReadyReader() autodispatch.ReadySetReader {
	return readySetReader{w: w}
}

// Dispatcher returns the daemon's SessionDispatcher implementation.
func (w *poolWiring) Dispatcher() autodispatch.SessionDispatcher {
	return sessionDispatcher{w: w}
}

// ColdStarter returns the daemon's cold-start dispatcher.
func (w *poolWiring) ColdStarter() autodispatch.ColdStarter {
	return sessionDispatcher{w: w}
}

// Recycler returns the daemon's SessionRecycler implementation. Always
// non-nil; implementations that don't support recycle return nil from
// the Recycle method, which the daemon treats as "recycle gate
// no-op".
func (w *poolWiring) Recycler() autodispatch.SessionRecycler {
	return sessionRecycler{w: w}
}

// ── IdleSessionLister ───────────────────────────────────────────

type idleSessionLister struct{ w *poolWiring }

// ListIdle returns SessionReady members filtered to this daemon's
// pool persona. Spec §6.1: filter on Status=Ready AND
// Session.Persona == this pool's persona — pools are isolated by
// persona, so a different persona's idle members are not eligible
// for this daemon's dispatches.
func (l idleSessionLister) ListIdle(ctx context.Context) ([]planner.OperationalContext, error) {
	sessions, err := l.w.op.ListSessions(ctx, core.SessionFilter{
		Status: []core.SessionStatus{core.SessionReady},
	})
	if err != nil {
		return nil, err
	}
	out := make([]planner.OperationalContext, 0, len(sessions))
	for i := range sessions {
		s := sessions[i]
		if s.Persona != l.w.pool.Persona {
			continue
		}
		if l.w.agentTyp == "codex" {
			// Codex integration is one-shot: gemba-codex-driver runs
			// `codex exec`, reports bead-done, then exits. The pane it
			// leaves behind is not a live interactive Codex worker, so
			// auto-dispatch must cold-start a fresh exec process for
			// each bead instead of reusing a ready session row.
			continue
		}
		oc := projectToContext(ctx, &s, l.w.readers)
		if oc != nil {
			out = append(out, *oc)
		}
	}
	return out, nil
}

// ── LiveSessionLister ───────────────────────────────────────────

type liveSessionLister struct{ w *poolWiring }

// ListLive returns SessionWorking and SessionPrompting members across
// EVERY pool, not just this daemon's. The conflict graph wants a
// global view: a different-persona session that is currently working
// `gm-foo` should still surface as a workspace conflict for any
// adjacent ready bead, regardless of which daemon is asking.
func (l liveSessionLister) ListLive(ctx context.Context) ([]planner.OperationalContext, error) {
	sessions, err := l.w.op.ListSessions(ctx, core.SessionFilter{
		Status: []core.SessionStatus{core.SessionWorking, core.SessionPrompting},
	})
	if err != nil {
		return nil, err
	}
	out := make([]planner.OperationalContext, 0, len(sessions))
	for i := range sessions {
		s := sessions[i]
		oc := projectToContext(ctx, &s, l.w.readers)
		if oc != nil {
			out = append(out, *oc)
		}
	}
	return out, nil
}

// ── ReadySetReader ──────────────────────────────────────────────

type readySetReader struct{ w *poolWiring }

// ReadySet pulls beads in StateUnstarted (the bd "ready" lane) and
// filters to the subset bound to this daemon's persona via the
// three-layer cascade (spec §3.2). Beads with no resolvable persona
// are dropped here — they wait for manual drag.
func (r readySetReader) ReadySet(ctx context.Context) ([]autodispatch.ReadyBead, error) {
	if r.w.wp == nil {
		return nil, nil
	}
	items, err := r.w.wp.ListWorkItems(ctx, core.WorkItemFilter{
		StateCategory: []core.StateCategory{core.StateUnstarted},
	})
	if err != nil {
		return nil, err
	}
	out := make([]autodispatch.ReadyBead, 0, len(items))
	for _, it := range items {
		if !isAutodispatchRunnableKind(it.Kind) {
			continue
		}
		// Skip items that aren't dispatch-ready (soft-blocked).
		if it.DispatchStatus.Effective() != core.DispatchReady {
			continue
		}
		blocked, err := hasOpenBlocker(ctx, r.w.wp, it)
		if err != nil || blocked {
			continue
		}
		// Three-layer cascade: bead extras > routing > default.
		beadPersona := beadPersonaFromCustom(it.Custom)
		resolved, ok := r.w.cfg.ResolvePersona(beadPersona, it.Kind)
		if !ok {
			continue
		}
		if resolved != r.w.pool.Persona {
			continue
		}
		concepts := make([]planner.ConceptTag, 0, len(it.Concepts))
		for _, c := range it.Concepts {
			concepts = append(concepts, planner.ConceptTag(c))
		}
		repoIDs := make([]string, 0, len(it.RepositoryIDs))
		for _, r := range it.RepositoryIDs {
			repoIDs = append(repoIDs, string(r))
		}
		if len(repoIDs) == 0 && it.PrimaryRepositoryID != "" {
			repoIDs = []string{string(it.PrimaryRepositoryID)}
		}
		out = append(out, autodispatch.ReadyBead{
			BeadID:       it.ID,
			Concepts:     concepts,
			Targets:      it.Targets,
			Repositories: repoIDs,
			Branch:       firstBranchName(it.Branches),
		})
	}
	return out, nil
}

func isAutodispatchRunnableKind(kind string) bool {
	switch kind {
	case core.KindMilestone, "epic", "decision":
		return false
	default:
		return true
	}
}

func hasOpenBlocker(ctx context.Context, wp core.WorkPlane, it core.WorkItem) (bool, error) {
	for _, rel := range it.Relationships {
		if rel.Kind != core.RelBlocks || rel.To != it.ID || rel.From == "" {
			continue
		}
		blocker, err := wp.GetWorkItem(ctx, rel.From)
		if err != nil {
			return true, err
		}
		if !isClosedWorkItem(blocker) {
			return true, nil
		}
	}
	return false, nil
}

func isClosedWorkItem(it core.WorkItem) bool {
	return it.StateCategory == core.StateCompleted || it.StateCategory == core.StateCanceled
}

// beadPersonaFromCustom reads the explicit-override `persona` extra
// off a WorkItem's Custom map, the highest-precedence layer of the
// routing cascade. Returns "" when no value is set or it isn't a
// string.
func beadPersonaFromCustom(custom map[string]any) string {
	if custom == nil {
		return ""
	}
	if v, ok := custom["persona"].(string); ok {
		return v
	}
	return ""
}

// firstBranchName extracts a primary branch name when the bead
// declares one. The conflict scorer reads only one branch; multi-repo
// beads with mixed branches are still surfaced via the targets +
// repositories list.
func firstBranchName(brs []core.BeadBranch) string {
	if len(brs) == 0 {
		return ""
	}
	return brs[0].Branch
}

// ── SessionDispatcher ───────────────────────────────────────────

type sessionDispatcher struct{ w *poolWiring }

// Dispatch calls StartSession with the canonical pool prompt
// extension. The native adaptor's cap-checking branch
// (start.go:140-204) consumes `gemba:reuse_pane_id` to thread the
// new bead onto the same pane as the idle session — same pool slot,
// new bead.
func (d sessionDispatcher) Dispatch(ctx context.Context, sessionID string, beadID core.WorkItemID) error {
	if d.w.op == nil {
		return errors.New("autodispatch: no orchestration plane")
	}
	// Look up the session's pane id.
	sessions, err := d.w.op.ListSessions(ctx, core.SessionFilter{
		Status: []core.SessionStatus{core.SessionReady},
	})
	if err != nil {
		return err
	}
	paneID := ""
	for i := range sessions {
		if sessions[i].ID == sessionID {
			if pid, ok := sessions[i].ProviderMetadata["pane_id"].(string); ok {
				paneID = pid
			}
			break
		}
	}
	if paneID == "" {
		return errors.New("autodispatch: pane not found for idle session")
	}
	prompt := core.SessionPrompt{
		Extension: map[string]any{
			"gemba:bead_id":       string(beadID),
			"gemba:persona_id":    d.w.pool.Persona,
			"gemba:agent_type":    d.w.agentTyp,
			"gemba:nonce":         newAutodispatchNonce(),
			"gemba:reuse_pane_id": paneID,
			"gemba:autodispatch":  "1",
		},
	}
	_, err = d.w.op.StartSession(ctx, string(beadID), prompt)
	return err
}

// StartCold starts a fresh session for the pool when no same-persona
// idle session exists yet. It intentionally omits gemba:reuse_pane_id;
// the native adaptor provisions a new pane/worktree and the agent
// driver owns lifecycle from there.
func (d sessionDispatcher) StartCold(ctx context.Context, beadID core.WorkItemID) (string, error) {
	if d.w.op == nil {
		return "", errors.New("autodispatch: no orchestration plane")
	}
	prompt := core.SessionPrompt{
		Extension: map[string]any{
			"gemba:bead_id":      string(beadID),
			"gemba:persona_id":   d.w.pool.Persona,
			"gemba:agent_type":   d.w.agentTyp,
			"gemba:nonce":        newAutodispatchNonce(),
			"gemba:autodispatch": "1",
		},
	}
	sess, err := d.w.op.StartSession(ctx, string(beadID), prompt)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

// newAutodispatchNonce mints a fresh nonce per dispatch. The
// adaptor's dedup table keys on (sessionID, nonce); using a unique
// nonce per dispatch lets us send back-to-back dispatches against the
// same idle session without the dedup tripping. See gm-native.9.
func newAutodispatchNonce() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failures are vanishingly rare; degrade to a
		// deterministic literal so the daemon doesn't crash on a
		// random-source hiccup. The nonce won't be unique across
		// dispatches in this degraded window, but the dispatch path
		// will retry with a fresh tick.
		return "autodispatch-nonce-fallback"
	}
	return "ad-" + hex.EncodeToString(buf[:])
}

// ── SessionRecycler ─────────────────────────────────────────────

type sessionRecycler struct{ w *poolWiring }

// Recycle calls OrchestrationPlane.RecycleSession. Per the
// daemon's contract (§6.4), KindUnsupported is treated as a no-op so
// adaptors that don't support pool semantics (gt today) don't break
// the daemon's recycle gate. Any other error propagates.
func (r sessionRecycler) Recycle(ctx context.Context, sessionID string) error {
	if r.w.op == nil {
		return nil
	}
	_, err := r.w.op.RecycleSession(ctx, sessionID)
	if err == nil {
		return nil
	}
	var aerr *core.AdaptorError
	if errors.As(err, &aerr) && aerr.Kind == core.KindUnsupported {
		return nil
	}
	return err
}

// ── helpers ─────────────────────────────────────────────────────

// projectToContext joins one core.Session into the planner's
// OperationalContext. Returns nil on degraded reads — graceful
// degradation per spec §4 Layer 1, mirroring planner.
// ReadOperationalContext's contract.
func projectToContext(ctx context.Context, s *core.Session, r planner.OperationalContextReaders) *planner.OperationalContext {
	if s == nil {
		return nil
	}
	// Build a minimal OperationalContext directly from the Session.
	// We don't go through ReadOperationalContext because the Sessions
	// reader would just round-trip the session id back through the
	// orchestration plane — we already have the row. Instead we
	// populate Session + best-effort Profile/Health.
	out := &planner.OperationalContext{Session: s}
	if r.Profiles != nil {
		if p, err := r.Profiles.GetProfile(ctx, s.ID); err == nil && p != nil {
			out.Profile = p
		}
	}
	if h, err := planner.ComputeHealth(ctx, s, out.Profile, r.BeadConcepts, r.Now); err == nil {
		out.Health = h
	}
	return out
}
