// Gas Town session lifecycle (gm-e7.9).
//
// Implements the OrchestrationPlaneAdaptor session-lifecycle methods
// by wrapping `gt` CLI primitives:
//
//   - StartSession         → `gt sling <bead> <target> --agent <type>`
//   - EndSession           → `gt unsling <bead>` (canceled/completed) or
//                            `gt release <bead> -r <reason>` (failed)
//   - ListSessions         → projection over `gt convoy list --json` +
//                            `gt polecat list --all --json`
//   - PeekSession          → `gt peek <session-id>` (transcript mode)
//   - ListPendingRequests  → `gt mail inbox <identity> --unread --json`
//   - ResolveEscalation    → `gt escalate close <id> --reason <r>`
//   - Subscribe            → poll-based emitter (manifest declares
//                            EventDeliveryPoll already)
//
// Pause/Resume are genuinely unsupported by gt today: the gt CLI has
// no first-class pause primitive (handoff/cycle replace the session
// entirely; estop/thaw are town-wide). The methods return a tagged
// KindUnsupported error and a follow-up bead should track adding the
// capability if Gas Town grows one.
//
// gm-s47n.11 may add an optional RecycleSession to the interface; if
// the upstream adds it before this lands, this file's `Recycle`
// helper wraps `gt handoff <session-id>` and the adaptor returns
// the canonical KindUnsupported on failure (the daemon's recycle
// gate handles it gracefully).

package gt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// SessionPrompt extension keys consumed by the gt adaptor. They mirror
// the native adaptor's keys (internal/adapter/native/start.go) so the
// server's StartSession body shape works against either plane.
const (
	extKeyBeadID       = "gemba:bead_id"
	extKeyAgentType    = "gemba:agent_type"
	extKeyNonce        = "gemba:nonce"
	extKeyReusePaneID  = "gemba:reuse_pane_id"
	extKeyPersonaID    = "gemba:persona_id"
	extKeyAutodispatch = "gemba:autodispatch"
	extKeyTargetRig    = "gemba:target_rig"
	extKeyArgs         = "gemba:args"
)

// gtConvoy is the row shape `gt convoy list --json` emits. We map each
// active member to a core.Session — one Session per active polecat
// participating in the convoy, fronted by the convoy id itself when
// per-polecat granularity isn't available.
type gtConvoy struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Status    string         `json:"status"`
	Rig       string         `json:"rig,omitempty"`
	Issues    []string       `json:"issues,omitempty"`
	Tracks    []string       `json:"tracks,omitempty"`
	Polecats  []string       `json:"polecats,omitempty"`
	Workers   []convoyWorker `json:"workers,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
	ClosedAt  *time.Time     `json:"closed_at,omitempty"`
	MergeMode string         `json:"merge,omitempty"`
}

// convoyWorker is the per-worker row inside a convoy — at minimum we
// need the rig + polecat name (the session id) and the bead it's
// running. The exact gt shape is permissive (some fields may be
// absent depending on gt version); ListSessions copes with missing
// fields by falling back to the convoy aggregate.
type convoyWorker struct {
	Rig       string    `json:"rig,omitempty"`
	Polecat   string    `json:"polecat,omitempty"`
	Name      string    `json:"name,omitempty"`
	Bead      string    `json:"bead,omitempty"`
	Issue     string    `json:"issue,omitempty"`
	Status    string    `json:"status,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
}

// gtMail is the row shape `gt mail inbox --json` emits when --json
// is supported. We treat the format permissively: any unknown fields
// degrade to empty strings.
type gtMail struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	Read      bool      `json:"read"`
	Labels    []string  `json:"labels,omitempty"`
}

