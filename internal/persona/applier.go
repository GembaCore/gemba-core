package persona

import (
	"context"
	"encoding/json"
)

// Applier executes a validated skill-output line against whatever
// backend the action mutates (typically the WorkPlane). One
// implementation per skill — the implementation knows the line's
// concrete shape and how to translate its SuggestedAction into a
// real mutation.
//
// Return contract:
//   - Success: the action was applied; ApplierResult.Detail names
//     what changed in operator-readable form.
//   - Error: the action did NOT land. The dispatcher's Apply path
//     suppresses the AppliedIdx append on failure so a retry can
//     re-attempt without hitting the duplicate-idx gate.
//
// Concurrency: implementations MUST be safe for concurrent use.
// The dispatcher invokes Apply outside its own lock so a slow
// WorkPlane mutation doesn't block other consults.
type Applier interface {
	Apply(ctx context.Context, line any) (ApplierResult, error)
}

// ApplierResult is the executor's response. Detail is the operator-
// readable summary (e.g. "PATCHed gm-e3 user_order=1"); Body
// carries the structured payload the executor produced (the
// resulting WorkItem after a PATCH, an empty {} for verbs that
// don't return a body) so the HTTP response can surface it
// without a follow-up GET.
type ApplierResult struct {
	Detail string          `json:"detail"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// SetApplier registers an executor for the given skill id. nil
// clears the registration (record-only fallback). Concurrency:
// holds the dispatcher's write lock for the map mutation; reads
// in Apply take the read lock.
func (d *Dispatcher) SetApplier(skillID string, a Applier) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.appliers == nil {
		d.appliers = make(map[string]Applier)
	}
	if a == nil {
		delete(d.appliers, skillID)
		return
	}
	d.appliers[skillID] = a
}

// applierFor returns the registered applier for skillID. Read
// access — caller must hold an RLock or accept the snapshot may
// race a concurrent SetApplier (the registration is set once at
// boot in production, so racing is benign in practice).
func (d *Dispatcher) applierFor(skillID string) Applier {
	if d.appliers == nil {
		return nil
	}
	return d.appliers[skillID]
}
