package persona

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
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

	mu       sync.RWMutex
	sessions map[string]*Consult

	now   func() time.Time
	newID func() string
}

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
// returned by Begin and read back by Get. Fields populated by
// Receive (ValidatedLines, LineErrors) accumulate as the spawned
// session emits its MCP tool calls.
//
// Treat the value Get returns as read-only — internal mutations
// happen through the dispatcher's locked methods. Concurrent
// readers can race with a still-running consult, so callers
// rendering UI should snapshot the value once per render.
type Consult struct {
	ID        string
	PersonaID string
	SkillID   string
	Workspace string
	StartedAt time.Time
	EndedAt   time.Time
	Status    ConsultStatus

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
	Persona  *corepersona.Persona
	Skill    corepersona.Skill
	Workspace string
	RawInput  json.RawMessage
	Guidance  string

	// Template is the substitution values for the persona's
	// system_prompt. The dispatcher fills Role from Persona.Role
	// when it sees an empty Role here so callers can omit the
	// duplicate.
	Template TemplateValues

	// ProjectValues etc. are the per-call promptctx layer overrides.
	// Pass-through to [Compose].
	ProjectValues     []string
	ProjectGuardrails []string
	ProjectGoals      []string
	WorkspaceValues   []string
	ContextChunks     []string
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
		Composed:       composed,
		RawRequest:     append(json.RawMessage(nil), req.RawInput...),
		ValidatedInput: validated,
		skill:          req.Skill,
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

// Get returns the consult with the given id. The bool is false when
// no consult is registered. Get returns a pointer into the live
// registry; treat as read-only.
func (d *Dispatcher) Get(id string) (*Consult, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	c, ok := d.sessions[id]
	return c, ok
}

// List returns a snapshot of all currently-registered consults,
// newest StartedAt first. Used by /insights/personas to render
// in-flight + recent consults; the audit log carries the full
// history for closed consults.
func (d *Dispatcher) List() []*Consult {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*Consult, 0, len(d.sessions))
	for _, c := range d.sessions {
		out = append(out, c)
	}
	sortByStartedAtDesc(out)
	return out
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
	return corepersona.PersonaConsultRecord{
		ID:        c.ID,
		PersonaID: c.PersonaID,
		SkillID:   c.SkillID,
		Workspace: c.Workspace,
		StartedAt: c.StartedAt,
		EndedAt:   c.EndedAt,
		Request:   append(json.RawMessage(nil), c.RawRequest...),
		Response:  resp,
		Tokens:    c.Tokens,
		Dollars:   c.Dollars,
		Model:     c.Model,
		LatencyMs: c.LatencyMs,
		Error:     c.Error,
	}, nil
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