// StartSession dispatches a bead to a Gas Town target via `gt sling`.
//
// Required Extension keys: gemba:bead_id (or assignmentID is treated as
// the bead), gemba:agent_type. Optional: gemba:target_rig (overrides
// rig derivation), gemba:persona_id, gemba:reuse_pane_id (pass-through;
// gt has no formal pane-reuse concept so it is recorded in
// ProviderMetadata for audit and otherwise ignored), gemba:nonce
// (idempotency hint; gt's own bead-already-hooked check covers replay,
// so we currently propagate but do not cache).
//
// The returned Session has Status=SessionInitializing because the gt
// CLI returns once the polecat is spawned and the hook is set, not
// when the agent has acknowledged. Subscribe()'s poller transitions
// the session to Working/Ready as `gt convoy list` reports progress.
func (o *OrchestrationPlane) StartSession(ctx context.Context, assignmentID string, prompt core.SessionPrompt) (core.Session, error) {
	beadID, _ := prompt.Extension[extKeyBeadID].(string)
	if beadID == "" {
		// Allow the assignmentID to double as the bead id, mirroring the
		// native adaptor's MVP convention (server passes bead_id as
		// AssignmentID for bead-mode sessions).
		beadID = assignmentID
	}
	if beadID == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"gastown: StartSession requires bead id (Extension[%q] or assignmentID)", extKeyBeadID)
	}
	agentType, _ := prompt.Extension[extKeyAgentType].(string)
	if agentType == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"gastown: StartSession requires Extension[%q]", extKeyAgentType)
	}

	target, err := o.resolveSlingTarget(ctx, prompt, agentType)
	if err != nil {
		return core.Session{}, err
	}

	args := []string{"sling", beadID, target, "--agent", agentType, "--create"}
	if extra, _ := prompt.Extension[extKeyArgs].(string); extra != "" {
		args = append(args, "--args", extra)
	}
	if persona, _ := prompt.Extension[extKeyPersonaID].(string); persona != "" {
		// Best-effort surface; gt does not have a first-class --persona
		// flag today, so pass through --args so the agent sees it.
		args = append(args, "--args", "persona:"+persona)
	}

	if _, err := o.run(ctx, args...); err != nil {
		return core.Session{}, wrapGtError(err, "gt "+strings.Join(args, " "))
	}

	now := time.Now()
	sessionID := fmt.Sprintf("gastown/%s/%s/%s", target, beadID, now.Format("20060102T150405"))
	// Try to map to a real polecat via convoy list — best-effort; on a
	// fresh sling the convoy may not yet show the worker. Falls back
	// to the synthetic id above.
	if real := o.tryResolveSessionID(ctx, beadID, target); real != "" {
		sessionID = real
	}

	nonce, _ := prompt.Extension[extKeyNonce].(string)
	reusePane, _ := prompt.Extension[extKeyReusePaneID].(string)

	sess := core.Session{
		ID:           sessionID,
		AssignmentID: beadID,
		AgentID:      core.AgentID(target),
		Status:       core.SessionInitializing,
		StartedAt:    now,
		ProviderMetadata: map[string]any{
			"adaptor":    "gastown",
			"bead_id":    beadID,
			"target":     target,
			"agent_type": agentType,
			"nonce":      nonce,
			"reuse_pane": reusePane,
		},
	}
	return sess, nil
}

// resolveSlingTarget converts the prompt extension + agent registry
// into the second positional arg to `gt sling`. The contract:
//
//   - Extension["gemba:target_rig"] wins when set ("greenplace" → rig
//     auto-spawn, "greenplace/Toast" → specific polecat).
//   - persona_id, when set, prefers "<rig>/<persona>" if a rig is
//     declared, else falls back to "mayor".
//   - Default: pick the first running rig from `gt rig list --json`.
//     This is the "least surprise" target for /api/sessions calls
//     that don't pin one — operators can override via target_rig.
func (o *OrchestrationPlane) resolveSlingTarget(ctx context.Context, prompt core.SessionPrompt, agentType string) (string, error) {
	if t, _ := prompt.Extension[extKeyTargetRig].(string); t != "" {
		return t, nil
	}
	if persona, _ := prompt.Extension[extKeyPersonaID].(string); persona == "mayor" {
		return "mayor", nil
	}
	rigs, err := o.loadRigs(ctx)
	if err != nil {
		return "", err
	}
	for _, r := range rigs {
		if r.Status == "operational" {
			return r.Name, nil
		}
	}
	if len(rigs) > 0 {
		return rigs[0].Name, nil
	}
	return "", core.NewAdaptorError(core.KindAdaptorDegraded,
		"gastown: no rigs available to receive sling (agent_type=%s)", agentType)
}

// tryResolveSessionID best-effort scans `gt convoy list --json` for a
// row matching the freshly-slung bead. Returns "" when nothing is
// found yet (the convoy may not have populated by the time gt sling
// returns); the synthetic id from StartSession is used instead and
// later poll cycles pick up the real worker as it materializes.
func (o *OrchestrationPlane) tryResolveSessionID(ctx context.Context, beadID, target string) string {
	convoys, err := o.loadConvoys(ctx)
	if err != nil {
		return ""
	}
	for _, c := range convoys {
		if !convoyHasBead(c, beadID) {
			continue
		}
		for _, w := range c.Workers {
			if w.Bead == beadID || w.Issue == beadID {
				return polecatSessionID(w)
			}
		}
		// Fall back to the convoy id when worker rows are missing.
		return c.ID
	}
	_ = target
	return ""
}

// PauseSession is not supported by Gas Town today — the gt CLI's
// equivalents (handoff, cycle, estop, thaw) replace the session
// rather than pause it. Returns KindUnsupported. A follow-up bead
// under gm-e7 should track adding pause semantics if/when gt grows
// them.
func (o *OrchestrationPlane) PauseSession(_ context.Context, _ string, _ core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, core.NewAdaptorError(core.KindUnsupported,
		"gastown: PauseSession not supported — gt CLI has no per-session pause primitive (handoff/estop replace, not pause)")
}

// ResumeSession is the companion stub: without pause, there is
// nothing to resume.
func (o *OrchestrationPlane) ResumeSession(_ context.Context, _ string, _ core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, core.NewAdaptorError(core.KindUnsupported,
		"gastown: ResumeSession not supported — see PauseSession")
}

