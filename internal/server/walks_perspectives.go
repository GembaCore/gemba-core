// Per-agenda-item perspective contributions (gm-bms).
//
// During a Gemba walk, each active agenda item should surface inline
// perspective notes from every persona whose Perspective.VolunteerMode
// is "always" — Security may volunteer "blocking", Architect may
// volunteer "DD amendment", Documentarian may volunteer "release-notes
// gap". The PM's framing stays primary; perspectives sit alongside.
//
// This handler implements GET /api/v1/walks/{id}/agenda/{item}/perspectives.
// GET (not POST) because:
//   - the operation is idempotent + cacheable per (walk, item) pair,
//   - it has no side effects on store state,
//   - the SPA can stale-revalidate it on SSE walk.* events without
//     having to invent an idempotency nonce.
//
// The response is a list of PerspectiveContribution. Per the bead's
// scope, v1 generates canned templated text per persona-perspective-
// kind so the UI is non-empty without doing real LLM work — the
// real persona-response generation belongs to gm-4p6's response
// shape. The kind is inferred from the persona's Purview.Domain when
// available (security domain → "blocking", architect/design domain →
// "amendment", everything else → "note").
//
// Returns:
//
//   - 503 when AttachWalk hasn't bound a store, or
//     AttachPersonaDispatcher hasn't bound a persona registry. The SPA
//     treats both as "not configured" and hides the panel.
//   - 404 when the walk or agenda item doesn't exist.
//   - 200 with {"perspectives": [...]} on success. Empty list is the
//     correct answer when no persona is configured with
//     VolunteerMode == "always".

package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	corepersona "github.com/GembaCore/gemba-core/internal/core/persona"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
	"github.com/GembaCore/gemba-core/internal/walk"
)

// PerspectiveContributionKind classifies an inline perspective. Kept
// as a typed string so the SPA can branch on it for styling
// (blocking → red, amendment → amber, note → neutral) without parsing
// the human-readable content.
type PerspectiveContributionKind string

const (
	PerspectiveKindBlocking  PerspectiveContributionKind = "blocking"
	PerspectiveKindAmendment PerspectiveContributionKind = "amendment"
	PerspectiveKindNote      PerspectiveContributionKind = "note"
)

// PerspectiveContribution is one persona's volunteered comment on an
// active agenda item. Stable JSON shape — additive fields can land
// later without breaking the SPA.
type PerspectiveContribution struct {
	PersonaID   string                      `json:"persona_id"`
	PersonaName string                      `json:"persona_name,omitempty"`
	PersonaIcon string                      `json:"persona_icon,omitempty"`
	Kind        PerspectiveContributionKind `json:"kind"`
	Content     string                      `json:"content"`
	At          time.Time                   `json:"at"`
}

// listWalkPerspectives handles GET /api/v1/walks/{id}/agenda/{item}/perspectives.
// Iterates the persona registry for VolunteerMode=="always" entries and
// emits one PerspectiveContribution per persona. v1 generates a
// templated stub message; gm-4p6 will replace this with real LLM
// invocations.
func (r *Router) listWalkPerspectives(w http.ResponseWriter, req *http.Request) {
	if r.walkStore == nil {
		walkAdaptorUnavailable(w)
		return
	}
	if r.personaRegistry == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "persona registry not registered")
		return
	}

	id := walk.ID(chi.URLParam(req, "id"))
	itemID := chi.URLParam(req, "item")
	if id == "" || itemID == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"walk id and item id required")
		return
	}

	wk, err := r.walkStore.Get(req.Context(), id)
	if err != nil {
		writeWalkError(w, err)
		return
	}

	var item *walk.AgendaItem
	for i := range wk.Agenda {
		if wk.Agenda[i].ID == itemID {
			item = &wk.Agenda[i]
			break
		}
	}
	if item == nil {
		httperr.Write(w, http.StatusNotFound, "not_found",
			"agenda item not found on walk")
		return
	}

	now := time.Now().UTC()
	contributions := make([]PerspectiveContribution, 0)
	for _, pid := range r.personaRegistry.List() {
		p, ok := r.personaRegistry.Get(pid)
		if !ok {
			continue
		}
		// Only personas with VolunteerMode=="always" surface here.
		// VolunteerOnTrigger / VolunteerOnDemand / VolunteerNever do
		// not auto-volunteer on every agenda item. The on-trigger
		// matcher (gm-perspective-volunteer) is a separate slice.
		if p.Perspective.IsZero() {
			continue
		}
		if p.Perspective.VolunteerMode != corepersona.VolunteerAlways {
			continue
		}
		contributions = append(contributions, perspectiveStubFor(p, *item, now))
	}

	// Stable order: by persona id ascending. Registry.List is already
	// sorted but a defensive re-sort keeps the contract explicit.
	sort.SliceStable(contributions, func(i, j int) bool {
		return contributions[i].PersonaID < contributions[j].PersonaID
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"walk_id":        string(wk.ID),
		"agenda_item_id": item.ID,
		"perspectives":   contributions,
		"total":          len(contributions),
	})
}

// perspectiveStubFor produces a templated PerspectiveContribution for
// the given persona + agenda item. v1 stub: deterministic text drawn
// from the persona's Perspective.Statement and Purview.Domain so the
// UI has non-empty content without invoking an LLM. gm-4p6 will
// replace the body with real persona-dispatcher output; the wire
// shape stays compatible.
func perspectiveStubFor(p *corepersona.Persona, item walk.AgendaItem, at time.Time) PerspectiveContribution {
	kind := perspectiveKindFromPurview(p.Purview)

	statement := strings.TrimSpace(p.Perspective.Statement)
	if statement == "" {
		statement = "no perspective statement declared"
	}

	var content string
	switch kind {
	case PerspectiveKindBlocking:
		content = p.Name + " flags this item against my purview (" +
			strings.TrimSpace(p.Purview.Domain) + "): " + statement +
			". Operator should resolve before ratifying."
	case PerspectiveKindAmendment:
		content = p.Name + " proposes an amendment through the lens of " +
			statement + " — review the framing on \"" + item.Topic + "\"."
	default:
		content = p.Name + " (" + statement + "): noting this item for awareness."
	}

	return PerspectiveContribution{
		PersonaID:   p.ID,
		PersonaName: p.Name,
		PersonaIcon: p.Icon,
		Kind:        kind,
		Content:     content,
		At:          at,
	}
}

// perspectiveKindFromPurview infers the contribution kind from the
// persona's Purview. Heuristics, not contract: the SPA only renders
// the kind for styling, so a wrong call doesn't break behaviour.
//
//   - Purview with BlockingAuthority == "blocking" → blocking
//   - Domain matching design / architecture / docs / release → amendment
//   - everything else → note
func perspectiveKindFromPurview(pv corepersona.Purview) PerspectiveContributionKind {
	if pv.IsZero() {
		return PerspectiveKindNote
	}
	if pv.BlockingAuthority == corepersona.PurviewBlocking {
		return PerspectiveKindBlocking
	}
	domain := strings.ToLower(strings.TrimSpace(pv.Domain))
	switch domain {
	case "design", "architecture", "docs", "documentation", "release", "copy":
		return PerspectiveKindAmendment
	}
	return PerspectiveKindNote
}
