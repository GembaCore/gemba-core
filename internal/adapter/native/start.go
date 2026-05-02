package native

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/agents"
	"github.com/GembaCore/gemba-core/internal/adapter/native/backend"
	"github.com/GembaCore/gemba-core/internal/adapter/native/install"
	"github.com/GembaCore/gemba-core/internal/adapter/native/preamble"
	"github.com/GembaCore/gemba-core/internal/adapter/native/worktrees"
	coreprompt "github.com/GembaCore/gemba-core/internal/core/prompt"
	"github.com/GembaCore/gemba-core/internal/milestones"
	"github.com/GembaCore/gemba-core/internal/persona"
)

// Keys the SessionPrompt.Extension map uses to carry Gemba-specific
// dispatch context. Operators would never see these strings directly
// — the server builds the map when translating an SPA "start session"
// click into a StartSession call.
const (
	extKeyBeadID    = "gemba:bead_id"
	extKeyAgentType = "gemba:agent_type"
	extKeyWorkspace = "gemba:workspace"
	extKeyNonce     = "gemba:nonce"
	// extKeyTitle carries the operator-visible pane title. Optional;
	// when unset the backend falls back to its default.
	extKeyTitle = "gemba:title"
	// extKeySurface carries a *persona.Surface materialized by the
	// dispatcher / polecat scheduler (gm-utik). When present it is
	// translated into the container backend's volume-mount list — the
	// strongest enforcement layer of the cwd-constraint contract. When
	// absent the backend falls back to the legacy operator-mounts-only
	// behavior so terminal backends and pre-surface dispatch paths
	// keep working.
	extKeySurface = "gemba:surface"
	// extKeySessionIDOverride lets a caller pin the spawned session's
	// id (gm-twp2). Persona consults pass consult.ID here so the
	// bridge frames the spawned agent emits carry a session id that
	// the persona dispatcher's Receive call can correlate against
	// without a separate session→consult lookup table. When absent
	// (the common case for bead dispatch), the auto-generated
	// "<backend>:<bead>:<nanos>" id is used unchanged.
	extKeySessionIDOverride = "gemba:session_id_override"
	// extKeyKind picks between bead-driven and manual session shapes
	// (gm-hmqj). Empty or "bead" → the historical bead-driven path
	// (requires extKeyBeadID). "manual" → free-form prompt against a
	// chosen repository, no work-item required.
	extKeyKind = "gemba:kind"
	// extKeyRepositoryID names the repository the manual session
	// works in. Carried for audit/UI; today the adaptor uses it as a
	// label and falls back to o.cfg.RepoRoot when no explicit
	// gemba:workspace path is set.
	extKeyRepositoryID = "gemba:repository_id"
	// extKeyPersonaID names an optional persona ride-along for manual
	// sessions ("coach" / "manager" / etc.). Surfaced into the pane
	// title and ProviderMetadata; preamble composition stays prompt-
	// text-driven for now.
	extKeyPersonaID = "gemba:persona_id"
)

// kindManual is the value of extKeyKind that selects manual-mode
// dispatch — no work-item, free-form prompt against a repository.
const kindManual = "manual"