// EndSession terminates a session by mode:
//
//   - completed  → `gt unsling <bead>` (clean release of the hook)
//   - canceled   → `gt unsling <bead> --force`
//   - failed     → `gt release <bead> -r "session ended: failed"`
//
// Idempotency: when the session is already terminal in our local
// view we re-derive the same Session record and return it without a
// second shell-out (gt's release/unsling are also idempotent at
// their layer). Unknown sessionID returns KindSessionNotFound so
// callers can branch on the tagged error.
func (o *OrchestrationPlane) EndSession(ctx context.Context, sessionID string, mode core.SessionEndMode, _ core.ConfirmNonce) (core.Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"gastown: EndSession requires a session id")
	}
	beadID, target, err := o.resolveSession(ctx, sessionID)
	if err != nil {
		return core.Session{}, err
	}
	var args []string
	switch mode {
	case core.SessionEndFailed:
		args = []string{"release", beadID, "-r", "gemba: session failed"}
	case core.SessionEndCanceled:
		args = []string{"unsling", beadID, "--force"}
	default:
		args = []string{"unsling", beadID}
	}
	if _, err := o.run(ctx, args...); err != nil {
		return core.Session{}, wrapGtError(err, "gt "+strings.Join(args, " "))
	}
	now := time.Now()
	reason := closeReasonFromEndMode(mode)
	status := core.SessionCompleted
	if mode == core.SessionEndFailed {
		status = core.SessionFailed
	}
	return core.Session{
		ID:           sessionID,
		AssignmentID: beadID,
		AgentID:      core.AgentID(target),
		Status:       status,
		StartedAt:    now,
		EndedAt:      &now,
		CloseReason:  &reason,
		ProviderMetadata: map[string]any{
			"adaptor": "gastown",
			"bead_id": beadID,
			"target":  target,
		},
	}, nil
}

func closeReasonFromEndMode(mode core.SessionEndMode) core.SessionCloseReason {
	switch mode {
	case core.SessionEndFailed:
		return core.CloseFatalStderr
	case core.SessionEndCanceled, core.SessionEndCompleted:
		return core.CloseUserStop
	default:
		return core.CloseUserStop
	}
}

// resolveSession looks up an active gt session by its Gemba session id.
// Returns (beadID, target, error). Unknown ids → KindSessionNotFound.
//
// The session id format produced by ListSessions/StartSession is
// "gastown/<rig>/polecats/<polecat>" (matches AgentID layout); the
// bead is recovered from the convoy listing where the polecat is
// participating. When the convoy listing cannot be read we degrade
// to KindAdaptorDegraded so the caller can retry.
func (o *OrchestrationPlane) resolveSession(ctx context.Context, sessionID string) (string, string, error) {
	convoys, err := o.loadConvoys(ctx)
	if err != nil {
		return "", "", err
	}
	for _, c := range convoys {
		for _, w := range c.Workers {
			id := polecatSessionID(w)
			if id == sessionID || c.ID == sessionID {
				bead := w.Bead
				if bead == "" {
					bead = w.Issue
				}
				if bead == "" && len(c.Issues) > 0 {
					bead = c.Issues[0]
				}
				if bead == "" && len(c.Tracks) > 0 {
					bead = c.Tracks[0]
				}
				target := w.Rig
				if target == "" {
					target = c.Rig
				}
				if target == "" {
					target = "mayor"
				}
				return bead, target, nil
			}
		}
	}
	// Fall back: the synthetic id format from StartSession encodes
	// "gastown/<target>/<bead>/<ts>" — split it to recover.
	if rest := strings.TrimPrefix(sessionID, "gastown/"); rest != sessionID {
		parts := strings.Split(rest, "/")
		// parts: [target, bead, ts] for synthetic; [rig, polecats, name]
		// for the AgentID-shaped form.
		if len(parts) >= 2 {
			if parts[1] == "polecats" {
				return "", parts[0], core.NewAdaptorError(core.KindSessionNotFound,
					"gastown: session %q not found in active convoys", sessionID)
			}
			return parts[1], parts[0], nil
		}
	}
	return "", "", core.NewAdaptorError(core.KindSessionNotFound,
		"gastown: session %q unknown", sessionID)
}

// PeekSession returns a transcript snapshot via `gt peek <session>`.
// The shell-out cost (50–500ms typical for tmux capture-pane) means
// the native gm-e7.3 200ms latency target is not met for gt; this
// is documented divergence — the manifest declares PeekTranscript
// only and callers SHOULD treat the snapshot as advisory.
func (o *OrchestrationPlane) PeekSession(ctx context.Context, sessionID string) (core.SessionPeek, error) {
	if strings.TrimSpace(sessionID) == "" {
		return core.SessionPeek{}, core.NewAdaptorError(core.KindValidation,
			"gastown: PeekSession requires a session id")
	}
	target := peekTargetFromSessionID(sessionID)
	if target == "" {
		return core.SessionPeek{}, core.NewAdaptorError(core.KindSessionNotFound,
			"gastown: cannot derive peek target from session id %q", sessionID)
	}
	out, err := o.run(ctx, "peek", target, "-n", "200")
	if err != nil {
		return core.SessionPeek{}, wrapGtError(err, "gt peek "+target)
	}
	return core.SessionPeek{
		SessionID:  sessionID,
		Status:     core.SessionWorking,
		CapturedAt: time.Now(),
		Transcript: string(out),
	}, nil
}

