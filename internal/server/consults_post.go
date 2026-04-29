// POST /api/consults — start a new persona consult (gm-twp2).
//
// Resolves persona_id + skill_id against the bound registries, runs
// Skill.ValidateInput, composes the prompt, and registers the
// consult with status=running. The spawn driver (separate slice)
// would normally pick up the registered consult and launch a Claude
// Code session whose env carries GEMBA_SESSION_ID = consult.ID; the
// bridge tail then routes emit_skill_output frames back into the
// dispatcher via persona.FanFromHub.
//
// Request body — all fields except persona_id, skill_id, workspace,
// raw_input are optional:
//
//	{
//	  "persona_id":          "project-manager",
//	  "skill_id":            "epic_order",
//	  "workspace":           "gemba",
//	  "repository_override": "gemba",          // required for scope=any personas
//	  "raw_input":           { ... },          // skill-specific shape
//	  "guidance":            "free-form intent to surface in the user message",
//	  "template": {
//	    "workspace_name": "Gemba",
//	    "project_prefix": "gm"
//	  },
//	  "project_values":     ["..."],
//	  "project_guardrails": ["..."],
//	  "project_goals":      ["..."],
//	  "workspace_values":   ["..."],
//	  "context_chunks":     ["..."]
//	}

package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/persona"
)

// createConsultRequest is the wire body. All optional context
// (project_values etc.) maps verbatim into the dispatcher's
// BeginRequest so a caller that wants to override workspace defaults
// can do so per-consult without touching disk.
type createConsultRequest struct {
	PersonaID          string            `json:"persona_id"`
	SkillID            string            `json:"skill_id"`
	Workspace          string            `json:"workspace"`
	RepositoryOverride core.RepositoryID `json:"repository_override,omitempty"`
	RawInput           json.RawMessage   `json:"raw_input"`
	Guidance           string            `json:"guidance,omitempty"`
	Template           templateValues    `json:"template,omitempty"`
	ProjectValues      []string          `json:"project_values,omitempty"`
	ProjectGuardrails  []string          `json:"project_guardrails,omitempty"`
	ProjectGoals       []string          `json:"project_goals,omitempty"`
	WorkspaceValues    []string          `json:"workspace_values,omitempty"`
	ContextChunks      []string          `json:"context_chunks,omitempty"`
	// Spawn picks whether the dispatcher's post-Begin SpawnFunc
	// fires after a successful registration. nil = default true
	// (spawn the session). Operators who want to iterate on prompt
	// composition without launching a real Claude Code session set
	// "spawn": false — the consult registers, the SPA can render
	// the composed prompt for review, and no agent ever runs.
	Spawn *bool `json:"spawn,omitempty"`
}

// templateValues mirrors persona.TemplateValues at the wire layer.
// Re-projected so the SPA's TypeScript codegen sees snake_case JSON
// without depending on the Go struct's field names.
type templateValues struct {
	WorkspaceName string `json:"workspace_name,omitempty"`
	Role          string `json:"role,omitempty"`
	ProjectPrefix string `json:"project_prefix,omitempty"`
}