// StartSession implements core.OrchestrationPlaneAdaptor for the
// native adaptor. gm-native.9.
//
// The assignmentID field on the call is carried through as a stable
// handle for the server-side bookkeeping, but the work-item (bead)
// id is what the adaptor actually provisions around — it's required
// in the SessionPrompt.Extension map under "gemba:bead_id".
//
// Nonces (extension["gemba:nonce"]) make the call idempotent: a
// replayed same-nonce StartSession returns the cached Session
// verbatim without re-spawning the pane. Missing nonce means the
// server isn't gating the call and we proceed unconditionally —
// the serve.go router should always set one, but tests can skip it.
func (o *OrchestrationPlane) StartSession(ctx context.Context, assignmentID string, prompt core.SessionPrompt) (core.Session, error) {
	if o.cfg.Backend == nil {
		return core.Session{}, unsupported("StartSession")
	}
	if assignmentID == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: StartSession requires assignment id")
	}

	// gm-hmqj: two dispatch shapes share this entry point. "bead" is
	// the historical path — every session anchors on a work-item the
	// caller names via extKeyBeadID. "manual" lets the operator dispatch
	// against a chosen repository without first filing a bead.
	kind, _ := prompt.Extension[extKeyKind].(string)
	manualMode := kind == kindManual

	beadID, _ := prompt.Extension[extKeyBeadID].(string)
	if !manualMode && beadID == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: SessionPrompt.Extension[%q] is required (or set %q=%q)",
			extKeyBeadID, extKeyKind, kindManual)
	}
	repositoryID, _ := prompt.Extension[extKeyRepositoryID].(string)
	if manualMode && repositoryID == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: manual sessions require %q", extKeyRepositoryID)
	}
	agentType, _ := prompt.Extension[extKeyAgentType].(string)
	if agentType == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: SessionPrompt.Extension[%q] is required", extKeyAgentType)
	}
	agent, ok := o.cfg.Registry.Get(agentType)
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: agent type %q not registered in .gemba/agents.toml (available: %v)",
			agentType, o.cfg.Registry.Names())
	}

	// Nonce-cache first — same-nonce replays return the cached
	// session without touching the backend again.
	nonce, _ := prompt.Extension[extKeyNonce].(string)
	if nonce != "" {
		if sid, prior := o.lookupNonce(nonce); prior {
			if cached := o.readSession(sid); cached != nil {
				return *cached, nil
			}
		}
	}

	// Reuse-pane branch (gm-root.16.3): when the caller hands us a
	// pane_id from a running same-agent-type session, skip worktree
	// provisioning, bridge install, and SpawnPane — the existing pane
	// already has all three. Validate cap before doing any work so a
	// rejected reuse claim doesn't allocate resources.
	reusePaneID, _ := prompt.Extension[extKeyReusePaneID].(string)
	maxParallel := agent.ResolvedMaxParallel()

	// Session id keys off the bead in bead-mode and the assignment id in
	// manual-mode. Both are operator-visible so the dispatcher's
	// /sessions list reads cleanly either way.
	sessionAnchor := beadID
	if manualMode {
		sessionAnchor = assignmentID
	}
	sessionID := fmt.Sprintf("%s:%s:%d", o.cfg.Backend.Name(), sessionAnchor, time.Now().UnixNano())
	if override, _ := prompt.Extension[extKeySessionIDOverride].(string); override != "" {
		sessionID = override
	}

	title, _ := prompt.Extension[extKeyTitle].(string)
	if title == "" {
		if manualMode {
			personaID, _ := prompt.Extension[extKeyPersonaID].(string)
			if personaID != "" {
				title = fmt.Sprintf("gemba: manual (%s · %s)", personaID, repositoryID)
			} else {
				title = fmt.Sprintf("gemba: manual (%s)", repositoryID)
			}
		} else {
			title = "gemba: " + beadID
		}
	}
	surface, _ := prompt.Extension[extKeySurface].(*persona.Surface)

	var (
		pane                   backend.Pane
		workspace              string
		codexPromptFile        string
		retiredReuseSessionIDs []string
	)
	if reusePaneID != "" {
		o.mu.Lock()
		paneSessionIDs := append([]string(nil), o.paneSessions[reusePaneID]...)
		if len(paneSessionIDs) == 0 {
			o.mu.Unlock()
			return core.Session{}, core.NewAdaptorError(core.KindValidation,
				"native: reuse_pane_id %q has no active session", reusePaneID)
		}
		anchor := o.sessions[paneSessionIDs[0]]
		if anchor == nil {
			o.mu.Unlock()
			return core.Session{}, core.NewAdaptorError(core.KindAdaptorDegraded,
				"native: pane %s has no anchor session record", reusePaneID)
		}
		anchorAgentType, _ := anchor.ProviderMetadata["agent_type"].(string)
		if anchorAgentType != agentType {
			o.mu.Unlock()
			return core.Session{}, core.NewAdaptorError(core.KindValidation,
				"native: pane %s runs agent %q, cannot reuse for %q",
				reusePaneID, anchorAgentType, agentType)
		}
		anchorWorkspace, _ := anchor.ProviderMetadata["worktree"].(string)
		anchorTitle, _ := anchor.ProviderMetadata["pane_title"].(string)
		anchorCwd, _ := anchor.ProviderMetadata["pane_cwd"].(string)
		retiredReuseSessionIDs = o.retireReadySessionsForPaneLocked(reusePaneID)
		inFlight := o.paneInFlightLocked(reusePaneID)
		if inFlight >= maxParallel {
			o.mu.Unlock()
			return core.Session{}, core.NewAdaptorError(core.KindValidation,
				"native: pane %s at cap (%d/%d for agent %q)",
				reusePaneID, inFlight, maxParallel, agentType)
		}
		o.mu.Unlock()
		workspace = anchorWorkspace
		pane = backend.Pane{ID: reusePaneID, Title: anchorTitle, Cwd: anchorCwd}
	} else {
		// Provision worktree. If a workspace was pre-resolved by the
		// caller (operator pre-selected an existing worktree), honor it
		// instead of auto-creating. Manual sessions (gm-hmqj) don't
		// branch off a bead — they run in the chosen repository's
		// canonical worktree; fall back to o.cfg.RepoRoot when the
		// caller didn't pin a path.
		workspace, _ = prompt.Extension[extKeyWorkspace].(string)
		if workspace == "" {
			if manualMode {
				workspace = o.cfg.RepoRoot
			} else {
				var err error
				workspace, err = worktrees.Resolve(ctx, worktrees.Config{
					RepoRoot:     o.cfg.RepoRoot,
					WorktreesDir: o.cfg.WorktreesDir,
				}, beadID)
				if err != nil {
					return core.Session{}, core.WrapAdaptorError(core.KindProcessFailed, err,
						"native: provision worktree for %s", beadID)
				}
			}
		}

		if agent.Preamble == agents.PreambleCodexExec && !manualMode {
			text, err := o.composeBeadPreamble(ctx, beadID, workspace, agent, surface)
			if err != nil {
				return core.Session{}, err
			}
			text = withCodexMCPInstructions(text, beadID)
			codexPromptFile, err = writeCodexPromptFile(sessionID, text)
			if err != nil {
				return core.Session{}, core.WrapAdaptorError(core.KindProcessFailed, err,
					"native: write codex prompt for %s", beadID)
			}
		}

		// Best-effort bridge install — the per-agent installer is
		// idempotent; a populated worktree is a supported no-op.
		_ = installBridgeForAgent(ctx, workspace, agent)

		spec, err := buildSpawnSpec(agent, workspace, sessionID, agentType, title, surface, beadID)
		if err != nil {
			return core.Session{}, core.WrapAdaptorError(core.KindValidation, err,
				"native: build spawn spec for %s", sessionAnchor)
		}
		if codexPromptFile != "" {
			spec.Env["GEMBA_CODEX_PROMPT_FILE"] = codexPromptFile
		}

		// Layer 2 producer (gm-v8vr.1): persist the resolved surface so
		// the gemba-bridge PreToolUse hook (gm-eazw) reads it instead of
		// falling back to the deny-outside-cwd default. Best-effort — a
		// write failure logs (TODO when slog lands here) but doesn't
		// abort the session; the bridge degrades to the safe default.
		if surface != nil {
			_ = persona.WriteSurfaceFile(sessionID, *surface)
		}

		pane, err = o.cfg.Backend.SpawnPane(ctx, spec)
		if err != nil {
			return core.Session{}, core.WrapAdaptorError(core.KindProcessFailed, err,
				"native: spawn pane for %s (agent=%s, backend=%s)",
				sessionAnchor, agentType, o.cfg.Backend.Name())
		}
	}

	now := time.Now()
	personaID, _ := prompt.Extension[extKeyPersonaID].(string)
	sess := &core.Session{
		ID:           sessionID,
		AssignmentID: assignmentID,
		// Initial status is Initializing — the preamble + bridge install
		// are still in flight. The agent transitions to Ready (idle) or
		// Working (bead dispatched) when the first gemba-state signal
		// lands via the bridge (gm-cdph).
		Status:    core.SessionInitializing,
		StartedAt: now,
		// Persona binds the session to its pool (session-pool.md §3.2).
		// Empty when the dispatcher didn't pin one — manual flow falls
		// back here. The daemon's IdleSessionLister filters by
		// (rig, persona) so this field is load-bearing for pool
		// membership.
		Persona: personaID,
		ProviderMetadata: map[string]any{
			"pane_id":    pane.ID,
			"backend":    o.cfg.Backend.Name(),
			"agent_type": agentType,
			"bead_id":    beadID,
			"worktree":   workspace,
			"pane_title": pane.Title,
			"pane_cwd":   pane.Cwd,
			"pane_pid":   pane.Pid,
			"started_at": now.Format(time.RFC3339Nano),
			"reuse_pane": reusePaneID != "",
		},
	}
	if personaID != "" {
		sess.ProviderMetadata["persona_id"] = personaID
	}
	if manualMode {
		sess.ProviderMetadata["kind"] = kindManual
		sess.ProviderMetadata["repository_id"] = repositoryID
		if personaID, _ := prompt.Extension[extKeyPersonaID].(string); personaID != "" {
			sess.ProviderMetadata["persona_id"] = personaID
		}
	}

	// Record session + pane + nonce under the lock.
	o.mu.Lock()
	// Bead-double-claim guard (gm-e3.8). A bead must not be the active
	// AssignmentID of two non-terminal sessions at once — that's the
	// inline-claim race the manifest's ClaimModelInline contract
	// protects against. Surface the typed sentinel so the
	// auto-dispatch daemon's soft-skip path picks the next candidate
	// rather than failing the tick. Manual-mode sessions are exempt
	// (no bead anchor; assignmentID names the manual ticket itself).
	if !manualMode && beadID != "" && o.beadAlreadyInFlightLocked(beadID, sessionID) {
		o.mu.Unlock()
		// We have not yet recorded the new session, but we DID spawn
		// (or reuse) a pane above. For a fresh-pane spawn, kill the
		// dangling pane so we don't leak a hot agent on a bead it can
		// never claim. Reuse-pane mode shares the pane already, so we
		// leave it alone — the existing session continues unaffected.
		if reusePaneID == "" {
			_ = o.cfg.Backend.Kill(ctx, pane.ID)
		}
		return core.Session{}, core.NewAlreadyClaimedError(
			"native: bead %q already in flight on another session", beadID)
	}
	if reusePaneID == "" && o.paneInFlightLocked(pane.ID) > 0 {
		// Brand-new pane that already has sessions — backend bug. Clean
		// up to avoid the leak.
		o.mu.Unlock()
		_ = o.cfg.Backend.Kill(ctx, pane.ID)
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: pane %s already tracked (backend reused id?)", pane.ID)
	}
	if reusePaneID != "" && o.paneInFlightLocked(pane.ID) >= maxParallel {
		// Race: another reuse claim filled the pane between the pre-check
		// and the lock. Refuse rather than overflow.
		o.mu.Unlock()
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: pane %s reached cap during dispatch", pane.ID)
	}
	o.sessions[sessionID] = sess
	o.addSessionToPaneLocked(pane.ID, sessionID)
	inFlight := o.paneInFlightLocked(pane.ID)
	if nonce != "" {
		o.nonces[nonce] = sessionID
	}
	o.mu.Unlock()
	for _, id := range retiredReuseSessionIDs {
		o.fanout.Unregister(id)
	}

	// Surface the parallelism transition so SPA pills + the global
	// counter update without polling. Skipped on zero-delta no-ops by
	// construction (this path always advances by +1).
	o.fanout.Publish(parallelChanged(pane.ID, sessionID, agentType, inFlight, maxParallel, +1))

	// Register a bridge tailer for this session so Subscribe()
	// consumers see its events.
	if err := o.fanout.Register(context.Background(), sessionID, agentType); err != nil {
		// Tailer registration failure is not fatal to StartSession —
		// we log and continue. The SPA can still observe the pane via
		// ListAgents + PeekSession. Without the tailer the drawer
		// won't get structured events though.
		_ = err // TODO surface via slog when structured logger lands here
	}

	// Preamble injection. Two shapes:
	//   - Bead mode (gm-native.10): fetch the bead via WorkPlane, compose
	//     project + workspace + bead context, apply per agent type's
	//     strategy (CLAUDE.md, first-message, or stdout banner).
	//   - Manual mode (gm-hmqj): no work-item exists — deliver
	//     prompt.Text directly as the first message. Skip the
	//     WorkItem-shaped preamble composer entirely.
	// Everything is best-effort — a failed preamble must not abort a
	// live session. If WorkPlane is nil (tests, degraded mode) we skip
	// the bead-mode branch.
	if manualMode {
		if prompt.Text != "" {
			go o.deliverFirstMessage(ctx, pane.ID, prompt.Text)
		}
	} else if o.cfg.WorkPlane != nil && agent.Preamble != agents.PreambleCodexExec {
		if text, err := o.composeBeadPreamble(ctx, beadID, workspace, agent, surface); err == nil {
			text = withSessionLifecycleEnv(text, sessionID, agentType, beadID)
			if strat, err := preamble.Apply(workspace, agent, coreprompt.Composed{Text: text}); err == nil && strat.FirstMessage != "" {
				if agent.Preamble == agents.PreambleClaudeMD {
					strat.FirstMessage = fmt.Sprintf("New assigned bead: %s. CLAUDE.md has the current bead context and lifecycle commands at the bottom. Read the latest CLAUDE.md task and begin.", beadID)
				}
				go o.deliverFirstMessage(ctx, pane.ID, strat.FirstMessage)
			}
		}
	}

	return *sess, nil
}

