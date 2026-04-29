package persona

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
	corepersona "github.com/GembaCore/gemba-core/internal/core/persona"
	"github.com/GembaCore/gemba-core/internal/persona/purviewgate"
)

// Dispatcher is the in-memory orchestrator for persona consults. It
// owns the lifecycle of a consult between [Begin] and [Finish]:
// composes the prompt, generates the consult ID, registers the live
// session, accepts incoming MCP-tool emissions via [Receive], and
// persists the final [corepersona.PersonaConsultRecord] when [Finish]
// is called.
//
// The dispatcher is intentionally decoupled from the spawn machinery.
// A future slice will wire it to [internal/adapter/native/start.go] —
// at which point Begin returns a Consult, the spawn driver launches a
// Claude Code session with GEMBA_SESSION_ID=Consult.ID, and the bridge
// tailer delivers GembaSkillOutput frames into Receive. Spawning
// outside this package keeps the dispatcher unit-testable without a
// real tmux + claude binary.
//
// Concurrency: every public method is safe for concurrent use. The
// sessions map is guarded by a RWMutex; the audit log already
// serialises file writes internally.
type Dispatcher struct {
	auditLog *AuditLog
	repos    *core.RepositoryRegistry
	// workspaceDir is the directory containing .gemba/. Used as the
	// cwd for project-scope persona consults (gm-k2jn). Absolute path.
	workspaceDir string

	mu       sync.RWMutex
	sessions map[string]*Consult
	// spawn is the optional post-Begin hook. nil = dry-run mode
	// (Begin returns the consult without launching a session). The
	// production wiring installs [NativeSpawn]; tests usually leave
	// it nil. Read with the RLock; written by [SetSpawnFunc] under
	// the write lock.
	spawn SpawnFunc

	// appliers is the per-skill executor registry (gm-twp2.1).
	// Written by [SetApplier] at boot; Apply reads it to decide
	// whether the apply path executes the SuggestedAction or just
	// records intent.
	appliers map[string]Applier

	// mutationKindResolver maps (skillID, line) → MutationKind so
	// the Purview gate (gm-3on) knows which authority the action
	// requires. nil = no Purview gating wired (backward compat with
	// pre-gm-3on tests + skills that don't declare a mutation kind).
	mutationKindResolver MutationKindResolver

	// phaseSource returns the project's current Phase for the
	// Purview gate. nil = phase unspecified (gm-jt9 fallback).
	// Wiring lands when gm-jt9's phase store is available.
	phaseSource PhaseSource

	now   func() time.Time
	newID func() string
}

// MutationKindResolver maps a SkillID + validated line value to the
// [purviewgate.MutationKind] the action would invoke. Returning the
// empty string means "this line has no mutation kind" — the gate is
// skipped for that line. The dispatcher invokes the resolver under
// no lock; implementations MUST be pure.
type MutationKindResolver func(skillID string, line any) purviewgate.MutationKind

// PhaseSource returns the project's currently-active Phase. The gate
// uses this to decide whether a persona's Purview is dormant or
// binding. The dispatcher invokes it under no lock; implementations
// MUST be pure / fast / non-blocking. Returns
// [purviewgate.PhaseUnspecified] when the phase is unknown — the
// gate's fallback behavior treats that as "dormant" for any persona
// declaring ActivePhases (see purviewgate.Allow doc).
type PhaseSource func() corepersona.Phase

// DispatcherOption tunes a [Dispatcher] at construction. All options
// have safe defaults; tests use them to inject deterministic time
// and id sources.
type DispatcherOption func(*Dispatcher)

// WithClock overrides the dispatcher's time source. Default is
// time.Now().UTC.
func WithClock(now func() time.Time) DispatcherOption {
	return func(d *Dispatcher) { d.now = now }
}

// WithIDFunc overrides the consult-id generator. Default returns
// "consult-<utc-yyyymmddhhmmss>-<6 hex>".
func WithIDFunc(fn func() string) DispatcherOption {
	return func(d *Dispatcher) { d.newID = fn }
}

// WithRepositoryRegistry attaches the workspace's repository registry
// (gm-26n4) so Begin can resolve persona-scope cwd at consult time.
// Without this, scope=repository and scope=any consults will fail
// Begin with a clear error.
func WithRepositoryRegistry(repos *core.RepositoryRegistry) DispatcherOption {
	return func(d *Dispatcher) { d.repos = repos }
}

