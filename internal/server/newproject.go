// /api/v1/newproject/* — conversational project-creation flow
// (gm-root.17; design: docs/design/newproject.md).
//
// This file ships the server-side backing for the /new SPA route. The
// route runs a transient Onboarder-persona conversation against the
// `newproject` skill, then commits the resulting plan tree as a real
// project via an atomic transaction (gm-root.17.6).
//
// Wire contract: web/src/api/newproject.ts is the canonical shape. The
// Go types here mirror those typed-client interfaces verbatim — JSON
// tags use the design-doc PascalCase field names so the SPA does not
// need a separate translation layer.
//
// Three endpoints:
//   POST /api/v1/newproject/start          — mint a session
//   POST /api/v1/newproject/:id/turn       — submit an operator message
//   POST /api/v1/newproject/:id/ratify     — atomic commit
//
// The skill (gm-root.17.5) and the Onboarder persona host
// (gm-root.17.10) are out of scope for gm-root.17.6 — start/turn store
// state in-memory and emit deterministic stub replies. The skill plugs
// in via the SkillTurner interface (AttachNewProject), which the
// onboarder bead will implement against the real LLM client. Until
// then a stub turner ships so the SPA keeps working in real-mode.
//
// Atomic ratification (gm-root.17.6 — primary deliverable): the
// transaction lives in newproject_ratify.go. This file owns the
// HTTP plumbing, session state, and the in-memory store.

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/server/httperr"
	"github.com/GembaCore/gemba-core/internal/skills/newproject"
)

// ─── Wire types (re-exported from internal/skills/newproject) ───────
//
// The skill package owns the canonical schema (see
// internal/skills/newproject/types.go). This package re-exports the
// types under the same name so server-package code and tests don't
// have to reach across the import boundary every time, and so the
// SPA's contract (web/src/api/newproject.ts) has a single Go-side
// declaration to mirror.

// DraftBead is the leaf of the plan tree. See
// internal/skills/newproject for the field-level documentation; the
// JSON wire shape is canonical at that location.
type DraftBead = newproject.DraftBead

// DraftEpic is one epic in the plan tree.
type DraftEpic = newproject.DraftEpic

// DraftMilestone is one milestone in the plan tree.
type DraftMilestone = newproject.DraftMilestone

// ChangeRef points at the most-recent skill edit.
type ChangeRef = newproject.ChangeRef

// NewProjectState is the authoritative state for one /new session.
type NewProjectState = newproject.NewProjectState

// emptyNewProjectState returns the skill's canonical EmptyState.
// Keeps the existing call sites working without churn.
func emptyNewProjectState() NewProjectState {
	return newproject.EmptyState()
}

// ─── Wire envelopes ─────────────────────────────────────────────────

type startNewProjectResponse struct {
	SessionID string          `json:"session_id"`
	State     NewProjectState `json:"state"`
	Greeting  string          `json:"greeting"`
}

type turnRequest struct {
	Message string                 `json:"message"`
	Edits   map[string]interface{} `json:"edits,omitempty"`
}

type turnResponse struct {
	State   NewProjectState `json:"state"`
	Reply   string          `json:"reply"`
	ReplyID string          `json:"reply_id"`
	ReplyAt string          `json:"reply_at"`
}

type ratifyRequest struct {
	State NewProjectState `json:"state"`
}

// createRequest is the body for POST /api/v1/newproject/create — the
// lightweight project-creation path that does NOT spawn the Onboarder
// persona or call an LLM. The handler builds a minimal NewProjectState
// (empty Milestones[]) and runs the same atomic ratify transaction
// the conversational flow uses, so the operator gets a project dir,
// git init, .gemba/workspace.toml, and an empty beads DB without ever
// touching a chat client (gm-root.17.13).
type createRequest struct {
	ProjectName string `json:"project_name"`
	Description string `json:"description"`
}

// onboarderProbeResponse is the body of GET /api/v1/onboarder/probe.
// The SPA reads this on board empty-state to decide whether to render
// the "Plan with the Onboarder" CTA. When unavailable, the reason
// carries the canonical operator-facing diagnostic.
type onboarderProbeResponse struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// RatifyResponse is the success envelope POST /ratify returns. The
// SPA owns post-ratify navigation (gm-root.17.7); the server returns
// the new project's filesystem root, its display name, and the seeded
// milestone / epic counts so the handoff screen can render
// "seeded N milestones and M epics" without re-querying the new
// workspace's beads database.
type RatifyResponse struct {
	ProjectPath    string `json:"project_path"`
	ProjectName    string `json:"project_name"`
	MilestoneCount int    `json:"milestone_count"`
	EpicCount      int    `json:"epic_count"`
	// SeedWarnings carries any non-fatal failures from the FTUX seed
	// steps (.gemba/agents.toml, .gemba/personas/, CLAUDE.md — gm-root.24).
	// These are best-effort additions: a failure here does NOT roll back
	// the project — the SPA renders a "partial seed" toast so the
	// operator can hand-fix and proceed. Empty (or omitted) means every
	// seed step succeeded.
	SeedWarnings []string `json:"seed_warnings,omitempty"`
}