// retireReadySessionsForPaneLocked marks previously-idle records on a
// reused pane terminal before assigning the next bead. The Claude/Codex
// process in that pane cannot receive a fresh GEMBA_SESSION_ID through
// its original environment, so each bead gets a new session row while
// the old ready rows stop counting against intra_parallel capacity.
func (o *OrchestrationPlane) retireReadySessionsForPaneLocked(paneID string) []string {
	ids := o.paneSessions[paneID]
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	retired := make([]string, 0, len(ids))
	kept := ids[:0]
	for _, id := range ids {
		sess := o.sessions[id]
		if sess == nil {
			continue
		}
		if sess.Status == core.SessionReady {
			sess.Status = core.SessionCompleted
			sess.EndedAt = &now
			retired = append(retired, id)
			continue
		}
		kept = append(kept, id)
	}
	if len(kept) == 0 {
		delete(o.paneSessions, paneID)
	} else {
		o.paneSessions[paneID] = kept
	}
	return retired
}

func withSessionLifecycleEnv(text, sessionID, agentType, beadID string) string {
	if text == "" || beadID == "" || sessionID == "" {
		return text
	}
	prefix := fmt.Sprintf("GEMBA_SESSION_ID=%s GEMBA_AGENT_TYPE=%s ",
		shellSingleQuote(sessionID), shellSingleQuote(agentType))
	working := fmt.Sprintf("${GEMBA_STATE_COMMAND:-gemba-state} -bead %s working", beadID)
	done := fmt.Sprintf("${GEMBA_STATE_COMMAND:-gemba-state} -bead %s bead-done", beadID)
	text = strings.ReplaceAll(text, working, prefix+working)
	text = strings.ReplaceAll(text, done, prefix+done)
	return text
}