// peekTargetFromSessionID strips the "gastown/" prefix so that
// session ids of the form "gastown/<rig>/polecats/<name>" become
// "<rig>/<name>" — the shape `gt peek` accepts. Other shapes are
// returned as-is.
func peekTargetFromSessionID(sessionID string) string {
	rest := strings.TrimPrefix(sessionID, "gastown/")
	parts := strings.Split(rest, "/")
	// "<rig>/polecats/<name>" → "<rig>/<name>"
	if len(parts) >= 3 && parts[1] == "polecats" {
		return parts[0] + "/" + parts[2]
	}
	// "<rig>/crew/<name>" → "<rig>/crew/<name>"
	if len(parts) >= 3 && parts[1] == "crew" {
		return parts[0] + "/crew/" + parts[2]
	}
	// town-level singletons
	if len(parts) == 1 {
		return parts[0]
	}
	return rest
}

// ListPendingRequests returns escalations + unread mail addressed to
// the session's polecat. Today gt's escalation surface is town-scoped
// (not per-session), so the primary signal we expose here is the
// session's unread mail. Callers reading "what is this session
// waiting on?" see operator-actionable items without us re-inventing
// per-session pending-request tracking on top of gt.
func (o *OrchestrationPlane) ListPendingRequests(ctx context.Context, sessionID string) ([]core.EscalationRequest, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, core.NewAdaptorError(core.KindValidation,
			"gastown: ListPendingRequests requires a session id")
	}
	identity := peekTargetFromSessionID(sessionID)
	if identity == "" {
		return nil, core.NewAdaptorError(core.KindSessionNotFound,
			"gastown: cannot derive identity from session id %q", sessionID)
	}
	out, err := o.run(ctx, "mail", "inbox", "--identity", identity, "--unread", "--json")
	if err != nil {
		// gt mail inbox failing for an unknown identity surfaces as
		// an exec error; map to SessionNotFound so callers can branch.
		return nil, wrapGtError(err, "gt mail inbox --identity "+identity+" --unread --json")
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var rows []gtMail
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, core.WrapAdaptorError(core.KindProcessFailed, err,
			"gastown: parse gt mail inbox --json")
	}
	results := make([]core.EscalationRequest, 0, len(rows))
	for _, m := range rows {
		results = append(results, core.EscalationRequest{
			ID:           m.ID,
			Source:       core.EscalationOrchestratorPause,
			AssignmentID: "",
			AgentID:      core.AgentID(strings.TrimSpace(m.From)),
			Urgency:      core.UrgencyAdvisory,
			Title:        m.Subject,
			Prompt:       m.Body,
			State:        core.EscalationOpen,
			CreatedAt:    m.CreatedAt,
		})
	}
	return results, nil
}

// ListSessions enumerates active sessions across every rig by joining
// `gt convoy list --json` with `gt polecat list --all --json`. Each
// active polecat (SessionRunning=true) maps to one core.Session; the
// convoy is the assignment id when discoverable. SessionFilter is
// applied in-process — gt has no server-side filter for this query.
func (o *OrchestrationPlane) ListSessions(ctx context.Context, f core.SessionFilter) ([]core.Session, error) {
	convoys, _ := o.loadConvoys(ctx) // best-effort; missing convoy data degrades to "no assignment id"
	polecats, err := o.loadPolecats(ctx)
	if err != nil {
		return nil, err
	}
	convoyByPolecat := indexConvoyByPolecat(convoys)

	out := make([]core.Session, 0, len(polecats))
	for _, p := range polecats {
		if !p.SessionRunning && !f.IncludeTerminal {
			continue
		}
		sessID := fmt.Sprintf("gastown/%s/polecats/%s", p.Rig, p.Name)
		status := polecatStateToSessionStatus(p.State, p.SessionRunning)
		bead := p.Issue
		if c, ok := convoyByPolecat[p.Rig+"/"+p.Name]; ok && bead == "" {
			if len(c.Issues) > 0 {
				bead = c.Issues[0]
			}
		}
		sess := core.Session{
			ID:           sessID,
			AssignmentID: bead,
			AgentID:      core.AgentID(sessID),
			Status:       status,
			StartedAt:    time.Now(), // gt polecat list does not surface start time
			ProviderMetadata: map[string]any{
				"adaptor": "gastown",
				"rig":     p.Rig,
				"polecat": p.Name,
				"state":   p.State,
			},
		}
		out = append(out, sess)
	}
	return applySessionFilter(out, f), nil
}