// ─── Session store ──────────────────────────────────────────────────

// NewProjectSession is the in-memory record for one /new conversation.
// Lives only in server process memory — refresh / restart / Ctrl-C
// discards it (one-shot persistence is by design; see
// docs/design/newproject.md §One-shot persistence).
type NewProjectSession struct {
	ID        string
	State     NewProjectState
	StartedAt time.Time
}

// NewProjectStore is the persistence interface for /new sessions.
// Mirrors the bootstrap / walk store pattern: callers hand pre-built
// records to mutation methods, the store concerns itself only with
// id + lifecycle.
type NewProjectStore interface {
	// Start allocates a new session with the empty state and returns
	// the persisted record so the handler can read the minted id back
	// without a second Get.
	Start(ctx context.Context) (NewProjectSession, error)

	// Get returns the session by id. ErrNewProjectNotFound when
	// unknown.
	Get(ctx context.Context, id string) (NewProjectSession, error)

	// Update replaces the state of an existing session. Returns
	// ErrNewProjectNotFound if the id is unknown.
	Update(ctx context.Context, id string, state NewProjectState) error

	// Delete drops the session from the store. Used after a
	// successful ratify to free memory; idempotent — deleting an
	// unknown id is not an error.
	Delete(ctx context.Context, id string)
}

// ErrNewProjectNotFound is returned by NewProjectStore when the
// session id is unknown.
var ErrNewProjectNotFound = errors.New("newproject: session not found")

// memoryNewProjectStore is the in-memory implementation. Capacity
// unbounded — sessions are operator-driven (a few per workspace ever)
// so an LRU is not worth the complexity in v1.
type memoryNewProjectStore struct {
	mu  sync.RWMutex
	now func() time.Time
	by  map[string]*NewProjectSession
}

// NewMemoryNewProjectStore returns a fresh in-memory store. cmd/gemba
// serve calls this once at boot and hands the result to
// AttachNewProject.
func NewMemoryNewProjectStore() NewProjectStore {
	return &memoryNewProjectStore{
		now: time.Now,
		by:  map[string]*NewProjectSession{},
	}
}

func (s *memoryNewProjectStore) Start(_ context.Context) (NewProjectSession, error) {
	id := newSessionID()
	rec := &NewProjectSession{
		ID:        id,
		State:     emptyNewProjectState(),
		StartedAt: s.now().UTC(),
	}
	s.mu.Lock()
	s.by[id] = rec
	s.mu.Unlock()
	return *rec, nil
}

func (s *memoryNewProjectStore) Get(_ context.Context, id string) (NewProjectSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.by[id]
	if !ok {
		return NewProjectSession{}, ErrNewProjectNotFound
	}
	return *rec, nil
}

func (s *memoryNewProjectStore) Update(_ context.Context, id string, state NewProjectState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.by[id]
	if !ok {
		return ErrNewProjectNotFound
	}
	rec.State = state
	return nil
}

func (s *memoryNewProjectStore) Delete(_ context.Context, id string) {
	s.mu.Lock()
	delete(s.by, id)
	s.mu.Unlock()
}

// newSessionID mints a 16-byte hex session id with a "newproj-"
// prefix for log-grep convenience.
func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "newproj-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return "newproj-" + hex.EncodeToString(b)
}

// ─── Skill turner (pluggable LLM contact point) ─────────────────────

// SkillTurner is the contract the Onboarder persona host (gm-root.17.10)
// implements: take a state + an operator message + any in-place edits
// and return the next state + a reply for the conversation pane.
//
// Until the real persona host lands, a stub implementation ships so
// the SPA keeps working in real mode. The stub mirrors the e2e fixture's
// deterministic plan-tree mutation so screenshot tests stay stable.
type SkillTurner interface {
	Turn(ctx context.Context, in NewProjectState, message string, edits map[string]interface{}) (out NewProjectState, reply string, err error)
	// Greeting returns the assistant's opening line on /start. Empty
	// is a valid greeting (the SPA renders nothing).
	Greeting() string
	// Probe is invoked on /start before allocating a session and
	// reports whether the turner can serve the request. Real-mode
	// implementations return an error wrapping the operator-facing
	// diagnostic (e.g. "no LLM client configured") so the handler
	// can return 503 with the diagnostic in the body. Stub turners
	// return nil. May be called concurrently.
	Probe(ctx context.Context) error
}