func withCodexMCPInstructions(text, beadID string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(text, "\n"))
	b.WriteString("\n\n## Codex Gemba MCP tools\n\n")
	b.WriteString("This Codex session is launched with a session-scoped MCP server named `gemba`. Prefer these MCP tools over shelling out to `gemba-state` or `gemba-ask` when they are available.\n\n")
	b.WriteString("- Call `report_state` with `state=\"working\"` and this bead id when you begin substantive work.\n")
	b.WriteString("- Call `report_state` with `state=\"prompting\"` before raising an operator question, and use `ask_question` for non-blocking judgment calls.\n")
	b.WriteString("- Use `raise_blocker` only when progress must stop until the operator responds.\n")
	b.WriteString("- Use `emit_skill_output` for structured persona or skill results instead of writing ad hoc transcript sections.\n")
	b.WriteString("- Call `report_state` with `state=\"stalled\"` if execution fails or you cannot proceed.\n")
	b.WriteString("- Only call `report_state` with `state=\"bead-done\"` after the bead is closed and the git worktree is clean. The driver also emits a fallback `bead-done` after successful `codex exec`, bead close, and commit.\n\n")
	if beadID != "" {
		fmt.Fprintf(&b, "Current bead id: `%s`.\n", beadID)
	}
	return b.String()
}

func shellSingleQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (o *OrchestrationPlane) composeBeadPreamble(
	ctx context.Context,
	beadID string,
	workspace string,
	agent agents.AgentType,
	surface *persona.Surface,
) (string, error) {
	if o.cfg.WorkPlane == nil {
		return fmt.Sprintf("# Assigned bead: %s\n\nWork this bead. Push when the DoD is met. Escalate if blocked.\n", beadID), nil
	}
	item, err := o.cfg.WorkPlane.GetWorkItem(ctx, core.WorkItemID(beadID))
	if err != nil {
		return "", core.WrapAdaptorError(core.KindProcessFailed, err,
			"native: load work item for preamble %s", beadID)
	}
	// Selective milestone context (gm-yyo8): walk parent_child up
	// looking for a milestone ancestor. Errors degrade to "no milestone
	// block" — we'd rather ship an unbound preamble than fail the
	// session start over a transport hiccup.
	milestone, _ := milestones.FindMilestoneAncestor(ctx, o.cfg.WorkPlane, item)
	composed := preamble.Build(preamble.Sources{
		RepoRoot:               o.cfg.RepoRoot,
		WorkspaceDir:           workspace,
		InteractionProfilePath: preamble.ResolveProfilePath(o.cfg.RepoRoot, agent.InteractionProfile),
		InteractionMode:        agent.ResolvedInteractionMode(),
		Surface:                surface,
		SurfaceEnv:             persona.EnvFromOS(os.Getenv),
		Repository:             surfaceRepoLabel(item),
		Branch:                 surfaceBranchLabel(item),
		Milestone:              milestone,
	}, item)
	return composed.Text, nil
}

