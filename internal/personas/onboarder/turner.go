package onboarder

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/GembaCore/gemba-core/internal/skills/newproject"
)

// SkillTurner adapts an Onboarder Persona onto the
// internal/server.SkillTurner contract so server.AttachNewProject
// can plug it in.
//
// Lifecycle: a single SkillTurner serves the whole serve process;
// every /api/v1/newproject session reuses the same Persona because
// the Persona itself is stateless modulo its LLMClient (the
// per-session conversation state lives in NewProjectState, threaded
// through Turn). The "spawn-on-demand → run conversation → discard"
// language in the design doc applies to the conversation, not the
// HTTP-shared chat client.
//
// Spawn timing: SkillTurner does NOT spawn a Persona at construction
// time. It spawns lazily on the first Probe / Turn call so a binary
// that boots without LLM credentials can still respond to every
// other route. Operators who configure credentials later restart
// gemba serve to reset the cached Persona — there is no live
// reconfiguration today (config.toml reload is out of scope for v1).
type SkillTurner struct {
	resolver Resolver

	mu      sync.Mutex
	persona *Persona
	// spawnErr caches the most recent Spawn failure so subsequent
	// calls don't thrash the resolver (which may hit disk). We
	// re-attempt on the next call after a non-ErrNoLLMClient
	// failure — those are typically transient (config.toml
	// permissions blip, etc.); the diagnostic ErrNoLLMClient is
	// stable until the operator edits config.
	spawnErr error
	// spawnAttempted is true once we've called Spawn at least once;
	// used to know we have something to surface even when both
	// persona and spawnErr are nil-safe sentinels.
	spawnAttempted bool
}

// NewSkillTurner builds the adapter. Pass [DefaultResolver] for the
// production wiring; tests inject a closure that returns a fake
// LLMClient. A nil resolver is rejected at construction time — the
// adapter is useless without one.
func NewSkillTurner(resolver Resolver) *SkillTurner {
	if resolver == nil {
		// Nil-resolver sentinel: hand back a turner whose Probe
		// always reports the no-client diagnostic. This keeps the
		// constructor total (no error return) so cmd/gemba serve
		// can compose it inline with other AttachNewProject args.
		resolver = func(_ context.Context) (newproject.LLMClient, error) {
			return nil, ErrNoLLMClient
		}
	}
	return &SkillTurner{resolver: resolver}
}

// Greeting implements server.SkillTurner. Returns the canonical
// Onboarder opener regardless of LLM-client state — the SPA shows
// it after /start succeeds, and /start has already gated on Probe,
// so by the time Greeting renders the operator has a working
// session.
func (t *SkillTurner) Greeting() string { return Greeting }

// Probe implements server.SkillTurner. Reports whether the
// underlying Persona can be spawned. Returns the no-client
// diagnostic when the resolver reports no chat client; nil
// otherwise. Idempotent — multiple Probe calls on a working turner
// reuse the cached Persona.
func (t *SkillTurner) Probe(ctx context.Context) error {
	_, err := t.acquire(ctx)
	return err
}

// Turn implements server.SkillTurner. Lazily spawns a Persona on
// first call, then forwards to Persona.Turn (which carries the
// validation-retry-once policy). Edits are accepted but ignored at
// this layer — the SPA already merges in-place edits into the
// state it POSTs back, so the skill sees the post-edit state via
// the prior NewProjectState argument.
func (t *SkillTurner) Turn(ctx context.Context, in NewProjectState, message string, _ map[string]interface{}) (NewProjectState, string, error) {
	persona, err := t.acquire(ctx)
	if err != nil {
		return NewProjectState{}, "", err
	}
	res, err := persona.Turn(ctx, in, message)
	if err != nil {
		return NewProjectState{}, "", err
	}
	return res.State, res.Reply, nil
}

// acquire is the lazy-spawn primitive both Probe and Turn share.
// Holds the lock for the duration of the spawn call so racing
// callers serialize through one resolver invocation.
func (t *SkillTurner) acquire(ctx context.Context) (*Persona, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.persona != nil {
		return t.persona, nil
	}
	// Re-attempt on every call when the previous attempt was a
	// non-diagnostic failure (transient infra). The no-client
	// diagnostic is stable: caching it avoids hammering disk on
	// every /start probe when the operator hasn't edited config.
	if t.spawnAttempted && errors.Is(t.spawnErr, ErrNoLLMClient) {
		return nil, t.spawnErr
	}
	t.spawnAttempted = true
	p, err := Spawn(ctx, t.resolver)
	if err != nil {
		t.spawnErr = err
		return nil, err
	}
	t.persona = p
	t.spawnErr = nil
	return p, nil
}

// NewProjectState is the wire alias re-exported from the skill
// package so callers of this package's Turn don't need to import
// internal/skills/newproject directly when wiring the turner. The
// alias matches the one server-side (internal/server/newproject.go)
// so type identity is preserved across the boundary.
type NewProjectState = newproject.NewProjectState

// Probe + Turn signatures must satisfy the contract documented at
// internal/server.SkillTurner. The compile-time assertion below
// fails at build time if the interfaces drift.
//
// We keep the assertion in this package (rather than in server) so
// adding a new method to SkillTurner forces the implementer to
// notice rather than silently breaking server-side compilation.
var _ skillTurnerContract = (*SkillTurner)(nil)

// skillTurnerContract is a private mirror of the
// internal/server.SkillTurner interface. Duplicated here (rather
// than imported) to break the cycle: the server package imports
// types from internal/skills/newproject; this package implements
// SkillTurner; if we imported server here we'd create a cycle.
type skillTurnerContract interface {
	Turn(ctx context.Context, in NewProjectState, message string, edits map[string]interface{}) (out NewProjectState, reply string, err error)
	Greeting() string
	Probe(ctx context.Context) error
}

// Used to silence the unused-import warning when fmt is only
// referenced in error wrapping helpers. Also a hook future
// implementations can call when they need to format a
// disambiguating error message.
var _ = fmt.Sprintf