// WithWorkspaceDir sets the cwd for project-scope consults
// (gm-k2jn). Required when ANY persona in the workspace declares
// scope=project; the dispatcher's Begin returns an error if this is
// empty for such a consult.
func WithWorkspaceDir(dir string) DispatcherOption {
	return func(d *Dispatcher) { d.workspaceDir = dir }
}

// WithMutationKindResolver wires the Purview gate (gm-3on) by
// teaching the dispatcher how to derive a [purviewgate.MutationKind]
// from a validated skill output line. Without a resolver registered
// the gate is skipped — callers retain pre-gm-3on behavior. With one
// registered, every Manager-variety Apply call routes through
// [purviewgate.Allow] before the applier runs; deny short-circuits
// with [ErrPurviewBlocked].
func WithMutationKindResolver(r MutationKindResolver) DispatcherOption {
	return func(d *Dispatcher) { d.mutationKindResolver = r }
}

// WithPhaseSource wires the Phase primitive (gm-jt9) into the gate.
// nil leaves the dispatcher in the "phase unspecified" fallback —
// the gate dorms any Purview that declares ActivePhases. Once
// gm-jt9's phase store is available, the HTTP wiring should pass a
// closure that reads `phase.WorkspaceState.Phase`.
func WithPhaseSource(s PhaseSource) DispatcherOption {
	return func(d *Dispatcher) { d.phaseSource = s }
}