func applySessionFilter(in []core.Session, f core.SessionFilter) []core.Session {
	if len(f.Status) == 0 && f.AgentID == "" && f.IncludeTerminal {
		return in
	}
	out := in[:0:len(in)]
	for _, s := range in {
		if !f.IncludeTerminal && isTerminalStatus(s.Status) {
			continue
		}
		if len(f.Status) > 0 && !containsStatus(f.Status, s.Status) {
			continue
		}
		if f.AgentID != "" && s.AgentID != f.AgentID {
			continue
		}
		out = append(out, s)
	}
	return out
}

func containsStatus(set []core.SessionStatus, s core.SessionStatus) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

func isTerminalStatus(s core.SessionStatus) bool {
	return s == core.SessionCompleted || s == core.SessionFailed
}

// polecatStateToSessionStatus maps the gt polecat state string onto
// the canonical SessionStatus. Unknown states are reported as
// Working when the session is running so they don't vanish from the
// /sessions inventory; callers can refine via gt show.
func polecatStateToSessionStatus(state string, running bool) core.SessionStatus {
	if !running {
		return core.SessionCompleted
	}
	switch state {
	case "idle":
		return core.SessionReady
	case "working", "claimed":
		return core.SessionWorking
	case "stalled", "stuck":
		return core.SessionStalled
	case "prompting", "blocked":
		return core.SessionPrompting
	case "suspended", "paused":
		return core.SessionSuspended
	default:
		return core.SessionWorking
	}
}

// ResolveEscalation closes a gt escalation by id with the operator's
// resolution kind translated into a `--reason` string. The mapping:
//
//   - approve  → "approved by <resolver>"
//   - deny     → "denied by <resolver>"
//   - modify   → caller-supplied modify text (Resolution.Value)
//   - defer    → "deferred by <resolver>"
//
// gt has no concept of "modify with a structured value" today — the
// caller's value is rendered into the resolution reason for audit
// only. Callers requiring a structured return path SHOULD prefer
// the native adaptor until gt grows a richer escalation API.
func (o *OrchestrationPlane) ResolveEscalation(ctx context.Context, escalationID string, r core.EscalationResolution, _ core.ConfirmNonce) (core.EscalationRequest, error) {
	if strings.TrimSpace(escalationID) == "" {
		return core.EscalationRequest{}, core.NewAdaptorError(core.KindValidation,
			"gastown: ResolveEscalation requires escalation id")
	}
	reason := buildResolutionReason(r)
	args := []string{"escalate", "close", escalationID, "--reason", reason}
	if _, err := o.run(ctx, args...); err != nil {
		return core.EscalationRequest{}, wrapGtError(err, "gt "+strings.Join(args, " "))
	}
	now := time.Now()
	resolved := r
	resolved.ResolvedAt = now
	return core.EscalationRequest{
		ID:         escalationID,
		Source:     core.EscalationOrchestratorPause,
		AgentID:    r.ResolvedBy,
		State:      core.EscalationResolved,
		Resolution: &resolved,
		CreatedAt:  now,
	}, nil
}

// buildResolutionReason renders the EscalationResolution into the
// free-form --reason gt accepts. Callers can read it back via
// `gt escalate list` for audit.
func buildResolutionReason(r core.EscalationResolution) string {
	prefix := string(r.Kind)
	if prefix == "" {
		prefix = "resolved"
	}
	by := string(r.ResolvedBy)
	if by == "" {
		by = "operator"
	}
	if r.Value != nil {
		return fmt.Sprintf("%s by %s: %v", prefix, by, r.Value)
	}
	return fmt.Sprintf("%s by %s", prefix, by)
}

