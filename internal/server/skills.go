// /api/skills handlers (gm-twp2). Read-only surface over the bound
// SkillRegistry. The SPA's persona configuration views and the future
// /plan view's "Recommend order" button render skill metadata + JSON
// schema from these endpoints; the dispatcher uses the registry
// directly, not the HTTP surface.
//
// All handlers return 503 when AttachPersonaDispatcher hasn't bound a
// registry yet so a stripped-down test or pre-config-load Router
// degrades cleanly instead of panicking on a nil deref.

package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
)

// skillSummary is the list-row shape — enough for a picker without
// the full output_schema (the schema endpoint serves that on demand).
type skillSummary struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	OutputToolName  string `json:"output_tool_name,omitempty"`
	HasOutputSchema bool   `json:"has_output_schema"`
}

// skillDetail extends skillSummary with the OutputTool description.
// The InputSchema itself is served through the dedicated
// /api/skills/{id}/output_schema.json endpoint so callers that want
// just the metadata don't pay the marshal cost on every list refresh.
type skillDetail struct {
	skillSummary
	OutputToolDescription string `json:"output_tool_description,omitempty"`
}

func (r *Router) listSkills(w http.ResponseWriter, _ *http.Request) {
	if r.skillRegistry == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"skills": []skillSummary{},
			"total":  0,
		})
		return
	}
	ids := r.skillRegistry.List()
	out := make([]skillSummary, 0, len(ids))
	for _, id := range ids {
		s, ok := r.skillRegistry.Get(id)
		if !ok {
			continue
		}
		out = append(out, summarizeSkill(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"skills": out,
		"total":  len(out),
	})
}

func (r *Router) getSkill(w http.ResponseWriter, req *http.Request) {
	if r.skillRegistry == nil {
		http.Error(w, "skill registry not configured", http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(req, "id")
	s, ok := r.skillRegistry.Get(id)
	if !ok {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, skillDetail{
		skillSummary:          summarizeSkill(s),
		OutputToolDescription: s.OutputTool().Description,
	})
}

// getSkillOutputSchema returns the JSON Schema describing the skill's
// structured-output array. application/schema+json is the IETF media
// type for JSON Schema documents (RFC 8927). When the skill ships the
// constrained-generation fallback (no output tool, IsZero), we return
// 404 — there is nothing to schematize.
func (r *Router) getSkillOutputSchema(w http.ResponseWriter, req *http.Request) {
	if r.skillRegistry == nil {
		http.Error(w, "skill registry not configured", http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(req, "id")
	s, ok := r.skillRegistry.Get(id)
	if !ok {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	tool := s.OutputTool()
	if tool.IsZero() || len(tool.InputSchema) == 0 {
		http.Error(w, "skill ships constrained-generation fallback (no output schema)", http.StatusNotFound)
		return
	}
	// Schema gets the IETF JSON-Schema media type (RFC 8927). The
	// shared writeJSON helper unconditionally stamps application/json,
	// so we serialize directly here to keep the more-specific
	// content-type that signals "this is a JSON Schema document, not
	// a generic JSON payload".
	w.Header().Set("Content-Type", "application/schema+json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tool.InputSchema)
}

func summarizeSkill(s corepersona.Skill) skillSummary {
	tool := s.OutputTool()
	return skillSummary{
		ID:              s.ID(),
		Name:            s.Name(),
		Description:     s.Description(),
		OutputToolName:  tool.Name,
		HasOutputSchema: !tool.IsZero() && len(tool.InputSchema) > 0,
	}
}
