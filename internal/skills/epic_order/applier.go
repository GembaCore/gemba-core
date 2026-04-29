package epic_order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/persona"
)

// Applier executes a *RecommendationLine's SuggestedAction against
// a bound WorkPlane (gm-twp2.1). Today's contract: the action must
// be a PATCH against /api/work-items/{id} (or /api/workitems/{id})
// with a JSON body that decodes into core.WorkItemPatch — the only
// shape epic_order's PM persona is configured to emit.
//
// Other verbs / paths are rejected with a clear error so a future
// skill that needs a different mutation surface gets its own
// applier rather than overloading this one.
type Applier struct {
	wp core.WorkPlane
}

// NewApplier returns an executor backed by wp. Pass nil to disable
// (the dispatcher's apply path falls through to record-only mode
// when the registry slot is empty).
func NewApplier(wp core.WorkPlane) *Applier {
	if wp == nil {
		return nil
	}
	return &Applier{wp: wp}
}

// Compile-time interface check.
var _ persona.Applier = (*Applier)(nil)

// Apply implements persona.Applier. Translates the SuggestedAction
// into a WorkPlane.UpdateWorkItem call. Errors are operator-facing
// (the dispatcher wraps with "applier for skill epic_order:" and
// the HTTP layer surfaces verbatim).
func (a *Applier) Apply(ctx context.Context, line any) (persona.ApplierResult, error) {
	rec, ok := line.(*RecommendationLine)
	if !ok {
		// Strategy / warning / deferred / summary lines have no
		// SuggestedAction. The dispatcher's HTTP layer should
		// disable Apply on those rows; defense-in-depth gate here
		// in case it doesn't.
		return persona.ApplierResult{}, fmt.Errorf("epic_order: only recommendation lines apply (got %T)", line)
	}
	if rec.SuggestedAction == nil {
		return persona.ApplierResult{}, errors.New("epic_order: recommendation has no suggested_action")
	}
	verb := strings.ToUpper(rec.SuggestedAction.Verb)
	if verb != "PATCH" {
		return persona.ApplierResult{}, fmt.Errorf("epic_order: unsupported verb %q (only PATCH)", rec.SuggestedAction.Verb)
	}

	id, err := workItemIDFromPath(rec.SuggestedAction.Path)
	if err != nil {
		return persona.ApplierResult{}, err
	}
	if id != rec.EpicID {
		// The path's id must match the line's epic_id — protects
		// against a model that hallucinates a path against an
		// epic it never ranked.
		return persona.ApplierResult{}, fmt.Errorf("epic_order: suggested_action.path id %q != recommendation.epic_id %q", id, rec.EpicID)
	}

	var patch core.WorkItemPatch
	if len(rec.SuggestedAction.Body) > 0 {
		if err := json.Unmarshal(rec.SuggestedAction.Body, &patch); err != nil {
			return persona.ApplierResult{}, fmt.Errorf("epic_order: suggested_action.body decode: %w", err)
		}
	}

	updated, err := a.wp.UpdateWorkItem(ctx, id, patch)
	if err != nil {
		return persona.ApplierResult{}, fmt.Errorf("epic_order: WorkPlane.UpdateWorkItem(%s): %w", id, err)
	}

	body, err := json.Marshal(updated)
	if err != nil {
		// Marshalling the WorkPlane response should never fail
		// with the standard core.WorkItem shape; treat any
		// failure as operator-actionable.
		return persona.ApplierResult{}, fmt.Errorf("epic_order: marshal WorkItem result: %w", err)
	}
	return persona.ApplierResult{
		Detail: fmt.Sprintf("PATCH %s applied (rank=%d)", id, rec.Rank),
		Body:   body,
	}, nil
}

// workItemIDFromPath extracts the trailing id from a SuggestedAction
// path. Accepts the two paths the API surface exposes today —
// /api/work-items/{id} and /api/workitems/{id} — and rejects
// anything else with a clear error so a model that emits a free-form
// URL gets caught at the boundary.
func workItemIDFromPath(p string) (core.WorkItemID, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("epic_order: suggested_action.path is empty")
	}
	for _, prefix := range []string{"/api/work-items/", "/api/workitems/"} {
		if strings.HasPrefix(p, prefix) {
			id := strings.TrimPrefix(p, prefix)
			if id == "" {
				return "", fmt.Errorf("epic_order: suggested_action.path %q is missing the id", p)
			}
			return core.WorkItemID(id), nil
		}
	}
	return "", fmt.Errorf("epic_order: suggested_action.path %q does not match /api/work-items/{id}", p)
}