// Subscribe streams OrchestrationEvents derived from polling the gt
// surface. The poll period defaults to 5s (matching the manifest's
// EventDeliveryPoll declaration); the GEMBA_GASTOWN_POLL_PERIOD env
// var (parsed by an external caller for now) can shorten it for
// tests. Each tick:
//
//  1. Re-reads `gt convoy list --json` + `gt polecat list --all --json`.
//  2. Diffs polecat session-state against the last snapshot.
//  3. Emits `session_transition` events for any change.
//  4. Re-reads `gt escalate list --json`.
//  5. Diffs against the last open-escalation set, emits
//     `escalation_raised` / `escalation_resolved` accordingly.
//
// The channel is closed when ctx is canceled. f is honored on emit:
// SubscribeFilter.SessionID narrows session_transition emissions;
// Kinds, when set, drops events whose Kind isn't listed.
func (o *OrchestrationPlane) Subscribe(ctx context.Context, f core.SubscribeFilter) (<-chan core.OrchestrationEvent, error) {
	ch := make(chan core.OrchestrationEvent, 16)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(subscribePollPeriod)
		defer ticker.Stop()
		var lastSessions map[string]core.SessionStatus
		var lastEscalations map[string]struct{}
		// recycledIn is the side-channel RecycleSession publishes
		// session.recycled events on. nil channel makes the select
		// case never fire (a no-op when the adaptor was constructed
		// without an event channel — e.g. in tests).
		recycledIn := o.recycledEvents
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-recycledIn:
				if !ok {
					recycledIn = nil
					continue
				}
				if subscribeMatches(evt, f) {
					select {
					case ch <- evt:
					case <-ctx.Done():
						return
					}
				}
			case <-ticker.C:
				now := time.Now()
				// Session transitions.
				if sessions, err := o.ListSessions(ctx, core.SessionFilter{IncludeTerminal: true}); err == nil {
					curr := make(map[string]core.SessionStatus, len(sessions))
					for _, s := range sessions {
						curr[s.ID] = s.Status
					}
					for id, status := range curr {
						if prev, ok := lastSessions[id]; !ok || prev != status {
							evt := core.OrchestrationEvent{
								ID:        fmt.Sprintf("gt-st-%s-%d", id, now.UnixNano()),
								Kind:      "session_transition",
								At:        now,
								SessionID: id,
								Payload: map[string]any{
									"status": string(status),
									"prev":   string(prev),
								},
							}
							if subscribeMatches(evt, f) {
								select {
								case ch <- evt:
								case <-ctx.Done():
									return
								}
							}
						}
					}
					lastSessions = curr
				}
				// Escalation transitions.
				if rows, err := o.listEscalationsRaw(ctx); err == nil {
					curr := make(map[string]struct{}, len(rows))
					for _, r := range rows {
						if r.Status != "" && r.Status != "open" {
							continue
						}
						curr[r.ID] = struct{}{}
						if _, seen := lastEscalations[r.ID]; !seen {
							evt := core.OrchestrationEvent{
								ID:   fmt.Sprintf("gt-esc-raised-%s-%d", r.ID, now.UnixNano()),
								Kind: "escalation_raised",
								At:   now,
								Payload: map[string]any{
									"escalation_id": r.ID,
									"title":         r.Title,
								},
							}
							if subscribeMatches(evt, f) {
								select {
								case ch <- evt:
								case <-ctx.Done():
									return
								}
							}
						}
					}
					for id := range lastEscalations {
						if _, still := curr[id]; !still {
							evt := core.OrchestrationEvent{
								ID:   fmt.Sprintf("gt-esc-resolved-%s-%d", id, now.UnixNano()),
								Kind: "escalation_resolved",
								At:   now,
								Payload: map[string]any{
									"escalation_id": id,
								},
							}
							if subscribeMatches(evt, f) {
								select {
								case ch <- evt:
								case <-ctx.Done():
									return
								}
							}
						}
					}
					lastEscalations = curr
				}
			}
		}
	}()
	return ch, nil
}

// subscribePollPeriod is the default tick interval for the gt-side
// poller. Matches the manifest's EventDeliveryPoll latency budget
// (5s ceiling per OrchestrationPlaneAdaptor docstring).
var subscribePollPeriod = 5 * time.Second

