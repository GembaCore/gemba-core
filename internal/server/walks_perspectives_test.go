// Tests for GET /api/v1/walks/{id}/agenda/{item}/perspectives (gm-bms).
//
// Covers:
//   - 503 when AttachWalk hasn't been called
//   - 503 when AttachPersonaDispatcher hasn't bound a registry
//   - 404 when the walk or item is unknown
//   - empty perspectives list when no persona declares VolunteerAlways
//   - filtering: only VolunteerAlways personas surface; other modes are
//     silent here (their volunteer paths are separate).
//   - kind inference from Purview (blocking | amendment | note)
//   - stable id-ascending order

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
	"github.com/MikeBengtson/gemba/internal/walk"
)

func newWalkWithItem(t *testing.T, store *walk.MemoryStore, itemID, topic string) walk.Walk {
	t.Helper()
	wk, err := store.Start(context.Background(), walk.StartParams{
		Workspace:      "ws",
		InitiatedBy:    "operator",
		PrimaryPersona: "project-manager",
		InitialAgenda: []walk.AgendaItem{
			{
				ID:      itemID,
				Topic:   topic,
				Source:  walk.AgendaSource{Kind: walk.SourceUser},
				Status:  walk.AgendaActive,
				AddedAt: time.Now().UTC(),
				AddedBy: "operator",
			},
		},
		Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Start walk: %v", err)
	}
	return wk
}

func TestWalkPerspectives_NotConfigured_503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/walks/walk-x/agenda/item-x/perspectives", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWalkPerspectives_NoPersonaRegistry_503(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	wk := newWalkWithItem(t, store, "item-1", "topic")

	// AttachPersonaDispatcher NOT called → 503.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/walks/"+string(wk.ID)+"/agenda/item-1/perspectives", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWalkPerspectives_WalkNotFound_404(t *testing.T) {
	r, _, _ := newWalkRouter(t)
	r.AttachPersonaDispatcher(nil, nil, corepersona.NewRegistry())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/walks/no-such-walk/agenda/item-x/perspectives", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWalkPerspectives_ItemNotFound_404(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	r.AttachPersonaDispatcher(nil, nil, corepersona.NewRegistry())
	wk := newWalkWithItem(t, store, "item-1", "topic")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/walks/"+string(wk.ID)+"/agenda/no-such-item/perspectives", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWalkPerspectives_EmptyWhenNoVolunteerAlways(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	reg := corepersona.NewRegistry()
	// Persona with VolunteerOnTrigger should NOT surface here.
	must(t, reg.Register(&corepersona.Persona{
		ID: "architect", Name: "Arch", Role: "Architect",
		Variety: corepersona.VarietyCoach,
		Scope:   corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Perspective: corepersona.Perspective{
			Statement:     "design integrity",
			VolunteerMode: corepersona.VolunteerOnTrigger,
		},
	}))
	// Persona with no Perspective declared at all.
	must(t, reg.Register(&corepersona.Persona{
		ID: "pm", Name: "PM", Role: "Project Manager",
		Variety: corepersona.VarietyCoach,
		Scope:   corepersona.PersonaScope{Kind: corepersona.ScopeProject},
	}))
	r.AttachPersonaDispatcher(nil, nil, reg)
	wk := newWalkWithItem(t, store, "item-1", "ship the thing")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/walks/"+string(wk.ID)+"/agenda/item-1/perspectives", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		WalkID       string                    `json:"walk_id"`
		AgendaItemID string                    `json:"agenda_item_id"`
		Perspectives []PerspectiveContribution `json:"perspectives"`
		Total        int                       `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 0 || len(got.Perspectives) != 0 {
		t.Errorf("want empty perspectives; got %+v", got)
	}
	if got.WalkID != string(wk.ID) || got.AgendaItemID != "item-1" {
		t.Errorf("got %+v, want walk_id=%s + item_id=item-1", got, wk.ID)
	}
}

func TestWalkPerspectives_FiltersAndKindInference(t *testing.T) {
	r, store, _ := newWalkRouter(t)
	reg := corepersona.NewRegistry()
	// Security: VolunteerAlways + blocking purview → kind=blocking.
	must(t, reg.Register(&corepersona.Persona{
		ID: "security", Name: "Sec", Role: "Security",
		Variety: corepersona.VarietyCoach,
		Scope:   corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Perspective: corepersona.Perspective{
			Statement:     "auth boundaries + secret leakage",
			VolunteerMode: corepersona.VolunteerAlways,
		},
		Purview: corepersona.Purview{
			Domain:            "auth",
			BlockingAuthority: corepersona.PurviewBlocking,
		},
	}))
	// Architect: VolunteerAlways + design domain (advisory) → kind=amendment.
	must(t, reg.Register(&corepersona.Persona{
		ID: "architect", Name: "Arch", Role: "Architect",
		Variety: corepersona.VarietyCoach,
		Scope:   corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Perspective: corepersona.Perspective{
			Statement:     "design integrity",
			VolunteerMode: corepersona.VolunteerAlways,
		},
		Purview: corepersona.Purview{
			Domain:            "design",
			BlockingAuthority: corepersona.PurviewAdvisory,
		},
	}))
	// Documentarian: VolunteerAlways + no purview → kind=note.
	must(t, reg.Register(&corepersona.Persona{
		ID: "documentarian", Name: "Docs", Role: "Documentarian",
		Variety: corepersona.VarietyCoach,
		Scope:   corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Perspective: corepersona.Perspective{
			Statement:     "release-notes coverage",
			VolunteerMode: corepersona.VolunteerAlways,
		},
	}))
	// Filtered out: VolunteerOnDemand should be silent.
	must(t, reg.Register(&corepersona.Persona{
		ID: "qa", Name: "QA", Role: "QA",
		Variety: corepersona.VarietyCoach,
		Scope:   corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Perspective: corepersona.Perspective{
			Statement:     "test coverage",
			VolunteerMode: corepersona.VolunteerOnDemand,
		},
	}))

	r.AttachPersonaDispatcher(nil, nil, reg)
	wk := newWalkWithItem(t, store, "item-1", "ship feature X")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/walks/"+string(wk.ID)+"/agenda/item-1/perspectives", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Perspectives []PerspectiveContribution `json:"perspectives"`
		Total        int                       `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 {
		t.Fatalf("total = %d, want 3 (security, architect, documentarian); got %+v",
			got.Total, got.Perspectives)
	}
	// Order: id ascending — architect, documentarian, security.
	wantOrder := []string{"architect", "documentarian", "security"}
	for i, c := range got.Perspectives {
		if c.PersonaID != wantOrder[i] {
			t.Errorf("[%d] persona_id = %q, want %q", i, c.PersonaID, wantOrder[i])
		}
	}
	byID := map[string]PerspectiveContribution{}
	for _, c := range got.Perspectives {
		byID[c.PersonaID] = c
	}
	if k := byID["security"].Kind; k != PerspectiveKindBlocking {
		t.Errorf("security kind = %q, want blocking", k)
	}
	if k := byID["architect"].Kind; k != PerspectiveKindAmendment {
		t.Errorf("architect kind = %q, want amendment", k)
	}
	if k := byID["documentarian"].Kind; k != PerspectiveKindNote {
		t.Errorf("documentarian kind = %q, want note", k)
	}
	for _, c := range got.Perspectives {
		if c.Content == "" {
			t.Errorf("persona %q produced empty content", c.PersonaID)
		}
		if c.At.IsZero() {
			t.Errorf("persona %q produced zero-time stamp", c.PersonaID)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