// stubSkillTurner is a deterministic placeholder used until the
// real Onboarder persona ships (gm-root.17.10). It mirrors the e2e
// fixture's behaviour: the first turn seeds a single placeholder
// milestone; subsequent turns append epics under it.
type stubSkillTurner struct{}

// NewStubSkillTurner returns the placeholder turner. Production
// builds use the real Onboarder when AttachNewProject(... real-turner)
// is wired (gm-root.17.10).
func NewStubSkillTurner() SkillTurner { return stubSkillTurner{} }

func (stubSkillTurner) Probe(_ context.Context) error { return nil }

func (stubSkillTurner) Greeting() string {
	return "Hi — what are we building? Tell me the rough shape (web app, library, ops tool, …) and I'll propose milestones, epics, and beads."
}

func (stubSkillTurner) Turn(_ context.Context, in NewProjectState, message string, _ map[string]interface{}) (NewProjectState, string, error) {
	out := in
	out.Turn = in.Turn + 1
	msg := strings.TrimSpace(message)
	if out.ProjectName == "" && msg != "" {
		// First turn seeds a project name from the user's message
		// (truncated to a reasonable length). Real skill replaces this
		// with a parsed intent.
		name := msg
		if len(name) > 48 {
			name = name[:48]
		}
		out.ProjectName = name
		out.Description = msg
	}
	if len(out.Milestones) == 0 {
		out.Milestones = []DraftMilestone{{
			Title:       "M1 — define scope",
			Description: "Initial milestone seeded by the stub Onboarder.",
			Labels:      []string{},
			Skills:      []string{},
			Epics:       []DraftEpic{},
		}}
		out.LastChange = ChangeRef{Path: "milestone:0", Kind: "added", Summary: "Drafted milestone 1."}
		return out, "Drafted 1 milestone. Tell me more and I'll add epics + beads.", nil
	}
	// Subsequent turns append a placeholder epic under the first
	// milestone so the SPA's diff badge has something to render.
	epicIdx := len(out.Milestones[0].Epics)
	out.Milestones[0].Epics = append(out.Milestones[0].Epics, DraftEpic{
		Title:       "E" + newProjectItoa(epicIdx+1) + " — placeholder epic",
		Description: "Stub epic; the real Onboarder will replace this.",
		Labels:      []string{},
		Skills:      []string{},
		Beads:       []DraftBead{},
	})
	out.LastChange = ChangeRef{
		Path:    "milestone:0/epic:" + newProjectItoa(epicIdx),
		Kind:    "added",
		Summary: "Added placeholder epic under milestone 1.",
	}
	return out, "Added a placeholder epic. Edit it in the preview pane or keep typing.", nil
}

// newProjectItoa is a tiny helper for the stub turner — renamed to avoid
// colliding with the package-level itoa in consults_apply.go.
func newProjectItoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ─── Router glue ────────────────────────────────────────────────────

// AttachNewProject binds the /new session store + skill turner +
// ratify executor (gm-root.17.6). Calling with a nil store removes
// the binding (handlers return 503). cmd/gemba serve calls this once
// at boot; tests inject fixtures per case.
//
// turner may be nil — when so, NewStubSkillTurner takes over. ratifier
// may be nil too — when so, /ratify returns 503 (the conversation
// surface still works for SPA preview / screenshot purposes).
func (r *Router) AttachNewProject(store NewProjectStore, turner SkillTurner, ratifier NewProjectRatifier) {
	r.newProjectStore = store
	r.newProjectTurner = turner
	r.newProjectRatifier = ratifier
}

func newProjectAdaptorUnavailable(w http.ResponseWriter) {
	httperr.Write(w, http.StatusServiceUnavailable,
		"adaptor_not_configured", "newproject store not registered")
}

// ─── Handlers ───────────────────────────────────────────────────────

func (r *Router) newProjectStart(w http.ResponseWriter, req *http.Request) {
	if r.newProjectStore == nil {
		newProjectAdaptorUnavailable(w)
		return
	}
	turner := r.newProjectTurner
	if turner == nil {
		turner = NewStubSkillTurner()
	}
	// Spawn-failure path (gm-root.17.10): real-mode turners (the
	// Onboarder persona) return an error here when they can't
	// resolve a chat client. Surface as 503 with the operator-facing
	// diagnostic so the SPA's /new route can render it verbatim
	// without parsing a 500 stack trace.
	if err := turner.Probe(req.Context()); err != nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"no_llm_client", err.Error())
		return
	}
	rec, err := r.newProjectStore.Start(req.Context())
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, startNewProjectResponse{
		SessionID: rec.ID,
		State:     rec.State,
		Greeting:  turner.Greeting(),
	})
}