func writeCodexPromptFile(sessionID, text string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "gemba", "codex-prompts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := safePromptFilename(sessionID) + ".md"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func safePromptFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "session"
	}
	return b.String()
}

// surfaceRepoLabel resolves the operator-visible repository name for
// the preamble's "Repository:" line (gm-r5vz). Returns "" when the
// bead has no primary repository set; the renderer surfaces that as
// the "(workspace root, no single repo)" placeholder.
func surfaceRepoLabel(item core.WorkItem) string {
	if item.PrimaryRepositoryID == "" || item.PrimaryRepositoryID == core.RepositoryUnspecified {
		return ""
	}
	return string(item.PrimaryRepositoryID)
}

// surfaceBranchLabel returns the branch name the spawned session is
// working on, picked out of the bead's per-repo branch map. Empty
// return is rendered as "no branch — read-only consult" upstream.
func surfaceBranchLabel(item core.WorkItem) string {
	if item.PrimaryRepositoryID == "" {
		return ""
	}
	for _, b := range item.Branches {
		if b.RepositoryID == item.PrimaryRepositoryID {
			return b.Branch
		}
	}
	return ""
}

// firstMessageBootDelay is how long we wait after spawning the agent
// pane before injecting the preamble keystrokes. Modern Claude Code can
// spend several seconds loading plugins, update notices, and auth-backed
// state before the input prompt is ready; twenty seconds avoids swallowing the first
// message during the splash screen. gm-4jsi.
const firstMessageBootDelay = 20 * time.Second

// firstMessageSubmitDelay is the gap between typing the preamble text
// and pressing Enter. Splitting the two SendKeys calls (rather than
// passing "<text> Enter" in one shot) gives Claude a moment to ingest
// the typed bytes and exit any interstitial banner state before the
// submit lands. gm-4jsi.
const firstMessageSubmitDelay = 500 * time.Millisecond

// deliverFirstMessage injects the preamble notification + Enter as
// two distinct SendKeys calls separated by short delays. Runs in its
// own goroutine so StartSession returns immediately; the operator sees
// the spawn complete while the agent finishes booting.
//
// Context detached from the request: a session that survives the HTTP
// request's lifetime should still finish its first turn.
func (o *OrchestrationPlane) deliverFirstMessage(_ context.Context, paneID, text string) {
	ctx := context.Background()
	time.Sleep(firstMessageBootDelay)
	if err := o.cfg.Backend.SendKeys(ctx, paneID, text); err != nil {
		return
	}
	time.Sleep(firstMessageSubmitDelay)
	_ = o.cfg.Backend.SendKeys(ctx, paneID, "Enter")
	time.Sleep(firstMessageSubmitDelay)
	_ = o.cfg.Backend.SendKeys(ctx, paneID, "Enter")
}

// lookupNonce reads the nonce table under the adaptor's lock.
func (o *OrchestrationPlane) lookupNonce(nonce string) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	sid, ok := o.nonces[nonce]
	return sid, ok
}

// readSession returns a copy of the session for the given id, or
// nil. Copies so callers can't mutate adaptor state.
func (o *OrchestrationPlane) readSession(sessionID string) *core.Session {
	o.mu.Lock()
	defer o.mu.Unlock()
	s, ok := o.sessions[sessionID]
	if !ok {
		return nil
	}
	cp := *s
	return &cp
}

