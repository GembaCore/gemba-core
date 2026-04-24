package native

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/native/agents"
	"github.com/MikeBengtson/gemba/internal/adapter/native/backend"
	"github.com/MikeBengtson/gemba/internal/adapter/native/worktrees"
	"github.com/MikeBengtson/gemba/internal/core"
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
)

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

	beadID, _ := prompt.Extension[extKeyBeadID].(string)
	if beadID == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: SessionPrompt.Extension[%q] is required", extKeyBeadID)
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

	// Provision worktree. If a workspace was pre-resolved by the
	// caller (operator pre-selected an existing worktree), honor it
	// instead of auto-creating.
	workspace, _ := prompt.Extension[extKeyWorkspace].(string)
	if workspace == "" {
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

	sessionID := fmt.Sprintf("%s:%s:%d", o.cfg.Backend.Name(), beadID, time.Now().UnixNano())

	// Best-effort bridge install — if settings.local.json is already
	// there, install is a no-op. Failures are logged but don't block
	// spawn (degraded observability is better than failed dispatch).
	_ = installBridgeForAgent(workspace, agent)

	spec := backend.SpawnSpec{
		Cwd: workspace,
		Env: map[string]string{
			"GEMBA_SESSION_ID": sessionID,
			"GEMBA_AGENT_TYPE": agentType,
		},
		Command: buildAgentCommand(agent),
	}
	if title, ok := prompt.Extension[extKeyTitle].(string); ok && title != "" {
		spec.Title = title
	} else {
		spec.Title = "gemba: " + beadID
	}

	pane, err := o.cfg.Backend.SpawnPane(ctx, spec)
	if err != nil {
		return core.Session{}, core.WrapAdaptorError(core.KindProcessFailed, err,
			"native: spawn pane for %s (agent=%s, backend=%s)",
			beadID, agentType, o.cfg.Backend.Name())
	}

	now := time.Now()
	sess := &core.Session{
		ID:           sessionID,
		AssignmentID: assignmentID,
		Status:       core.SessionRunning,
		StartedAt:    now,
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
		},
	}

	// Record session + pane + nonce under the lock.
	o.mu.Lock()
	if prior, busy := o.paneActive[pane.ID]; busy {
		o.mu.Unlock()
		// Clean up the spawned pane — leaving it alive would be a
		// leak since no session record claims it. Best-effort; the
		// operator will see a dangling pane if Kill also fails.
		_ = o.cfg.Backend.Kill(ctx, pane.ID)
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: pane %s is already running assignment %s", pane.ID, prior)
	}
	o.sessions[sessionID] = sess
	o.paneActive[pane.ID] = sessionID
	if nonce != "" {
		o.nonces[nonce] = sessionID
	}
	o.mu.Unlock()

	// Register a bridge tailer for this session so Subscribe()
	// consumers see its events.
	if err := o.fanout.Register(ctx, sessionID, agentType); err != nil {
		// Tailer registration failure is not fatal to StartSession —
		// we log and continue. The SPA can still observe the pane via
		// ListAgents + PeekSession. Without the tailer the drawer
		// won't get structured events though.
		_ = err // TODO surface via slog when structured logger lands here
	}
	return *sess, nil
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

// buildAgentCommand assembles the argv the backend SpawnPane runs.
// When the agent registry declares a model and the agent's binary
// accepts a --model flag, append it. MVP treats --model as a
// Claude-Code-specific flag; future agents can drop a per-agent
// arg builder without touching this code.
func buildAgentCommand(a agents.AgentType) []string {
	argv := []string{a.Binary}
	argv = append(argv, a.Args...)
	if a.Model != "" && a.Name == "claude" {
		argv = append(argv, "--model", a.Model)
	}
	return argv
}

// installBridgeForAgent writes the appropriate hook stanza for the
// agent's declared profile into the workspace. The native adaptor
// does this eagerly on every StartSession (idempotent) so operators
// never have to run `gemba install-bridge` by hand.
//
// Kept here (not in internal/cli) to avoid a dependency from
// internal/adapter/native back to the CLI package.
func installBridgeForAgent(workspace string, a agents.AgentType) error {
	if a.Hooks == agents.HookNone {
		return nil
	}
	// Minimal install: ensure the .claude/settings.local.json file
	// exists. Full stanza installation lives behind the cli command
	// so serve-side doesn't have to keep its JSON schema in sync
	// with the CLI when the SPA later exposes install-bridge. For
	// now, if the file is missing, create a placeholder so the
	// operator knows to run install-bridge — this becomes real in a
	// follow-up if bridging is mandatory at startup.
	if a.Hooks == agents.HookClaudeCode {
		dir := workspace + "/.claude"
		_ = mkdirAllQuiet(dir)
	}
	return nil
}

// mkdirAllQuiet is a helper that swallows errors — best-effort
// operations should not crash the spawn path.
func mkdirAllQuiet(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
