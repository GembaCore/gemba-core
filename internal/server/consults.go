// /api/consults handlers (gm-twp2). Read-only surface over the
// in-memory persona consult dispatcher.
//
// LIST returns active + recently-finished consults newest-first; the
// list shape omits the rendered prompt and full validated-line stream
// because those can be many KB and the SPA's /insights/personas only
// needs row-level metadata. GET /api/consults/{id} returns the full
// detail including the composed prompt, raw request, and every
// validated-output line so a consult-detail drawer can render the
// model's structured output as it lands.
//
// Both handlers return 503 when AttachPersonaDispatcher hasn't bound a
// dispatcher yet so a stripped-down test or pre-config-load Router
// degrades cleanly. The audit log on disk is the source of truth for
// closed consults that aged out of the in-memory map; a future slice
// will fall through to it on a cache miss.

package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/persona"
	"github.com/go-chi/chi/v5"
)

// consultSummary is the row shape for /api/consults. Sized for a
// dense list — the operator should be able to scan dozens of in-flight
// consults without each row eating a screen.
type consultSummary struct {
	ID             string            `json:"id"`
	PersonaID      string            `json:"persona_id"`
	SkillID        string            `json:"skill_id"`
	Workspace      string            `json:"workspace"`
	WorkingDir     string            `json:"working_dir,omitempty"`
	RepositoryID   core.RepositoryID `json:"repository_id,omitempty"`
	Status         string            `json:"status"`
	StartedAt      time.Time         `json:"started_at"`
	EndedAt        *time.Time        `json:"ended_at,omitempty"`
	LineCount      int               `json:"line_count"`
	LineErrorCount int               `json:"line_error_count"`
	Model          string            `json:"model,omitempty"`
	LatencyMs      int               `json:"latency_ms,omitempty"`
	Dollars        float64           `json:"dollars,omitempty"`
	Error          string            `json:"error,omitempty"`
}

// consultDetail extends consultSummary with the per-consult payloads
// the SPA's drawer renders. Composed is the rendered prompt envelope
// (system + first user message) so the operator can audit what the
// model was actually told. Lines + LineErrors carry the structured
// output stream and the rejected-line audit trail.
type consultDetail struct {
	consultSummary
	Composed       persona.Composed     `json:"composed"`
	RawRequest     json.RawMessage      `json:"raw_request,omitempty"`
	ValidatedLines []any                `json:"validated_lines"`
	LineErrors     []persona.LineError  `json:"line_errors,omitempty"`
	Tokens         consultTokens        `json:"tokens"`
}

// consultTokens mirrors corepersona.TokenUsage with explicit JSON
// shape. corepersona.TokenUsage's wire form uses {"in", "out"};
// the SPA already speaks {"input", "output", "total"} for the budget
// gauge so we re-shape at the wire boundary and add a derived total
// (the source struct doesn't carry one).
type consultTokens struct {
	Input  int `json:"input"`
	Output int `json:"output"`
	Total  int `json:"total"`
}

func (r *Router) listConsults(w http.ResponseWriter, _ *http.Request) {
	if r.personaDispatcher == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"consults": []consultSummary{},
			"total":    0,
		})
		return
	}
	live := r.personaDispatcher.List()
	out := make([]consultSummary, 0, len(live))
	for _, c := range live {
		out = append(out, summarizeConsult(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"consults": out,
		"total":    len(out),
	})
}

func (r *Router) getConsult(w http.ResponseWriter, req *http.Request) {
	if r.personaDispatcher == nil {
		http.Error(w, "persona dispatcher not configured", http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(req, "id")
	c, ok := r.personaDispatcher.Get(id)
	if !ok {
		// Live-map miss. The audit-log fall-through lands in a
		// follow-up slice — for now the operator sees the 404 and
		// can `cat` the on-disk record.
		http.Error(w, "consult not found", http.StatusNotFound)
		return
	}
	lines := c.ValidatedLines
	if lines == nil {
		lines = []any{}
	}
	writeJSON(w, http.StatusOK, consultDetail{
		consultSummary: summarizeConsult(c),
		Composed:       c.Composed,
		RawRequest:     c.RawRequest,
		ValidatedLines: lines,
		LineErrors:     c.LineErrors,
		Tokens: consultTokens{
			Input:  c.Tokens.In,
			Output: c.Tokens.Out,
			Total:  c.Tokens.In + c.Tokens.Out,
		},
	})
}

func summarizeConsult(c *persona.Consult) consultSummary {
	s := consultSummary{
		ID:             c.ID,
		PersonaID:      c.PersonaID,
		SkillID:        c.SkillID,
		Workspace:      c.Workspace,
		WorkingDir:     c.WorkingDir,
		RepositoryID:   c.RepositoryID,
		Status:         string(c.Status),
		StartedAt:      c.StartedAt,
		LineCount:      len(c.ValidatedLines),
		LineErrorCount: len(c.LineErrors),
		Model:          c.Model,
		LatencyMs:      c.LatencyMs,
		Dollars:        c.Dollars,
		Error:          c.Error,
	}
	if !c.EndedAt.IsZero() {
		ended := c.EndedAt
		s.EndedAt = &ended
	}
	return s
}
