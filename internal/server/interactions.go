// /api/v1/interactions:* — shared operator interaction contract
// (gm-ddpy).
//
// Interactions are transcript-bearing operator exchanges hosted by the
// SPA shell while the active orchestration plane owns any backing
// runtime. This first server slice makes the contract real and
// process-local; follow-up work can persist ratified decisions and
// transcript frames into the WorkPlane.

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

type interactionKind string

const (
	interactionKindPMConsult interactionKind = "pm_consult"
)

type interactionScopeType string

const (
	interactionScopeProject    interactionScopeType = "project"
	interactionScopeMilestone  interactionScopeType = "milestone"
	interactionScopeEpic       interactionScopeType = "epic"
	interactionScopeWorkItem   interactionScopeType = "workitem"
	interactionScopeSession    interactionScopeType = "session"
	interactionScopeEscalation interactionScopeType = "escalation"
	interactionScopeWalk       interactionScopeType = "walk"
)

type interactionStatus string

const (
	interactionWaiting interactionStatus = "waiting_on_operator"
)

type interactionRuntimeHost string

const (
	interactionRuntimeNative       interactionRuntimeHost = "native"
	interactionRuntimeCodex        interactionRuntimeHost = "codex"
	interactionRuntimeClaude       interactionRuntimeHost = "claude"
	interactionRuntimeGasTownMayor interactionRuntimeHost = "gastown_mayor"
	interactionRuntimeGasTownCrew  interactionRuntimeHost = "gastown_crew"
	interactionRuntimeServer       interactionRuntimeHost = "server_persona"
)

type interactionScope struct {
	Type       interactionScopeType `json:"type"`
	ID         string               `json:"id"`
	Title      string               `json:"title,omitempty"`
	Breadcrumb []interactionCrumb   `json:"breadcrumb,omitempty"`
}

type interactionCrumb struct {
	Type  interactionScopeType `json:"type"`
	ID    string               `json:"id"`
	Label string               `json:"label"`
}

type interactionMessage struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Body string `json:"body"`
	At   string `json:"at,omitempty"`
}

type interactionAction struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Description    string `json:"description"`
	DisabledReason string `json:"disabled_reason,omitempty"`
}

type interactionDraft struct {
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Bullets []string `json:"bullets,omitempty"`
}

type interactionDecision struct {
	ID        string `json:"id"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale,omitempty"`
	Outcome   string `json:"outcome"`
	DecidedAt string `json:"decided_at,omitempty"`
}

type interactionEvidence struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Href  string `json:"href,omitempty"`
}