// buildSpawnSpec composes the backend.SpawnSpec for a StartSession
// call. For tmux-backed agents it produces the same spec as before
// the container work (no behavior change). For agents declaring a
// [agent.container] stanza (gm-root.15.6) it populates Image, Mounts,
// Network, Secrets, Limits, Labels, and UserNS per gm-root.15.10 /
// docs/design/containerized-sessions.md §7.
//
// Workspace bind-mount policy: if the operator did not declare an
// explicit mount for the container Cwd, one is added automatically so
// the worktree the adaptor provisioned is reachable inside the
// container. Files the agent writes land in the worktree on the host
// owned by the operator (UserNS == host UID of the worktree).
func buildSpawnSpec(agent agents.AgentType, workspace, sessionID, agentType, title string, surface *persona.Surface, beadIDOpt ...string) (backend.SpawnSpec, error) {
	beadID := ""
	if len(beadIDOpt) > 0 {
		beadID = beadIDOpt[0]
	}
	spec := backend.SpawnSpec{
		Cwd: workspace,
		Env: map[string]string{
			"GEMBA_SESSION_ID":       sessionID,
			"GEMBA_AGENT_TYPE":       agentType,
			"GEMBA_BEAD_ID":          beadID,
			"GEMBA_MCP_COMMAND":      resolveSiblingBinary("gemba-mcp"),
			"GEMBA_STATE_COMMAND":    resolveSiblingBinary("gemba-state"),
			"PATH":                   prependSiblingBinaryDir(os.Getenv("PATH")),
			"GEMBA_INTERACTION_MODE": string(agent.ResolvedInteractionMode()),
		},
		Command: buildAgentCommand(agent),
		Title:   title,
	}
	if !agent.Container.Present() {
		return spec, nil
	}
	c := agent.Container

	cwd := c.Cwd
	if cwd == "" {
		cwd = "/work"
	}
	spec.Cwd = cwd
	spec.Image = c.Image
	spec.Network = backend.NetworkPolicy{Mode: c.Network}
	spec.Secrets = append([]string(nil), c.Secrets...)

	mem, err := parseMemory(c.Memory)
	if err != nil {
		return backend.SpawnSpec{}, fmt.Errorf("agent %q: container.memory: %w", agent.Name, err)
	}
	spec.Limits = backend.Limits{
		CPUs:      c.CPUs,
		Memory:    mem,
		PidsLimit: c.PidsLimit,
	}

	// ReadOnlyRootfs defaults to true when the operator didn't say
	// otherwise. Nil pointer on the config struct means "not set";
	// non-nil respects the explicit value.
	spec.ReadOnlyRootfs = c.ReadOnlyRootfs == nil || *c.ReadOnlyRootfs

	// UserNS aligns container uid to the host uid of the worktree so
	// writes round-trip without chown surprises. os.Getuid is correct
	// because the gemba process provisioned the worktree itself.
	spec.UserNS = fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())

	// Labels the reaper (gm-root.15.13) relies on. Any operator
	// labels carried by a future ContainerSpec.Labels would merge in
	// here; for now only the gemba-owned keys.
	spec.Labels = map[string]string{
		"gemba.session": sessionID,
		"gemba.agent":   agent.Name,
	}

	// Workspace bind-mount: if the operator's mount list already
	// covers the container cwd, trust it; otherwise add one mapping
	// the worktree to cwd read-write. Operator-declared mounts come
	// first so their order (which the backend preserves) reflects
	// the operator's intent.
	var cwdCovered bool
	operatorMounts := make([]backend.Mount, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		if m.Dst == cwd {
			cwdCovered = true
		}
		operatorMounts = append(operatorMounts, expandMount(m, workspace, sessionID, agent.Name))
	}
	if !cwdCovered {
		operatorMounts = append(operatorMounts, backend.Mount{
			Src:  workspace,
			Dst:  cwd,
			Mode: "rw",
		})
	}

	// gm-utik: when the dispatcher attached a Surface, translate it
	// into the strongest enforcement layer — the OS sees only the
	// surface-allowed paths and refuses anything else. ExpandPaths
	// resolves $HOME / $GOPATH / $GOROOT against the host env (the
	// surface ships them as templates so the resolution stays out of
	// the persona package). Operator mounts compose per SurfaceMode.
	if surface != nil {
		expanded := persona.ExpandPaths(*surface, persona.EnvFromOS(os.Getenv))
		surfaceMounts, _ := SurfaceMounts(expanded, DefaultPathExists)
		mode, _ := ParseSurfaceMode(c.SurfaceMode)
		merged, _ := MergeSurfaceMounts(operatorMounts, surfaceMounts, mode)
		spec.Mounts = merged
	} else {
		spec.Mounts = operatorMounts
	}
	return spec, nil
}