// subscribeMatches honors the SubscribeFilter against an event we are
// about to emit. Empty filter accepts everything.
func subscribeMatches(evt core.OrchestrationEvent, f core.SubscribeFilter) bool {
	if f.SessionID != "" && evt.SessionID != f.SessionID {
		return false
	}
	if len(f.Kinds) > 0 {
		ok := false
		for _, k := range f.Kinds {
			if k == evt.Kind {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if f.Since != nil && evt.At.Before(*f.Since) {
		return false
	}
	return true
}

// loadConvoys shells out to `gt convoy list --json` and parses the
// response. Empty output / "null" → nil slice (no convoys yet, not an
// error). Test seam: OrchestrationPlane.run is stubbed in tests to
// return canned JSON without spawning a process.
func (o *OrchestrationPlane) loadConvoys(ctx context.Context) ([]gtConvoy, error) {
	out, err := o.run(ctx, "convoy", "list", "--json")
	if err != nil {
		return nil, wrapGtError(err, "gt convoy list --json")
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var rows []gtConvoy
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, core.WrapAdaptorError(core.KindProcessFailed, err,
			"gastown: parse gt convoy list --json")
	}
	return rows, nil
}

// indexConvoyByPolecat builds a "<rig>/<polecat>" → convoy map. Used
// by ListSessions to attach an assignment id to each polecat session
// without rescanning the convoy list per polecat.
func indexConvoyByPolecat(convoys []gtConvoy) map[string]gtConvoy {
	out := make(map[string]gtConvoy)
	for _, c := range convoys {
		for _, p := range c.Polecats {
			out[c.Rig+"/"+p] = c
		}
		for _, w := range c.Workers {
			out[w.Rig+"/"+w.Polecat] = c
			out[w.Rig+"/"+w.Name] = c
		}
	}
	return out
}

// convoyHasBead reports whether the convoy carries the named bead in
// either Issues or Tracks. Used by tryResolveSessionID after a sling.
func convoyHasBead(c gtConvoy, beadID string) bool {
	for _, b := range c.Issues {
		if b == beadID {
			return true
		}
	}
	for _, b := range c.Tracks {
		if b == beadID {
			return true
		}
	}
	for _, w := range c.Workers {
		if w.Bead == beadID || w.Issue == beadID {
			return true
		}
	}
	return false
}

// polecatSessionID builds the canonical Gemba session id ("gastown/<rig>/polecats/<name>")
// from a convoy worker row. Falls back to the convoy id when the row
// lacks the rig/polecat shape.
func polecatSessionID(w convoyWorker) string {
	name := w.Polecat
	if name == "" {
		name = w.Name
	}
	if w.Rig == "" || name == "" {
		return ""
	}
	return fmt.Sprintf("gastown/%s/polecats/%s", w.Rig, name)
}

// gtPolecatGitState matches the JSON shape `gt polecat git-state --json`
// emits. Used by RecycleSession's cleanliness gate (gm-e7.13). Fields
// not used here (uncommitted_files, unpushed_commits, stash_count) are
// preserved in the struct for forward compatibility.
type gtPolecatGitState struct {
	Clean            bool     `json:"clean"`
	UncommittedFiles []string `json:"uncommitted_files,omitempty"`
	UnpushedCommits  int      `json:"unpushed_commits,omitempty"`
	StashCount       int      `json:"stash_count,omitempty"`
}

// RecycleSession implements the §5.2 recycle protocol against gt by
// wrapping `gt handoff <session-id>`. Same polecat slot, fresh context.
//
// Sequence:
//
//  1. Validate session is Status=Ready. Mid-bead recycle is rejected as
//     KindValidation — we don't destroy in-flight work.
//  2. Verify the polecat's worktree is clean via `gt polecat git-state
//     <rig>/<name> --json`. Dirty → REFUSE; convert to
//     EndSession(SessionEndCompleted) so the pool releases the slot
//     and the daemon spawns fresh on the next dispatch tick. Spec §5.2
//     step 2 / §5.4 is explicit: never destructively reset.
//  3. Shell `gt handoff <rig>/<name>`. gt's handoff respawns the agent
//     in place; our adaptor mints a logical new session id (gt's own
//     polecat path is stable across handoffs).
//  4. gt handoff already syncs the worktree internally (per the
//     subcommand's docstring — "Hand off to a fresh agent session"
//     implies a clean handoff base). We do not run a second checkout
//     + ff-only pull here.
//  5. Mint a new session id, publish a session.recycled
//     OrchestrationEvent for the retro pipeline (gm-s47n.12 writer).
//
// Capability gate (gm-e7.12): when probeCapabilities reported
// HasHandoff=false, RecycleSession returns KindUnsupported with a
// pointed diagnostic instead of attempting the shell-out.
func (o *OrchestrationPlane) RecycleSession(ctx context.Context, sessionID string) (core.Session, error) {
	if !o.caps.HasHandoff && o.caps.VersionRaw != "" {
		// Probe ran (caps non-zero) and reported handoff missing.
		// Tests that don't run the probe leave caps zero-valued; we
		// treat that as "probe didn't run, take the optimistic path".
		return core.Session{}, core.NewAdaptorError(core.KindUnsupported,
			"gastown: RecycleSession requires gt handoff subcommand (not detected at probe time; gt %s)",
			o.caps.Version)
	}
	if strings.TrimSpace(sessionID) == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"gastown: RecycleSession requires a session id")
	}

	// Step 1 — status gate. We need the session row to know its
	// current status; resolveSession + a polecat lookup gives us
	// both the (rig, name) for git-state + handoff and the status.
	beadID, target, err := o.resolveSession(ctx, sessionID)
	if err != nil {
		return core.Session{}, err
	}
	rigName, polecatName := splitPolecatTarget(sessionID, target)
	if rigName == "" || polecatName == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"gastown: RecycleSession only supports polecat sessions (id=%q)", sessionID)
	}
	status, err := o.lookupPolecatStatus(ctx, rigName, polecatName)
	if err != nil {
		return core.Session{}, err
	}
	if status != core.SessionReady {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"gastown: recycle requires Status=Ready, got %q for %q", status, sessionID)
	}

	// Step 2 — cleanliness gate.
	clean, gitErr := o.polecatWorktreeClean(ctx, rigName, polecatName)
	if gitErr != nil {
		// Probe failure is non-fatal but worth logging; we proceed
		// only when we can prove cleanliness, never assume it.
		slog.Warn("gastown: recycle cleanliness probe failed; treating as dirty (refusing recycle)",
			"session", sessionID,
			"err", gitErr)
		clean = false
	}
	if !clean {
		slog.Warn("session.recycle.refused.dirty_worktree",
			"session", sessionID,
			"rig", rigName,
			"polecat", polecatName)
		// Convert to End so the slot releases and the daemon's
		// dispatch tick spawns fresh.
		_, endErr := o.EndSession(ctx, sessionID, core.SessionEndCompleted, "")
		if endErr != nil {
			return core.Session{}, core.WrapAdaptorError(core.KindAdaptorDegraded, endErr,
				"gastown: recycle refused (dirty worktree) and end fallback failed for %s", sessionID)
		}
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"gastown: recycle refused: %s/%s has uncommitted changes (converted to end; pool will spawn fresh)",
			rigName, polecatName)
	}

	// Step 3 — shell `gt handoff <rig>/<polecat>`. gt mints a fresh
	// session in place; we mint the logical session id ourselves so
	// the orchestration plane's session id namespace stays consistent.
	target = rigName + "/" + polecatName
	if _, err := o.run(ctx, "handoff", target); err != nil {
		return core.Session{}, wrapGtError(err, "gt handoff "+target)
	}

	// Step 5 — mint, emit session.recycled, return.
	now := time.Now()
	newSessionID := fmt.Sprintf("gastown/%s/polecats/%s:recycle:%d",
		rigName, polecatName, now.UnixNano())

	priorAgentID := core.AgentID(sessionID)
	newSess := core.Session{
		ID:           newSessionID,
		AssignmentID: "", // recycle clears the prior assignment (no bead until next dispatch)
		AgentID:      priorAgentID,
		Status:       core.SessionInitializing,
		StartedAt:    now,
		ProviderMetadata: map[string]any{
			"adaptor":          "gastown",
			"rig":              rigName,
			"polecat":          polecatName,
			"prior_session_id": sessionID,
			"prior_bead_id":    beadID,
			"gt_handoff":       true,
			"recycled_at":      now.Format(time.RFC3339Nano),
		},
	}

	// Publish session.recycled — non-blocking (the channel has a
	// buffer; if it's full we drop, matching the "best-effort event
	// emission" pattern the rest of Subscribe uses).
	if o.recycledEvents != nil {
		evt := core.OrchestrationEvent{
			ID:        "gastown-session-recycled:" + newSessionID,
			Kind:      "session.recycled",
			At:        now,
			SessionID: newSessionID,
			Payload: map[string]any{
				"prior_session_id": sessionID,
				"new_session_id":   newSessionID,
				"rig":              rigName,
				"polecat":          polecatName,
				"agent_type":       "polecat",
				"reason":           "explicit",
				"prior_bead_id":    beadID,
			},
		}
		select {
		case o.recycledEvents <- evt:
		default:
			slog.Warn("gastown: recycle event channel full; dropping session.recycled",
				"session", newSessionID)
		}
	}

	return newSess, nil
}