// NewDispatcher returns a Dispatcher writing audit rows to the
// supplied [AuditLog]. Pass nil to disable audit-log writes (useful
// in tests that only exercise in-memory state); the dispatcher will
// still produce PersonaConsultRecord values via Finish.
func NewDispatcher(auditLog *AuditLog, opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{
		auditLog: auditLog,
		sessions: make(map[string]*Consult),
		now:      func() time.Time { return time.Now().UTC() },
		newID:    defaultConsultID,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// ConsultStatus enumerates the lifecycle states of a Consult. The
// HTTP surface renders these verbatim; the SPA filters /insights/
// personas by them.
type ConsultStatus string

const (
	// StatusRunning — Begin has registered the session; it has not
	// yet been Finish'd.
	StatusRunning ConsultStatus = "running"
	// StatusCompleted — Finish was called with no Error.
	StatusCompleted ConsultStatus = "completed"
	// StatusFailed — Finish was called with a non-empty Error
	// (provider returned an APIError, the spawn died, etc).
	StatusFailed ConsultStatus = "failed"
)

// Consult is the live state of one persona consult. The struct is
// returned by Begin and read back by Get / List. Fields populated by
// Receive (ValidatedLines, LineErrors) accumulate as the spawned
// session emits its MCP tool calls.
//
// Get and List return a deep-enough snapshot copy so concurrent
// readers cannot race a still-running Receive. Mutating the returned
// value has no effect on the registry — the dispatcher's locked
// methods are the only write path. The value Begin returns IS the
// live registry pointer (so the caller doesn't pay for a copy on the
// happy path); only post-Begin reads via Get / List are snapshots.
type Consult struct {
	ID        string
	PersonaID string
	SkillID   string
	Workspace string
	StartedAt time.Time
	EndedAt   time.Time
	Status    ConsultStatus

	// WorkingDir is the absolute path the spawn driver will use as
	// cwd for the Claude Code session (gm-k2jn). Resolved by Begin
	// from the persona's [corepersona.PersonaScope]:
	//   - scope=project    → the dispatcher's workspaceDir
	//   - scope=repository → the registered Repository's Path
	//   - scope=any        → the caller-supplied repository override's Path
	WorkingDir string

	// RepositoryID is the repository the consult is bound to (empty
	// for project-scope). Stored separately from Scope so the SPA
	// can surface "Working in <repo>" without walking the persona
	// definition.
	RepositoryID core.RepositoryID

	// Composed is the rendered prompt pair. Stored on the consult so
	// the spawn driver and the audit log share a single source of
	// truth (no re-composition risk if a downstream layer mutates
	// inputs after Begin).
	Composed Composed

	// RawRequest is the unmodified request body (Skill.ValidateInput
	// input). The audit log's Request field receives this verbatim.
	RawRequest json.RawMessage

	// ValidatedInput is the typed value Skill.ValidateInput returned.
	// Available so the spawn driver can pass it to template helpers
	// without re-decoding.
	ValidatedInput any

	// ValidatedLines accumulates skill-validated output lines in
	// arrival order. Mixed types per skill (e.g. *StrategyLine,
	// *RecommendationLine, …) — callers type-switch on the element.
	ValidatedLines []any

	// LineErrors record lines that failed Skill.ValidateOutputLine.
	// The audit log captures these so a hallucinated tool call is
	// visible post-hoc; a future warning-line splice will surface
	// them to the SPA in real time.
	LineErrors []LineError

	// AppliedIdx lists the ValidatedLines indexes the operator has
	// applied via [Dispatcher.Apply] (gm-twp2). Mirrors the same-
	// named field on the on-disk audit-log row so live and archived
	// consult shapes carry the same apply history.
	AppliedIdx []int

	// Error is set by Finish for failed consults. Empty for
	// completed; populated for failed.
	Error string

	// Tokens / Dollars / LatencyMs / Model are filled in by Finish
	// from the spawn driver's reported usage.
	Tokens    corepersona.TokenUsage
	Dollars   float64
	LatencyMs int
	Model     string

	// skill is the bound skill instance for output-line validation
	// during Receive. Unexported so json.Marshal (and the HTTP
	// /api/v1/consult/:id encoder) skips it without ceremony.
	skill corepersona.Skill

	// persona is the bound persona definition. Stored so
	// [Dispatcher.Apply] can run the Purview gate (gm-3on)
	// without an extra registry lookup. Unexported — the JSON
	// encoder skips it; the HTTP layer surfaces persona metadata
	// via PersonaID + dedicated endpoints.
	persona *corepersona.Persona
}

// LineError is one rejected MCP-tool emission. Index is the offset
// inside the offending GembaSkillOutput frame; Raw is the raw JSON
// of the line; Reason is the human-readable error from
// [corepersona.Skill.ValidateOutputLine].
type LineError struct {
	Index  int             `json:"index"`
	Raw    json.RawMessage `json:"raw"`
	Reason string          `json:"reason"`
}

// BeginRequest carries the inputs to Dispatcher.Begin. The HTTP
// layer assembles it from the /api/v1/consult body plus the
// resolved persona/skill from the registries.
type BeginRequest struct {
	Persona   *corepersona.Persona
	Skill     corepersona.Skill
	Workspace string
	RawInput  json.RawMessage
	Guidance  string

	// Template is the substitution values for the persona's
	// system_prompt. The dispatcher fills Role from Persona.Role
	// when it sees an empty Role here so callers can omit the
	// duplicate.
	Template TemplateValues

	// RepositoryOverride binds a scope=any consult to a specific
	// repository (gm-k2jn). Required when Persona.Scope.Kind is
	// ScopeAny; ignored otherwise. The HTTP layer requires a `repo`
	// query parameter on /api/v1/consult to populate this.
	RepositoryOverride core.RepositoryID

	// ProjectValues etc. are the per-call promptctx layer overrides.
	// Pass-through to [Compose].
	ProjectValues     []string
	ProjectGuardrails []string
	ProjectGoals      []string
	WorkspaceValues   []string
	ContextChunks     []string

	// WalkID, when non-empty, marks this consult as running inside
	// an active Gemba walk (gm-4p6). The dispatcher forwards it to
	// [Compose] which appends the walk-mode system-prompt
	// extension. The HTTP /walks/:id/turn handler (gm-wgv's
	// domain) populates this from walk.IDFromContext on the
	// inbound request context. Zero value = non-walk consult; the
	// PM persona then behaves as it always has.
	WalkID string
}

// Begin starts a new consult. It runs Skill.ValidateInput on
// req.RawInput, composes the prompt, generates a consult ID, and
// registers the consult in the in-memory map with status="running".
//
// The returned Consult is the live registry entry; mutations via
// Receive/Finish update the same pointer. Callers MUST NOT mutate
// fields directly — go through Receive/Finish.
//
// Errors:
//   - missing/invalid fields → wrapped error with caller-safe message
//   - Skill.ValidateInput failure → wrapped, surfaced as 400 by HTTP
//   - prompt composition failure → wrapped (rare; Compose only fails
//     on nil persona/skill/input or unmarshalable input)
func (d *Dispatcher) Begin(req BeginRequest) (*Consult, error) {
	if req.Persona == nil {
		return nil, errors.New("persona/dispatcher: persona must not be nil")
	}
	if req.Skill == nil {
		return nil, errors.New("persona/dispatcher: skill must not be nil")
	}
	if !corepersona.PersonaCanInvoke(req.Persona, req.Skill.ID()) {
		return nil, fmt.Errorf("persona/dispatcher: persona %q is not authorized to invoke skill %q",
			req.Persona.ID, req.Skill.ID())
	}
	if strings.TrimSpace(req.Workspace) == "" {
		return nil, errors.New("persona/dispatcher: workspace must not be empty")
	}
	if len(req.RawInput) == 0 {
		return nil, errors.New("persona/dispatcher: raw_input must not be empty")
	}

	validated, err := req.Skill.ValidateInput(req.RawInput)
	if err != nil {
		return nil, fmt.Errorf("persona/dispatcher: validate input: %w", err)
	}

	// Resolve the cwd for the spawn driver. This is the load-bearing
	// answer to gm-k2jn's "where does this persona run?" question and
	// the same call that catches an unregistered repository early
	// (before we sink the cost of prompt composition).
	workingDir, err := req.Persona.Scope.ResolveWorkingDir(d.workspaceDir, d.repos, req.RepositoryOverride)
	if err != nil {
		return nil, fmt.Errorf("persona/dispatcher: resolve working dir: %w", err)
	}
	resolvedRepoID := repositoryFromScope(req.Persona.Scope, req.RepositoryOverride)

	tmpl := req.Template
	if tmpl.Role == "" {
		tmpl.Role = req.Persona.Role
	}

	composed, err := Compose(ComposeRequest{
		Persona:           req.Persona,
		Skill:             req.Skill,
		Template:          tmpl,
		Input:             validated,
		Guidance:          req.Guidance,
		ProjectValues:     req.ProjectValues,
		ProjectGuardrails: req.ProjectGuardrails,
		ProjectGoals:      req.ProjectGoals,
		WorkspaceValues:   req.WorkspaceValues,
		ContextChunks:     req.ContextChunks,
		WalkID:            req.WalkID,
	})
	if err != nil {
		return nil, fmt.Errorf("persona/dispatcher: compose prompt: %w", err)
	}

	now := d.now()
	c := &Consult{
		ID:             d.newID(),
		PersonaID:      req.Persona.ID,
		SkillID:        req.Skill.ID(),
		Workspace:      req.Workspace,
		StartedAt:      now,
		Status:         StatusRunning,
		WorkingDir:     workingDir,
		RepositoryID:   resolvedRepoID,
		Composed:       composed,
		RawRequest:     append(json.RawMessage(nil), req.RawInput...),
		ValidatedInput: validated,
		skill:          req.Skill,
		persona:        req.Persona,
	}

	d.mu.Lock()
	if _, exists := d.sessions[c.ID]; exists {
		d.mu.Unlock()
		return nil, fmt.Errorf("persona/dispatcher: id collision %q (newID is broken)", c.ID)
	}
	d.sessions[c.ID] = c
	d.mu.Unlock()
	return c, nil
}

// AuditLog returns the dispatcher's audit-log handle. Used by HTTP
// handlers that fall through to disk for consults that aged out of
// the in-memory registry (gm-twp2 audit-log fall-through). Nil when
// the dispatcher was constructed with no audit log (test paths).
func (d *Dispatcher) AuditLog() *AuditLog { return d.auditLog }

// Get returns the consult with the given id. The bool is false when
// no consult is registered.
//
// Returned value is a snapshot taken under the read lock — callers
// can read its fields freely without racing against a concurrent
// Receive. Mutating the snapshot has no effect on the registry; the
// dispatcher's locked methods are the only write surface.
func (d *Dispatcher) Get(id string) (*Consult, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	c, ok := d.sessions[id]
	if !ok {
		return nil, false
	}
	snap := snapshotConsult(c)
	return snap, true
}

// List returns a snapshot of all currently-registered consults,
// newest StartedAt first. Used by /insights/personas to render
// in-flight + recent consults; the audit log carries the full
// history for closed consults.
//
// Each entry is a snapshot copy taken under the read lock — same
// race-free contract as Get.
func (d *Dispatcher) List() []*Consult {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*Consult, 0, len(d.sessions))
	for _, c := range d.sessions {
		out = append(out, snapshotConsult(c))
	}
	sortByStartedAtDesc(out)
	return out
}

// snapshotConsult returns a deep-enough copy of c so a caller can
// read every field without racing a concurrent Receive. Caller MUST
// hold d.mu (read or write) when calling — this function does not
// take a lock itself.
//
// "Deep enough" means slices with mutable backing get their own
// arrays. Pointers / interfaces inside ValidatedLines are not deep-
// cloned because the skill validator returns immutable values
// (typed line records produced once, never mutated).
func snapshotConsult(c *Consult) *Consult {
	if c == nil {
		return nil
	}
	snap := *c
	if c.ValidatedLines != nil {
		snap.ValidatedLines = append([]any(nil), c.ValidatedLines...)
	}
	if c.LineErrors != nil {
		snap.LineErrors = append([]LineError(nil), c.LineErrors...)
	}
	if c.RawRequest != nil {
		snap.RawRequest = append(json.RawMessage(nil), c.RawRequest...)
	}
	return &snap
}

// Receive ingests one MCP-tool emission for an active consult. The
// dispatcher resolves the skill via the consult's stored skill
// reference, validates each line, and appends results to the
// consult's ValidatedLines / LineErrors. Lines that fail validation
// produce a LineError; the consult does NOT fail (a hallucinated
// line should not destroy preceding good lines).
//
// Receive is idempotent in the sense that a re-delivery of the same
// frame yields duplicate lines — the bridge tailer is responsible
// for deduping at the frame level. Most spawn drivers will only call
// Receive once per emit_skill_output tool call.
func (d *Dispatcher) Receive(consultID string, lines []json.RawMessage) error {
	if strings.TrimSpace(consultID) == "" {
		return errors.New("persona/dispatcher: consult_id must not be empty")
	}
	if len(lines) == 0 {
		return errors.New("persona/dispatcher: lines must not be empty")
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	c, ok := d.sessions[consultID]
	if !ok {
		return fmt.Errorf("persona/dispatcher: unknown consult_id %q", consultID)
	}
	if c.Status != StatusRunning {
		return fmt.Errorf("persona/dispatcher: consult %q already %s", consultID, c.Status)
	}

	// Reuse the skill stored at Begin via the validated-input proxy:
	// Begin held a ref to the skill in the closure, but we don't
	// keep it on the Consult so /api/v1/consult/:id can serialize
	// the consult cleanly. Instead, look up by id below.
	skill := c.skill
	if skill == nil {
		return fmt.Errorf("persona/dispatcher: consult %q has no skill bound (registry corrupted)", consultID)
	}

	for _, raw := range lines {
		validated, err := skill.ValidateOutputLine(raw)
		if err != nil {
			c.LineErrors = append(c.LineErrors, LineError{
				Index:  len(c.ValidatedLines) + len(c.LineErrors),
				Raw:    append(json.RawMessage(nil), raw...),
				Reason: err.Error(),
			})
			continue
		}
		c.ValidatedLines = append(c.ValidatedLines, validated)
	}
	return nil
}

// FinishInfo carries the post-spawn metadata the dispatcher persists
// to the audit log: cost, latency, served model, and any error from
// the spawn path. A nil-equivalent (zero value) is acceptable for
// in-test consults that don't run a real provider.
type FinishInfo struct {
	Tokens    corepersona.TokenUsage
	Dollars   float64
	LatencyMs int
	Model     string

	// Error, when non-empty, marks the consult as failed and lands
	// in the audit-log row's Error field.
	Error string

	// EndedAt overrides the wall-clock stamp. Zero means "use now".
	EndedAt time.Time
}

// ApplyResult is what [Dispatcher.Apply] returns to the HTTP layer.
// The line is the validated entry the operator chose to apply
// (typically a *RecommendationLine for epic_order); the SPA renders
// its SuggestedAction so the operator knows what just got recorded.
//
// Executed reports whether a registered [Applier] ran the action.
// True when an applier is registered for the consult's skill AND
// it returned without error; false in record-only mode (no applier
// registered). Executor is the applier's response when Executed.
type ApplyResult struct {
	// Line is the validated-output entry at the requested index.
	// Type is skill-specific (e.g. *epic_order.RecommendationLine);
	// callers type-switch when they need the SuggestedAction.
	Line any
	// AppliedIdx is the snapshot of every recorded apply for this
	// consult AFTER the new index was added. Useful for the HTTP
	// response so the SPA can render the apply history without a
	// follow-up GET.
	AppliedIdx []int
	// Executed is true when a registered Applier ran the action.
	// False in record-only mode — the operator dispatches the
	// SuggestedAction manually.
	Executed bool
	// Executor is the applier's response. Zero value when
	// Executed is false.
	Executor ApplierResult
}

// Apply records that the operator applied the validated line at
// idx and (when an applier is registered for the consult's skill)
// executes the SuggestedAction. Errors:
//
//   - unknown consult_id (id never began OR already finished)
//   - idx out of range (negative or ≥ len(ValidatedLines))
//   - duplicate apply (idx already in AppliedIdx; idempotency gate)
//   - applier failure (no AppliedIdx append on failure so a retry
//     can re-attempt without hitting the duplicate gate)
//
// Concurrency: takes the write lock to validate + read the line +
// look up the applier; releases before invoking the applier so a
// slow WorkPlane mutation doesn't block other consults; re-takes
// the write lock to append AppliedIdx on success.
func (d *Dispatcher) Apply(ctx context.Context, consultID string, idx int) (ApplyResult, error) {
	if strings.TrimSpace(consultID) == "" {
		return ApplyResult{}, errors.New("persona/dispatcher: consult_id must not be empty")
	}
	d.mu.Lock()
	c, ok := d.sessions[consultID]
	if !ok {
		d.mu.Unlock()
		return ApplyResult{}, fmt.Errorf("persona/dispatcher: unknown consult_id %q", consultID)
	}
	if idx < 0 || idx >= len(c.ValidatedLines) {
		d.mu.Unlock()
		return ApplyResult{}, fmt.Errorf("persona/dispatcher: apply idx %d out of range (have %d lines)", idx, len(c.ValidatedLines))
	}
	for _, prior := range c.AppliedIdx {
		if prior == idx {
			d.mu.Unlock()
			return ApplyResult{}, fmt.Errorf("persona/dispatcher: apply idx %d already recorded", idx)
		}
	}
	line := c.ValidatedLines[idx]
	applier := d.applierFor(c.SkillID)
	persona := c.persona
	skillID := c.SkillID
	resolver := d.mutationKindResolver
	phaseSource := d.phaseSource
	d.mu.Unlock()

	// Purview gate (gm-3on). Runs outside the lock — pure check
	// against persona config + an optional phase source. Skips
	// silently if no resolver is wired (pre-gm-3on behavior). Coach
	// personas bypass the gate inside [purviewgate.Allow]; we still
	// invoke for symmetry + audit clarity. Deny short-circuits the
	// applier path entirely so the mutation never lands; the
	// AppliedIdx stays untouched so a corrected retry is allowed
	// (matches the applier-failure semantics).
	if resolver != nil && persona != nil && persona.Variety == corepersona.VarietyManager {
		kind := resolver(skillID, line)
		if kind != "" {
			currentPhase := purviewgate.PhaseUnspecified
			if phaseSource != nil {
				currentPhase = phaseSource()
			}
			decision := purviewgate.Allow(persona, kind, currentPhase)
			if !decision.Allowed {
				return ApplyResult{}, &purviewgate.ErrPurviewBlocked{
					PersonaID:    persona.ID,
					MutationKind: kind,
					Phase:        currentPhase,
					Reason:       decision.Reason,
				}
			}
		}
	}

	// Executor pass — runs outside the lock so a slow WorkPlane
	// mutation doesn't block other consults' Receive / Apply
	// calls.
	var executorResult ApplierResult
	executed := false
	if applier != nil {
		var err error
		executorResult, err = applier.Apply(ctx, line)
		if err != nil {
			// Roll-forward never happened — the action did not
			// land. AppliedIdx stays untouched so the operator's
			// retry is allowed (would otherwise 409 the next
			// click).
			return ApplyResult{}, fmt.Errorf("persona/dispatcher: applier for skill %q: %w", c.SkillID, err)
		}
		executed = true
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	// Re-fetch under the lock — the consult may have been
	// Finished while the executor ran. Surface as "unknown" so
	// the operator gets the same error as a stale id.
	c, ok = d.sessions[consultID]
	if !ok {
		return ApplyResult{}, fmt.Errorf("persona/dispatcher: consult %q finished during applier", consultID)
	}
	c.AppliedIdx = append(c.AppliedIdx, idx)
	snapshot := append([]int(nil), c.AppliedIdx...)
	return ApplyResult{
		Line:       line,
		AppliedIdx: snapshot,
		Executed:   executed,
		Executor:   executorResult,
	}, nil
}

// Finish closes a consult, removes it from the in-memory registry,
// and writes a [corepersona.PersonaConsultRecord] to the audit log.
// Returns the audit-log record so callers can echo it back over
// HTTP without a second Get.
//
// A consult that has been Finish'd cannot be resumed. Subsequent
// Get returns false; subsequent Receive returns an error.
func (d *Dispatcher) Finish(consultID string, info FinishInfo) (*corepersona.PersonaConsultRecord, error) {
	if strings.TrimSpace(consultID) == "" {
		return nil, errors.New("persona/dispatcher: consult_id must not be empty")
	}
	d.mu.Lock()
	c, ok := d.sessions[consultID]
	if !ok {
		d.mu.Unlock()
		return nil, fmt.Errorf("persona/dispatcher: unknown consult_id %q", consultID)
	}
	if c.Status != StatusRunning {
		d.mu.Unlock()
		return nil, fmt.Errorf("persona/dispatcher: consult %q already %s", consultID, c.Status)
	}

	endedAt := info.EndedAt
	if endedAt.IsZero() {
		endedAt = d.now()
	}
	c.EndedAt = endedAt
	c.Tokens = info.Tokens
	c.Dollars = info.Dollars
	c.LatencyMs = info.LatencyMs
	c.Model = info.Model
	c.Error = info.Error
	if info.Error != "" {
		c.Status = StatusFailed
	} else {
		c.Status = StatusCompleted
	}

	rec, err := buildAuditRecord(c)
	delete(d.sessions, consultID)
	d.mu.Unlock()
	if err != nil {
		return nil, err
	}

	if d.auditLog != nil {
		if err := d.auditLog.Append(rec); err != nil {
			return &rec, fmt.Errorf("persona/dispatcher: audit-log append: %w", err)
		}
	}
	return &rec, nil
}

// buildAuditRecord projects a finished Consult onto a
// PersonaConsultRecord. Called under the dispatcher lock so the
// fields read here are consistent.
func buildAuditRecord(c *Consult) (corepersona.PersonaConsultRecord, error) {
	resp, err := json.Marshal(struct {
		Lines       []any       `json:"lines"`
		LineErrors  []LineError `json:"line_errors,omitempty"`
		Diagnostics any         `json:"diagnostics,omitempty"`
	}{
		Lines:       c.ValidatedLines,
		LineErrors:  c.LineErrors,
		Diagnostics: c.Composed.Diagnostics,
	})
	if err != nil {
		return corepersona.PersonaConsultRecord{}, fmt.Errorf("persona/dispatcher: marshal response: %w", err)
	}
	var applied []int
	if len(c.AppliedIdx) > 0 {
		applied = append([]int(nil), c.AppliedIdx...)
	}
	return corepersona.PersonaConsultRecord{
		ID:         c.ID,
		PersonaID:  c.PersonaID,
		SkillID:    c.SkillID,
		Workspace:  c.Workspace,
		StartedAt:  c.StartedAt,
		EndedAt:    c.EndedAt,
		Request:    append(json.RawMessage(nil), c.RawRequest...),
		Response:   resp,
		Tokens:     c.Tokens,
		Dollars:    c.Dollars,
		Model:      c.Model,
		LatencyMs:  c.LatencyMs,
		AppliedIdx: applied,
		Error:      c.Error,
	}, nil
}

// repositoryFromScope picks the repository id that gets stored on
// the Consult for "Working in <repo>" UI affordance. Project-scope
// consults have no bound repo (empty); repository-scope consults
// echo the scope's id; any-scope consults echo the caller override.
func repositoryFromScope(s corepersona.PersonaScope, override core.RepositoryID) core.RepositoryID {
	switch s.Kind {
	case corepersona.ScopeRepository:
		return s.RepositoryID
	case corepersona.ScopeAny:
		return override
	}
	return ""
}

// defaultConsultID returns a sortable, time-prefixed id with a 6-hex
// random suffix. Format: consult-YYYYMMDDhhmmss-XXXXXX. The dispatcher
// stores ids on disk via the audit log's id-as-filename pattern, so
// the format MUST stay free of path separators (validated by AuditLog).
func defaultConsultID() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return "consult-" + time.Now().UTC().Format("20060102150405") + "-" + hex.EncodeToString(b[:])
}

func sortByStartedAtDesc(cs []*Consult) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].StartedAt.After(cs[j-1].StartedAt); j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}
