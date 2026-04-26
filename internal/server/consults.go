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
	"errors"
	"io/fs"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MikeBengtson/gemba/internal/core"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
	"github.com/MikeBengtson/gemba/internal/persona"
	"github.com/MikeBengtson/gemba/internal/server/httperr"

	"github.com/go-chi/chi/v5"
)

// detailSource identifies where a consultDetail was loaded from. The
// SPA uses it to render an "archived" badge on audit-log records and
// hide UI affordances (apply / re-emit) that only make sense for
// live consults.
type detailSource string

const (
	sourceLive  detailSource = "live"
	sourceAudit detailSource = "audit"
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
//
// Source identifies whether the detail came from the in-memory
// dispatcher (full shape) or the on-disk audit log (reduced shape —
// composed_persisted=false, validated_lines empty). The SPA uses
// it to render an archived badge instead of pretending an empty
// drawer is fresh.
type consultDetail struct {
	consultSummary
	Source            detailSource        `json:"source"`
	Composed          persona.Composed    `json:"composed"`
	ComposedPersisted bool                `json:"composed_persisted"`
	RawRequest        json.RawMessage     `json:"raw_request,omitempty"`
	ValidatedLines    []any               `json:"validated_lines"`
	LineErrors        []persona.LineError `json:"line_errors,omitempty"`
	// AppliedIdx lists the recommendation indexes the operator
	// applied (POST /api/consults/:id/apply/:idx — separate slice).
	// Only populated for audit-log records; live consults track
	// applies in their own state once the apply endpoint lands.
	AppliedIdx []int         `json:"applied_idx,omitempty"`
	Tokens     consultTokens `json:"tokens"`
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
	if c, ok := r.personaDispatcher.Get(id); ok {
		writeJSON(w, http.StatusOK, detailFromLiveConsult(c))
		return
	}

	// Live-map miss → audit-log fall-through. A consult that
	// finished (success or failure) is removed from memory by
	// Dispatcher.Finish but the audit-log row carries the post-hoc
	// shape: status (derived from Error empty/non-empty), request,
	// tokens, dollars, applied indexes, error string. ValidatedLines
	// + Composed aren't preserved on disk — the operator sees a
	// reduced detail shape with composed_persisted=false so the SPA
	// can render an "archived" badge instead of pretending the live
	// drawer state still exists.
	if log := r.personaDispatcher.AuditLog(); log != nil {
		rec, err := log.Get(id)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, detailFromAuditRecord(rec))
			return
		case errors.Is(err, fs.ErrNotExist):
			// fall through to 404 below
		default:
			httperr.WriteError(w, err)
			return
		}
	}

	http.Error(w, "consult not found", http.StatusNotFound)
}

// detailFromLiveConsult renders the full in-memory consult shape,
// including Composed prompt + ValidatedLines + LineErrors. The drawer
// sees everything because the operator may want to re-read the prompt
// the model saw or step through the validated lines as they arrive.
func detailFromLiveConsult(c *persona.Consult) consultDetail {
	lines := c.ValidatedLines
	if lines == nil {
		lines = []any{}
	}
	return consultDetail{
		consultSummary:    summarizeConsult(c),
		Source:            sourceLive,
		Composed:          c.Composed,
		ComposedPersisted: true,
		RawRequest:        c.RawRequest,
		ValidatedLines:    lines,
		LineErrors:        c.LineErrors,
		Tokens: consultTokens{
			Input:  c.Tokens.In,
			Output: c.Tokens.Out,
			Total:  c.Tokens.In + c.Tokens.Out,
		},
	}
}

// detailFromAuditRecord renders an archived consult — what's left
// after Finish removed the in-memory entry. Composed + ValidatedLines
// are NOT preserved on disk so the response surfaces empty slots with
// composed_persisted=false; the drawer can render a "consult ended,
// detail archived" placeholder instead of an empty composed prompt
// that looks like a bug.
func detailFromAuditRecord(rec *corepersona.PersonaConsultRecord) consultDetail {
	status := "completed"
	if rec.Error != "" {
		status = "failed"
	}
	endedAt := rec.EndedAt
	endedPtr := &endedAt
	return consultDetail{
		consultSummary: consultSummary{
			ID:        rec.ID,
			PersonaID: rec.PersonaID,
			SkillID:   rec.SkillID,
			Workspace: rec.Workspace,
			Status:    status,
			StartedAt: rec.StartedAt,
			EndedAt:   endedPtr,
			Model:     rec.Model,
			LatencyMs: rec.LatencyMs,
			Dollars:   rec.Dollars,
			Error:     rec.Error,
		},
		Source:            sourceAudit,
		Composed:          persona.Composed{},
		ComposedPersisted: false,
		RawRequest:        rec.Request,
		ValidatedLines:    []any{},
		LineErrors:        nil,
		AppliedIdx:        rec.AppliedIdx,
		Tokens: consultTokens{
			Input:  rec.Tokens.In,
			Output: rec.Tokens.Out,
			Total:  rec.Tokens.In + rec.Tokens.Out,
		},
	}
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