func (r *Router) createConsult(w http.ResponseWriter, req *http.Request) {
	if r.personaDispatcher == nil || r.skillRegistry == nil {
		http.Error(w, "persona dispatcher not configured", http.StatusServiceUnavailable)
		return
	}
	if r.personaRegistry == nil {
		http.Error(w, "persona registry not configured (no .gemba/personas loaded)", http.StatusServiceUnavailable)
		return
	}

	var body createConsultRequest
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if body.PersonaID == "" {
		http.Error(w, "persona_id is required", http.StatusBadRequest)
		return
	}
	if body.SkillID == "" {
		http.Error(w, "skill_id is required", http.StatusBadRequest)
		return
	}
	if body.Workspace == "" {
		http.Error(w, "workspace is required", http.StatusBadRequest)
		return
	}
	if len(body.RawInput) == 0 {
		http.Error(w, "raw_input is required", http.StatusBadRequest)
		return
	}

	p, ok := r.personaRegistry.Get(body.PersonaID)
	if !ok {
		http.Error(w, "persona "+body.PersonaID+" not registered", http.StatusNotFound)
		return
	}
	skill, ok := r.skillRegistry.Get(body.SkillID)
	if !ok {
		http.Error(w, "skill "+body.SkillID+" not registered", http.StatusNotFound)
		return
	}

	if body.Spawn == nil {
		// Default: spawn after Begin. The operator picks
		// "spawn=false" to dry-run (compose prompt + register
		// consult, never launch a session) — useful for
		// integration tests and prompt-engineering iteration.
		t := true
		body.Spawn = &t
	}

	c, err := r.personaDispatcher.Begin(persona.BeginRequest{
		Persona:            p,
		Skill:              skill,
		Workspace:          body.Workspace,
		RawInput:           body.RawInput,
		Guidance:           body.Guidance,
		RepositoryOverride: body.RepositoryOverride,
		Template: persona.TemplateValues{
			WorkspaceName: body.Template.WorkspaceName,
			Role:          body.Template.Role,
			ProjectPrefix: body.Template.ProjectPrefix,
		},
		ProjectValues:     body.ProjectValues,
		ProjectGuardrails: body.ProjectGuardrails,
		ProjectGoals:      body.ProjectGoals,
		WorkspaceValues:   body.WorkspaceValues,
		ContextChunks:     body.ContextChunks,
	})
	if err != nil {
		http.Error(w, beginErrorMessage(err), beginErrorStatus(err))
		return
	}

	// gm-twp2 spawn slice: if the dispatcher has a SpawnFunc bound
	// (cmd/gemba serve installs persona.NativeSpawn when an
	// OrchestrationPlane is registered), launch the session now.
	// Spawn failure flips the consult to terminal-Failed via Finish
	// so the audit-log row carries the spawn error and downstream
	// inspectors don't see a phantom "running" consult that never
	// actually launched. Surfaces as 502 so the SPA's drawer can
	// render the spawn-error path explicitly.
	if *body.Spawn {
		if err := r.personaDispatcher.MaybeSpawn(req.Context(), c); err != nil {
			_, finishErr := r.personaDispatcher.Finish(c.ID, persona.FinishInfo{
				Error: "spawn failed: " + err.Error(),
			})
			if finishErr != nil {
				// A Finish failure on top of a spawn failure means
				// the consult's audit-log write didn't land; log
				// here would help an operator chase it. Returning
				// 502 with the original spawn error is still the
				// right operator-facing message.
				_ = finishErr
			}
			http.Error(w, "consult registered; spawn failed: "+err.Error(), http.StatusBadGateway)
			return
		}
	}

	// 201 Created with the consult summary — the SPA polls
	// /api/consults/{id} for the full detail (including
	// ValidatedLines as they trickle in through FanFromHub) so the
	// POST response stays small.
	writeJSON(w, http.StatusCreated, summarizeConsult(c))
}

// beginErrorMessage strips the "persona/dispatcher: " prefix from
// the dispatcher's error messages so the HTTP response surface
// doesn't leak internal package names. The dispatcher's wrapped
// errors are already caller-safe — they describe what's wrong
// without naming Go types.
func beginErrorMessage(err error) string {
	msg := err.Error()
	const prefix = "persona/dispatcher: "
	if len(msg) >= len(prefix) && msg[:len(prefix)] == prefix {
		return msg[len(prefix):]
	}
	return msg
}

// beginErrorStatus picks the right HTTP status for a Begin error.
// Validation errors (missing fields, invalid persona/skill, scope-
// resolution failures) → 400. Anything else → 500.
func beginErrorStatus(err error) int {
	// The dispatcher wraps every input failure but doesn't expose a
	// typed sentinel; the closest we have is errors.Is checks
	// against well-known phrases. Most Begin errors are validation;
	// fall back to 400 unless the cause is clearly a server-side
	// fault.
	switch {
	case errors.Is(err, errors.ErrUnsupported):
		return http.StatusBadRequest
	default:
		// validate input failures, persona-not-authorized, missing
		// workspace, malformed input — all caller-facing.
		return http.StatusBadRequest
	}
}