// expandMount resolves {{workspace}}, {{session_id}}, {{agent_name}}
// in a ContainerMount.Src. Unresolved placeholders are left as-is and
// surface later as docker errors — the validator upstream does not
// know the runtime values so we prefer cheap, visible failures over
// silent trimming.
func expandMount(m agents.ContainerMount, workspace, sessionID, agentName string) backend.Mount {
	src := m.Src
	src = strings.ReplaceAll(src, "{{workspace}}", workspace)
	src = strings.ReplaceAll(src, "{{session_id}}", sessionID)
	src = strings.ReplaceAll(src, "{{agent_name}}", agentName)
	return backend.Mount{
		Src:  src,
		Dst:  m.Dst,
		Mode: m.Mode,
		Size: m.Size,
	}
}

// parseMemory accepts the compact human form ("4g", "512m", "1024k")
// or a plain byte count. Empty returns 0 (backend default). The
// parser is deliberately strict — typo'd specs like "4gb" or "4gigs"
// fail config-load rather than silently becoming "no limit".
func parseMemory(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, nil
	}
	var unit int64 = 1
	switch s[len(s)-1] {
	case 'k':
		unit = 1 << 10
		s = s[:len(s)-1]
	case 'm':
		unit = 1 << 20
		s = s[:len(s)-1]
	case 'g':
		unit = 1 << 30
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory spec %q (want '4g', '512m', '1024k', or plain bytes)", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("memory must be non-negative, got %d", n)
	}
	return n * unit, nil
}

// buildAgentCommand assembles the argv the backend SpawnPane runs.
// When the agent registry declares a model and the agent's binary
// accepts a --model flag, append it. MVP treats --model as a
// Claude-Code-specific flag; future agents can drop a per-agent
// arg builder without touching this code.
func buildAgentCommand(a agents.AgentType) []string {
	binary := a.Binary
	if !filepath.IsAbs(binary) && strings.HasPrefix(filepath.Base(binary), "gemba-") {
		binary = resolveSiblingBinary(binary)
	}
	argv := []string{binary}
	argv = append(argv, a.Args...)
	if a.Model != "" && (a.Name == "claude" || a.Preamble == agents.PreambleCodexExec) {
		argv = append(argv, "--model", a.Model)
	}
	return argv
}

// installBridgeForAgent runs the per-agent install strategy for the
// agent's declared HookProfile. Idempotent — a populated worktree is
// a supported no-op, so eager-install on every StartSession is safe.
//
// Kept here (not in internal/cli) so the spawn path doesn't depend on
// the CLI package; both call sites resolve through install.Get.
func installBridgeForAgent(ctx context.Context, workspace string, a agents.AgentType) error {
	name, ok := installerNameForHook(a.Hooks)
	if !ok {
		return nil
	}
	inst, err := install.Get(name)
	if err != nil {
		return err
	}
	_, err = inst.Install(ctx, install.Options{
		Dir:           workspace,
		BridgeCommand: resolveSiblingBinary("gemba-bridge"),
		McpCommand:    resolveSiblingBinary("gemba-mcp"),
	})
	return err
}

// resolveSiblingBinary returns an absolute path to the named companion
// binary (gemba-bridge, gemba-mcp) sitting next to the running gemba
// server, falling back to PATH lookup, and finally to the bare name.
// Spawned Claude panes inherit the operator's shell PATH; our build-
// local bin/ directory is rarely on it, which made hooks fail with
// "command not found" right after a fresh StartSession. Embedding the
// absolute path in the hook stanza sidesteps the PATH dependency.
func resolveSiblingBinary(name string) string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}

func prependSiblingBinaryDir(pathValue string) string {
	dir := filepath.Dir(resolveSiblingBinary("gemba-state"))
	if pathValue == "" {
		return dir
	}
	return dir + string(os.PathListSeparator) + pathValue
}

// installerNameForHook maps an agents.HookProfile to its installer
// registry key. Returns ok=false for HookNone (no installation).
func installerNameForHook(h agents.HookProfile) (string, bool) {
	switch h {
	case agents.HookClaudeCode:
		return "claude", true
	case agents.HookPromptCommand:
		return "shell_only", true
	case agents.HookNone:
		return "", false
	default:
		return "", false
	}
}
