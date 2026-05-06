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
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
	"github.com/GembaCore/gemba-core/internal/speckit"
)

type interactionKind string

const (
	interactionKindPMConsult interactionKind = "pm_consult"
)

type interactionScopeType string

const (
	interactionScopeProject    interactionScopeType = "project"
	interactionScopeBootstrap  interactionScopeType = "bootstrap"
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
	Context    string               `json:"context,omitempty"`
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

type interactionQuickReply struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Message string `json:"message"`
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
	ID               string                  `json:"id"`
	Kind             interactionKind         `json:"kind"`
	Status           interactionStatus       `json:"status"`
	UIHost           string                  `json:"ui_host"`
	RuntimeHost      interactionRuntimeHost  `json:"runtime_host"`
	RuntimeLabel     string                  `json:"runtime_label"`
	Scope            interactionScope        `json:"scope"`
	Messages         []interactionMessage    `json:"messages"`
	SuggestedActions []interactionAction     `json:"suggested_actions"`
	QuickReplies     []interactionQuickReply `json:"quick_replies,omitempty"`
	Draft            *interactionDraft       `json:"draft,omitempty"`
	Evidence         []interactionEvidence   `json:"evidence,omitempty"`
	DecisionLog      []interactionDecision   `json:"decision_log,omitempty"`
	Capabilities     []string                `json:"capabilities"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

type ensureInteractionRequest struct {
	Kind  interactionKind  `json:"kind"`
	Scope interactionScope `json:"scope"`
}

type turnInteractionRequest struct {
	ID      string `json:"id"`
	Message string `json:"message"`
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

func (s *interactionStore) turn(id, message string, now time.Time) (interactionSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.by[id]
	if !ok {
		return interactionSession{}, ErrNewProjectNotFound
	}
	nextIndex := len(rec.Messages) + 1
	rec.Messages = append(rec.Messages, interactionMessage{
		ID:   fmt.Sprintf("operator-%d", nextIndex),
		Role: "operator",
		Body: message,
		At:   now.Format(time.RFC3339),
	})
	rec.Messages = append(rec.Messages, interactionMessage{
		ID:   fmt.Sprintf("assistant-%d", nextIndex+1),
		Role: "assistant",
		Body: interactionReply(rec.Scope, message),
		At:   now.Format(time.RFC3339),
	})
	rec.UpdatedAt = now
	s.by[id] = rec
	return rec, nil
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
		QuickReplies:     defaultInteractionQuickReplies(scope.Type),
		Draft:            defaultInteractionDraft(scope.Type),
		Evidence:         evidence,
		Capabilities:     interactionCapabilities(host),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	writeJSON(w, http.StatusOK, r.interactionStore.ensure(rec))
}

func (r *Router) turnInteraction(w http.ResponseWriter, req *http.Request) {
	var body turnInteractionRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "invalid_body", "request body must be JSON {id, message}")
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	body.Message = strings.TrimSpace(body.Message)
	if body.ID == "" || body.Message == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "id and message are required")
		return
	}
	rec, err := r.interactionStore.turn(body.ID, body.Message, time.Now().UTC())
	if err != nil {
		httperr.Write(w, http.StatusNotFound, "not_found", "interaction not found")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func validInteractionScope(t interactionScopeType) bool {
	switch t {
	case interactionScopeProject, interactionScopeBootstrap, interactionScopeMilestone, interactionScopeEpic, interactionScopeWorkItem,
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
	if scope.Type == interactionScopeBootstrap {
		return r.enrichBootstrapInteractionScope(req, scope)
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

func (r *Router) enrichBootstrapInteractionScope(req *http.Request, scope interactionScope) (interactionScope, []interactionEvidence) {
	scanner := speckit.NewScanner(r.specKitRoot())
	feature, err := scanner.Load(req.Context(), scope.ID)
	if err != nil {
		return scope, nil
	}
	if scope.Title == "" {
		scope.Title = feature.Title
	}
	var draft *speckit.SyncDraft
	if wp := r.host.WorkPlane(); wp != nil {
		if d, err := speckit.DraftFeature(req.Context(), wp, feature); err == nil {
			draft = &d
		}
	}
	scope.Context = bootstrapInteractionContext(feature, draft)
	evidence := []interactionEvidence{{
		ID:    "spec-kit:" + feature.ID,
		Label: "Spec Kit feature: " + feature.Directory,
	}}
	if feature.SpecPath != "" {
		evidence = append(evidence, interactionEvidence{ID: "spec:" + feature.ID, Label: "spec.md: " + feature.SpecPath})
	}
	if feature.PlanPath != "" {
		evidence = append(evidence, interactionEvidence{ID: "plan:" + feature.ID, Label: "plan.md: " + feature.PlanPath})
	}
	if feature.TasksPath != "" {
		evidence = append(evidence, interactionEvidence{ID: "tasks:" + feature.ID, Label: "tasks.md: " + feature.TasksPath})
	}
	return scope, evidence
}

func bootstrapInteractionContext(feature speckit.Feature, draft *speckit.SyncDraft) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Provider: Spec Kit\nFeature: %s (%s)\nDirectory: %s\n", feature.Title, feature.ID, feature.Directory)
	if feature.SpecPath != "" {
		fmt.Fprintf(&b, "Spec: %s\n", feature.SpecPath)
	}
	if feature.PlanPath != "" {
		fmt.Fprintf(&b, "Plan: %s\n", feature.PlanPath)
	}
	if feature.TasksPath != "" {
		fmt.Fprintf(&b, "Tasks: %s\n", feature.TasksPath)
	}
	if len(feature.Spec.UserStories) > 0 {
		b.WriteString("\nSpec Kit user stories:\n")
		for _, story := range feature.Spec.UserStories {
			fmt.Fprintf(&b, "- %s", story.ID)
			if story.Priority != "" {
				fmt.Fprintf(&b, " (%s)", story.Priority)
			}
			fmt.Fprintf(&b, ": %s\n", story.Title)
			for _, scenario := range story.AcceptanceScenarios {
				fmt.Fprintf(&b, "  acceptance: %s\n", scenario)
			}
		}
	}
	if len(feature.Spec.AcceptanceScenarios) > 0 {
		b.WriteString("\nFeature acceptance scenarios:\n")
		for _, scenario := range feature.Spec.AcceptanceScenarios {
			fmt.Fprintf(&b, "- %s\n", scenario)
		}
	}
	if len(feature.Spec.FunctionalRequirements) > 0 {
		b.WriteString("\nFunctional requirements:\n")
		for _, req := range feature.Spec.FunctionalRequirements {
			fmt.Fprintf(&b, "- %s\n", req)
		}
	}
	if len(feature.Tasks) > 0 {
		b.WriteString("\nSpec Kit tasks:\n")
		for _, task := range feature.Tasks {
			fmt.Fprintf(&b, "- %s", task.ID)
			if task.Parallel {
				b.WriteString(" [P]")
			}
			if task.StoryID != "" {
				fmt.Fprintf(&b, " [%s]", task.StoryID)
			}
			fmt.Fprintf(&b, ": %s", task.Title)
			if task.Phase != "" {
				fmt.Fprintf(&b, " (%s)", task.Phase)
			}
			b.WriteString("\n")
		}
	}
	if draft != nil {
		fmt.Fprintf(&b, "\nPlan hash: %s\n", draft.Plan.Hash)
		fmt.Fprintf(&b, "Change plan: %d create, %d update, %d delete\n", draft.Plan.Counts.Create, draft.Plan.Counts.Update, draft.Plan.Counts.Delete)
		if len(draft.Items) > 0 {
			b.WriteString("Draft Beads tree:\n")
			for _, item := range draft.Items {
				parent := ""
				for _, rel := range item.Relationships {
					if rel.Kind == core.RelParentChild && rel.From != "" {
						parent = string(rel.From)
						break
					}
				}
				if parent != "" {
					fmt.Fprintf(&b, "- %s %s: %s (parent: %s)\n", item.Kind, item.ID, item.Title, parent)
				} else {
					fmt.Fprintf(&b, "- %s %s: %s\n", item.Kind, item.ID, item.Title)
				}
			}
		}
	}
	return strings.TrimSpace(b.String())
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
	if scope.Type == interactionScopeBootstrap {
		context := strings.TrimSpace(scope.Context)
		if context == "" {
			context = "No provider context loaded yet. Ask the user to reopen the draft after the bootstrap pack is available."
		}
		return []interactionMessage{
			{ID: "system-1", Role: "system", Body: "Goal: translate bootstrap input into Beads through a first-pass automatic decomposition, then guided editing, then a finished ratified draft set. The user is reviewing milestones, epics, stories, and beads before any database mutation. Keep draft Beads uncommitted until the user ratifies them into a selected database or exports JSONL. Use the full provider context below when responding; preserve the user's perspective on the project.\n\n" + context, At: now},
			{ID: "assistant-1", Role: "assistant", Body: "Ready to review bootstrap draft " + title + ". The current draft is not stored in Beads yet; it is a staged decomposition for you to approve, reshape with me, edit manually, export as JSONL, or ratify into a database when it reflects your view of the project.", At: now},
		}
	}
	return []interactionMessage{
		{ID: "system-1", Role: "system", Body: authority, At: now},
		{ID: "assistant-1", Role: "assistant", Body: "Ready to work with " + string(scope.Type) + " " + title + ".", At: now},
	}
}

func defaultInteractionQuickReplies(scope interactionScopeType) []interactionQuickReply {
	if scope != interactionScopeBootstrap {
		return nil
	}
	return []interactionQuickReply{
		{ID: "looks-good", Label: "Looks good", Message: "This draft looks good. Help me do a final readiness check before I ratify it."},
		{ID: "change-things", Label: "I want changes", Message: "I want to change some things. Review the draft as a batch and suggest what should be renamed, split, merged, or clarified."},
		{ID: "edit-board", Label: "I'll edit on board", Message: "I'll edit on the board. Keep track of the goal and call out anything I should verify before ratifying."},
		{ID: "export-jsonl", Label: "Export JSONL", Message: "I want to export this draft as Beads-compatible JSONL instead of committing it to a database right now."},
		{ID: "need-questions", Label: "Ask questions", Message: "Ask me any clarifying questions needed before this draft becomes milestones, epics, and beads."},
	}
}

func interactionReply(scope interactionScope, message string) string {
	if scope.Type == interactionScopeBootstrap {
		lower := strings.ToLower(message)
		switch {
		case strings.Contains(lower, "jsonl"):
			return "Got it. Keep this draft set unratified; export remains a file operation until you choose a database target and commit."
		case strings.Contains(lower, "split") || strings.Contains(lower, "merge") || strings.Contains(lower, "rename"):
			return "Captured as batch-shaping guidance. Review the staged bead titles and descriptions, then apply the edits to the draft set before ratifying."
		case strings.Contains(lower, "approve") || strings.Contains(lower, "done"):
			return "When the staged set matches your intent, ratify it into the selected Beads database or export it as JSONL. Until then it remains a draft only."
		default:
			return "Captured. I will treat that as guidance for the bootstrap draft set, not as permission to write Beads yet."
		}
	}
	return "Captured. This interaction now has your latest note in context for the next action."
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

func defaultInteractionDraft(scope interactionScopeType) *interactionDraft {
	if scope == interactionScopeBootstrap {
		return &interactionDraft{
			Title:   "Bootstrap Review Goal",
			Summary: "Translate the bootstrap input into a draft Beads decomposition, shape it through guided review and manual edits, then ratify a finished set that represents the operator perspective.",
			Bullets: []string{
				"Draft beads are not stored in a Beads database until ratified.",
				"The review preserves provider context, including Spec Kit user stories, tasks, acceptance criteria, and draft item tree.",
				"The final output is a coherent set of milestones, epics, stories, and beads ready for database commit or JSONL export.",
			},
		}
	}
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
		return []string{"input.send", "suggested_actions.apply", "ratify"}
	}
}