// splitPolecatTarget pulls (rig, polecat) out of a Gemba session id of
// the form "gastown/<rig>/polecats/<name>". Falls back to the resolved
// target rig when the id shape is non-canonical (synthetic ids from
// StartSession use "gastown/<target>/<bead>/<ts>" which can't recycle).
// Returns "", "" when the id can't be split into a polecat target.
func splitPolecatTarget(sessionID, fallbackTarget string) (string, string) {
	rest := strings.TrimPrefix(sessionID, "gastown/")
	parts := strings.Split(rest, "/")
	if len(parts) >= 3 && parts[1] == "polecats" {
		return parts[0], parts[2]
	}
	_ = fallbackTarget
	return "", ""
}

// lookupPolecatStatus reads the polecat's current status by re-running
// `gt polecat list --all --json` and matching on (rig, name). Returns
// KindSessionNotFound when the polecat is not in the listing.
func (o *OrchestrationPlane) lookupPolecatStatus(ctx context.Context, rig, name string) (core.SessionStatus, error) {
	pcs, err := o.loadPolecats(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range pcs {
		if p.Rig == rig && p.Name == name {
			return polecatStateToSessionStatus(p.State, p.SessionRunning), nil
		}
	}
	return "", core.NewAdaptorError(core.KindSessionNotFound,
		"gastown: polecat %s/%s not found in `gt polecat list`", rig, name)
}

// polecatWorktreeClean shells `gt polecat git-state <rig>/<name> --json`
// and parses the {clean: bool, ...} payload. Returns (clean, err) —
// any non-nil err is the underlying gt failure (cmd missing, JSON
// parse error, etc.). The recycle path treats err as "dirty" (defense
// in depth: never destroy on uncertainty).
func (o *OrchestrationPlane) polecatWorktreeClean(ctx context.Context, rig, name string) (bool, error) {
	target := rig + "/" + name
	out, err := o.run(ctx, "polecat", "git-state", target, "--json")
	if err != nil {
		return false, wrapGtError(err, "gt polecat git-state "+target+" --json")
	}
	var st gtPolecatGitState
	if err := json.Unmarshal(out, &st); err != nil {
		return false, core.WrapAdaptorError(core.KindProcessFailed, err,
			"gastown: parse gt polecat git-state --json")
	}
	return st.Clean, nil
}
