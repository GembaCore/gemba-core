// /api/v1/personas read surface (gm-8qr). Lists the personas
// registered with the dispatcher's persona registry, projecting only
// the operator-facing axes — id, name, role, variety, scope, and the
// three gm-9rv axes (Personality, Perspective, Purview).
//
// Designed as additive metadata. Future beads (gm-3on, gm-jt9) will
// extend the response with phase + purview-active state without
// breaking this shape.
//
// Returns 503 when AttachPersonaDispatcher hasn't bound a registry
// yet so a stripped-down test or pre-config-load Router degrades
// cleanly. Empty registry returns {"personas": [], "total": 0}.

package server

import (
	"net/http"
	"sort"

	"github.com/GembaCore/gemba-core/core"
	corepersona "github.com/GembaCore/gemba-core/internal/core/persona"
)

// personaSummary is the row shape for /api/v1/personas. Keep the
// projection intentionally small — operator-facing identity + the
// three gm-9rv axes. Heavy fields (system_prompt, model config,
// budget policy) are NOT surfaced here; a separate detail endpoint
// can land later if a UI needs them.
type personaSummary struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Role        string              `json:"role"`
	Variety     corepersona.Variety `json:"variety"`
	Scope       personaScopeView    `json:"scope"`
	Description string              `json:"description,omitempty"`
	Icon        string              `json:"icon,omitempty"`
	Skills      []string            `json:"skills,omitempty"`

	// gm-9rv axes. Each block is omitted when the persona TOML
	// declared the zero value, so the SPA can render presence as a
	// boolean ("does this persona have a Purview?") without parsing
	// nested shape.
	Personality *corepersona.Personality `json:"personality,omitempty"`
	Perspective *perspectiveView         `json:"perspective,omitempty"`
	Purview     *purviewView             `json:"purview,omitempty"`
}

// personaScopeView is the wire shape for [corepersona.PersonaScope].
// Re-projected so the response avoids leaking unexported fields and
// keeps the JSON tag names stable across Go-side renames.
type personaScopeView struct {
	Kind         corepersona.ScopeKind `json:"kind"`
	RepositoryID core.RepositoryID     `json:"repository,omitempty"`
}

// perspectiveView mirrors [corepersona.Perspective] for the wire.
// The dedicated view lets the JSON tag set stay frozen even if the
// Go struct sprouts internal-only fields later.
type perspectiveView struct {
	Statement     string                    `json:"statement,omitempty"`
	Triggers      []string                  `json:"triggers,omitempty"`
	VolunteerMode corepersona.VolunteerMode `json:"volunteer_mode,omitempty"`
	CostTier      string                    `json:"cost_tier,omitempty"`
}

// purviewView mirrors [corepersona.Purview] for the wire. Phase is
// projected as a string slice — gm-jt9 may strengthen the type later
// without breaking the wire shape.
type purviewView struct {
	Domain            string                       `json:"domain,omitempty"`
	ActivePhases      []corepersona.Phase          `json:"active_phases,omitempty"`
	BlockingAuthority corepersona.PurviewAuthority `json:"blocking_authority,omitempty"`
}

// listPersonasV1 handles GET /api/v1/personas. Returns 503 when no
// registry is bound; otherwise emits all registered personas sorted
// by id (matching [corepersona.Registry.List]).
func (r *Router) listPersonasV1(w http.ResponseWriter, _ *http.Request) {
	if r.personaRegistry == nil {
		http.Error(w, "persona registry not configured", http.StatusServiceUnavailable)
		return
	}

	ids := r.personaRegistry.List()
	rows := make([]personaSummary, 0, len(ids))
	for _, id := range ids {
		p, ok := r.personaRegistry.Get(id)
		if !ok {
			continue
		}
		rows = append(rows, personaToSummary(p))
	}
	// Defensive re-sort: Registry.List sorts by id today, but a
	// future caching layer may not. Cheap on the small set of
	// personas a workspace typically declares.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

	writeJSON(w, http.StatusOK, map[string]any{
		"personas": rows,
		"total":    len(rows),
	})
}

// personaToSummary projects a [corepersona.Persona] onto the wire
// shape. Pointer-typed PPPP fields stay nil for zero-value blocks so
// the JSON omitempty produces the absence-as-null contract the SPA
// expects.
func personaToSummary(p *corepersona.Persona) personaSummary {
	out := personaSummary{
		ID:          p.ID,
		Name:        p.Name,
		Role:        p.Role,
		Variety:     p.Variety,
		Description: p.Description,
		Icon:        p.Icon,
		Skills:      p.Skills,
		Scope: personaScopeView{
			Kind:         p.Scope.Kind,
			RepositoryID: p.Scope.RepositoryID,
		},
	}
	if !p.Personality.IsZero() {
		// Copy so a downstream JSON encode of a zero examples slice
		// stays nil-or-non-nil consistent with the source.
		pp := p.Personality
		out.Personality = &pp
	}
	if !p.Perspective.IsZero() {
		out.Perspective = &perspectiveView{
			Statement:     p.Perspective.Statement,
			Triggers:      p.Perspective.Triggers,
			VolunteerMode: p.Perspective.VolunteerMode,
			CostTier:      p.Perspective.CostTier,
		}
	}
	if !p.Purview.IsZero() {
		out.Purview = &purviewView{
			Domain:            p.Purview.Domain,
			ActivePhases:      p.Purview.ActivePhases,
			BlockingAuthority: p.Purview.BlockingAuthority,
		}
	}
	return out
}