func (r *Router) newProjectTurn(w http.ResponseWriter, req *http.Request) {
	if r.newProjectStore == nil {
		newProjectAdaptorUnavailable(w)
		return
	}
	turner := r.newProjectTurner
	if turner == nil {
		turner = NewStubSkillTurner()
	}
	id := strings.TrimSpace(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"session id is required")
		return
	}
	var body turnRequest
	if err := decodeJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"invalid JSON body: "+err.Error())
		return
	}
	sess, err := r.newProjectStore.Get(req.Context(), id)
	if err != nil {
		writeNewProjectError(w, err)
		return
	}
	out, reply, err := turner.Turn(req.Context(), sess.State, body.Message, body.Edits)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	if err := r.newProjectStore.Update(req.Context(), id, out); err != nil {
		writeNewProjectError(w, err)
		return
	}
	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, turnResponse{
		State:   out,
		Reply:   reply,
		ReplyID: "reply-" + newSessionID(),
		ReplyAt: now.Format(time.RFC3339Nano),
	})
}

func (r *Router) newProjectRatify(w http.ResponseWriter, req *http.Request) {
	if r.newProjectStore == nil {
		newProjectAdaptorUnavailable(w)
		return
	}
	if r.newProjectRatifier == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured",
			"newproject ratifier not registered")
		return
	}
	id := strings.TrimSpace(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"session id is required")
		return
	}
	var body ratifyRequest
	if err := decodeJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"invalid JSON body: "+err.Error())
		return
	}
	sess, err := r.newProjectStore.Get(req.Context(), id)
	if err != nil {
		writeNewProjectError(w, err)
		return
	}
	// Stale-modal guard: the SPA POSTs the snapshot the operator
	// confirmed in the modal. Reject if the live session state has
	// drifted (turn count differs) so a background turn racing the
	// confirm click cannot commit a tree the operator did not see.
	if body.State.Turn != sess.State.Turn {
		httperr.Write(w, http.StatusConflict, "stale_state",
			"session state changed since the ratify modal opened; reopen and retry")
		return
	}
	resp, err := r.newProjectRatifier.Ratify(req.Context(), body.State)
	if err != nil {
		writeRatifyError(w, err)
		return
	}
	// Successful ratify — drop the session so a stale id can't double-
	// commit on a delayed retry that bypassed the nonce cache (e.g.
	// the operator hits Back and clicks Ratify again).
	r.newProjectStore.Delete(req.Context(), id)
	writeJSON(w, http.StatusOK, resp)
}

// writeNewProjectError maps NewProjectStore errors onto the shared
// envelope. ErrNewProjectNotFound → 404; everything else flows
// through the generic mapper.
func writeNewProjectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNewProjectNotFound):
		httperr.Write(w, http.StatusNotFound, "newproject_not_found", err.Error())
	default:
		httperr.WriteError(w, err)
	}
}

// newProjectCreate is the lightweight project-creation handler. It
// runs the same atomic ratify transaction the conversational flow
// uses, but with an empty Milestones[] and ProjectName/Description
// straight from the request — no session, no Onboarder, no LLM
// (gm-root.17.13). Operators who want to scaffold a project and
// manage beads manually never touch a chat client.
func (r *Router) newProjectCreate(w http.ResponseWriter, req *http.Request) {
	if r.newProjectRatifier == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured",
			"newproject ratifier not registered")
		return
	}
	var body createRequest
	if err := decodeJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"invalid JSON body: "+err.Error())
		return
	}
	name := strings.TrimSpace(body.ProjectName)
	if name == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"project_name is required")
		return
	}
	state := emptyNewProjectState()
	state.ProjectName = name
	state.Description = strings.TrimSpace(body.Description)
	resp, err := r.newProjectRatifier.Ratify(req.Context(), state)
	if err != nil {
		writeRatifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// onboarderProbe reports whether the Onboarder persona host can
// resolve an LLM client. Returns 200 in both available + unavailable
// cases — the body's `available` flag is the operative signal. The
// SPA uses this to gate the optional "Plan with the Onboarder" CTA
// on the board's empty-state without invoking the (heavier)
// /api/v1/newproject/start probe (gm-root.17.13).
func (r *Router) onboarderProbe(w http.ResponseWriter, req *http.Request) {
	turner := r.newProjectTurner
	if turner == nil {
		writeJSON(w, http.StatusOK, onboarderProbeResponse{Available: true})
		return
	}
	if err := turner.Probe(req.Context()); err != nil {
		writeJSON(w, http.StatusOK, onboarderProbeResponse{
			Available: false,
			Reason:    err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, onboarderProbeResponse{Available: true})
}