type interactionSession struct {
	ID               string                 `json:"id"`
	Kind             interactionKind        `json:"kind"`
	Status           interactionStatus      `json:"status"`
	UIHost           string                 `json:"ui_host"`
	RuntimeHost      interactionRuntimeHost `json:"runtime_host"`
	RuntimeLabel     string                 `json:"runtime_label"`
	Scope            interactionScope       `json:"scope"`
	Messages         []interactionMessage   `json:"messages"`
	SuggestedActions []interactionAction    `json:"suggested_actions"`
	Draft            *interactionDraft      `json:"draft,omitempty"`
	Evidence         []interactionEvidence  `json:"evidence,omitempty"`
	DecisionLog      []interactionDecision  `json:"decision_log,omitempty"`
	Capabilities     []string               `json:"capabilities"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

type ensureInteractionRequest struct {
	Kind  interactionKind  `json:"kind"`
	Scope interactionScope `json:"scope"`
}

type interactionStore struct {
	mu sync.Mutex
	by map[string]interactionSession
}

func newInteractionStore() *interactionStore {
	return &interactionStore{by: map[string]interactionSession{}}
}

func (s *interactionStore) ensure(rec interactionSession) interactionSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.by[rec.ID]; ok {
		return existing
	}
	s.by[rec.ID] = rec
	return rec
}

func (r *Router) ensureInteraction(w http.ResponseWriter, req *http.Request) {
	var body ensureInteractionRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "invalid_body", "request body must be JSON {kind, scope}")
		return
	}
	if body.Kind == "" {
		body.Kind = interactionKindPMConsult
	}
	if body.Scope.ID == "" || body.Scope.Type == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "scope.type and scope.id are required")
		return
	}
	if !validInteractionScope(body.Scope.Type) {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "unknown interaction scope type")
		return
	}

	scope, evidence := r.enrichInteractionScope(req, body.Scope)
	host, label := r.interactionRuntime(scope.Type)
	now := time.Now().UTC()
	rec := interactionSession{
		ID:               interactionID(body.Kind, scope),
		Kind:             body.Kind,
		Status:           interactionWaiting,
		UIHost:           "rhp",
		RuntimeHost:      host,
		RuntimeLabel:     label,
		Scope:            scope,
		Messages:         defaultInteractionMessages(scope, host),
		SuggestedActions: defaultInteractionActions(host, r.host != nil && r.host.OrchestrationPlane() != nil),
		Draft:            defaultInteractionDraft(),
		Evidence:         evidence,
		Capabilities:     interactionCapabilities(host),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	writeJSON(w, http.StatusOK, r.interactionStore.ensure(rec))
}

func validInteractionScope(t interactionScopeType) bool {
	switch t {
	case interactionScopeProject, interactionScopeMilestone, interactionScopeEpic, interactionScopeWorkItem,
		interactionScopeSession, interactionScopeEscalation, interactionScopeWalk:
		return true
	default:
		return false
	}
}

func (r *Router) enrichInteractionScope(req *http.Request, scope interactionScope) (interactionScope, []interactionEvidence) {
	if r.host == nil {
		return scope, nil
	}
	wp := r.host.WorkPlane()
	if wp == nil {
		return scope, nil
	}
	if scope.Type != interactionScopeWorkItem && scope.Type != interactionScopeEpic && scope.Type != interactionScopeMilestone {
		return scope, nil
	}
	item, err := wp.GetWorkItem(req.Context(), core.WorkItemID(scope.ID))
	if err != nil {
		return scope, nil
	}
	itemScope := scope
	itemScope.ID = string(item.ID)
	itemScope.Title = item.Title
	switch item.Kind {
	case core.KindMilestone:
		itemScope.Type = interactionScopeMilestone
	case "epic":
		itemScope.Type = interactionScopeEpic
	default:
		itemScope.Type = interactionScopeWorkItem
	}
	itemScope.Breadcrumb = []interactionCrumb{{
		Type:  itemScope.Type,
		ID:    string(item.ID),
		Label: item.Title,
	}}
	return itemScope, evidenceFromWorkItem(item)
}

func evidenceFromWorkItem(item core.WorkItem) []interactionEvidence {
	if len(item.Evidence) == 0 {
		return nil
	}
	out := make([]interactionEvidence, 0, len(item.Evidence))
	for _, ev := range item.Evidence {
		label := string(ev.Kind)
		switch {
		case strings.TrimSpace(ev.Summary) != "":
			label += ": " + ev.Summary
		case strings.TrimSpace(ev.Ref) != "":
			label += ": " + ev.Ref
		case strings.TrimSpace(ev.Source) != "":
			label += ": " + ev.Source
		}
		out = append(out, interactionEvidence{ID: ev.ID, Label: label})
	}
	return out
}

func (r *Router) interactionRuntime(scope interactionScopeType) (interactionRuntimeHost, string) {
	if r.host == nil || r.host.OrchestrationPlane() == nil {
		return interactionRuntimeServer, "Server persona"
	}
	adaptor := r.host.OrchestrationPlane().Describe().AdaptorID
	switch adaptor {
	case "gastown":
		if scope == interactionScopeWorkItem || scope == interactionScopeSession {
			return interactionRuntimeGasTownCrew, "Gas Town crew"
		}
		return interactionRuntimeGasTownMayor, "Gas Town mayor"
	case "native":
		return interactionRuntimeNative, "Native session"
	case "codex":
		return interactionRuntimeCodex, "Codex session"
	case "claude":
		return interactionRuntimeClaude, "Claude session"
	default:
		return interactionRuntimeServer, "Server persona"
	}
}

func interactionID(kind interactionKind, scope interactionScope) string {
	return string(kind) + ":" + string(scope.Type) + ":" + scope.ID
}

func defaultInteractionMessages(scope interactionScope, host interactionRuntimeHost) []interactionMessage {
	now := time.Now().UTC().Format(time.RFC3339)
	authority := "Gemba hosts the operator cockpit; the active orchestration plane owns runtime execution."
	if host == interactionRuntimeGasTownMayor || host == interactionRuntimeGasTownCrew {
		authority = "Gemba hosts the operator cockpit; Gas Town owns the mayor or crew runtime."
	}
	title := scope.Title
	if title == "" {
		title = scope.ID
	}
	return []interactionMessage{
		{ID: "system-1", Role: "system", Body: authority, At: now},
		{ID: "assistant-1", Role: "assistant", Body: "Ready to work with " + string(scope.Type) + " " + title + ".", At: now},
	}
}

func defaultInteractionActions(host interactionRuntimeHost, hasOrchestration bool) []interactionAction {
	dispatchReason := ""
	if !hasOrchestration {
		dispatchReason = "No orchestration plane is bound for this workspace."
	}
	return []interactionAction{
		{
			ID:             "refine",
			Label:          "Refine scope",
			Description:    "Ask the PM persona to clarify acceptance criteria and risks.",
			DisabledReason: "Persona-backed refinement is not wired to the shared interaction API yet.",
		},
		{
			ID:             "dispatch",
			Label:          "Dispatch runtime",
			Description:    dispatchDescription(host),
			DisabledReason: dispatchReason,
		},
		{
			ID:             "record-decision",
			Label:          "Record decision",
			Description:    "Capture the operator choice as a decision/evidence entry.",
			DisabledReason: "Decision persistence is pending the shared interaction API.",
		},
	}
}

func dispatchDescription(host interactionRuntimeHost) string {
	switch host {
	case interactionRuntimeGasTownMayor:
		return "Request a Gas Town mayor session for the next action."
	case interactionRuntimeGasTownCrew:
		return "Request a Gas Town crew session for the next action."
	case interactionRuntimeNative:
		return "Start work through the native runtime."
	case interactionRuntimeCodex:
		return "Start work through the Codex runtime."
	case interactionRuntimeClaude:
		return "Start work through the Claude runtime."
	default:
		return "Start work through the active runtime."
	}
}

func defaultInteractionDraft() *interactionDraft {
	return &interactionDraft{
		Title:   "Working Brief",
		Summary: "Use this tab for scoped clarification, triage, and runtime supervision without leaving board context.",
		Bullets: []string{
			"Transcript-bearing exchanges share one contract across onboarding, PM consults, escalations, walks, and session supervision.",
			"The UI host remains the RHP while native, Codex, Claude, or Gas Town owns the runtime lifecycle.",
			"Structured actions can ratify changes, dispatch sessions, attach evidence, or resolve escalations as backend hooks land.",
		},
	}
}

func interactionCapabilities(host interactionRuntimeHost) []string {
	switch host {
	case interactionRuntimeGasTownMayor:
		return []string{"transcript.peek", "input.send", "suggested_actions.apply", "runtime.mayor"}
	case interactionRuntimeGasTownCrew:
		return []string{"transcript.peek", "input.send", "suggested_actions.apply", "runtime.crew"}
	case interactionRuntimeNative, interactionRuntimeCodex, interactionRuntimeClaude:
		return []string{"transcript.peek", "input.send", "pause.resume", "evidence.attach"}
	default:
		return []string{"suggested_actions.apply", "ratify"}
	}
}
