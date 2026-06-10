package onboarder

import (
	"context"
	"errors"
	"fmt"

	"github.com/GembaCore/gemba-core/internal/skills/newproject"
)

// Greeting is the Onboarder's opening line on /start. Returned by
// [Persona.Greeting] and by the [SkillTurner] adapter so the SPA's
// /api/v1/newproject/start response carries an immediate assistant
// turn the operator can read while typing their first message.
const Greeting = "Hi — what are we building? Tell me the rough shape (web app, library, ops tool, …) and I'll propose milestones, epics, and beads."

// Persona is one transient Onboarder instance. Bind one to each
// /api/v1/newproject session; discard at session end.
//
// A Persona is single-skill: it runs the newproject skill and
// nothing else. The persona-skill binding is fixed at the package
// level (not configurable) — see docs/design/newproject.md
// §"Onboarder persona + newproject skill".
//
// Concurrency: a Persona is owned by exactly one HTTP session. The
// HTTP layer serializes Turn calls per session (the SPA sends one
// turn at a time and waits for the response before sending the
// next), so Persona itself does not internalize a mutex. If a
// future caller needs concurrent Turn invocations on one Persona,
// add a sync.Mutex around the LLMClient call.
type Persona struct {
	client newproject.LLMClient
}

// Spawn constructs a fresh Onboarder using resolver to obtain an
// LLM client. Callers MUST treat the returned Persona as
// short-lived: spawn → run conversation → discard. Reusing one
// Persona across unrelated sessions is supported (the Persona
// itself is stateless modulo its client) but not the design intent.
//
// Returns ErrNoLLMClient (or a wrapping error) when the resolver
// reports no chat client is configured. The HTTP layer surfaces
// this as a 503 with [MissingClientDiagnostic] so the SPA's /onboard
// route and board CTA can render the operator-facing diagnostic
// verbatim.
func Spawn(ctx context.Context, resolver Resolver) (*Persona, error) {
	if resolver == nil {
		return nil, fmt.Errorf("onboarder: nil resolver")
	}
	client, err := resolver(ctx)
	if err != nil {
		return nil, err
	}
	if client == nil {
		// A resolver that returns (nil, nil) is a programmer bug —
		// it means "I claim success but produced no client". Treat
		// it as the no-client diagnostic so the operator gets an
		// actionable error.
		return nil, ErrNoLLMClient
	}
	return &Persona{client: client}, nil
}

// SpawnWithClient builds a Persona around an explicit
// [newproject.LLMClient], bypassing resolver lookup. Useful for
// tests and for hosts that already have a client in hand (e.g. a
// terminal interactive mode that wants to inject a custom transport).
func SpawnWithClient(client newproject.LLMClient) (*Persona, error) {
	if client == nil {
		return nil, ErrNoLLMClient
	}
	return &Persona{client: client}, nil
}

// Greeting returns the assistant's opening message for /start. The
// SPA renders this as the first conversational bubble before any
// user input.
func (p *Persona) Greeting() string { return Greeting }

// Turn runs one conversational turn through the bound newproject
// skill. On success returns the next state and the assistant
// reply. On a [newproject.ValidationError] the persona retries
// once with a hint embedded in the user message describing the
// validator's complaint; that retry is the validation-retry-once
// policy from the design doc.
//
// Errors:
//   - newproject.ValidationError after retry: the model produced a
//     malformed plan twice running. The host surfaces this as a
//     500 (the SPA shows a retry banner).
//   - any other error: transport / decode / context. Surfaced as
//     a 500.
//
// Discard signals: there are none. A Persona is discarded by
// dropping the reference; the LLMClient does not own resources
// that require explicit release in this package (the HTTP transport
// is owned by net/http). Future client implementations that hold
// long-lived state SHOULD add a Close() method here.
func (p *Persona) Turn(ctx context.Context, prior newproject.NewProjectState, message string) (newproject.TurnResult, error) {
	out, err := newproject.Run(ctx, prior, message, p.client)
	if err == nil {
		return out, nil
	}
	if !newproject.IsValidationError(err) {
		return newproject.TurnResult{}, err
	}
	// Validation-retry-once: re-prompt with the validator's
	// complaint embedded in the user message so the model has
	// concrete corrective feedback. We DO NOT mutate the system
	// prompt — the contract is locked in the skill package and
	// editing it from outside risks decoupling prompt from
	// validator.
	retryMsg := buildRetryHint(message, err)
	out, err2 := newproject.Run(ctx, prior, retryMsg, p.client)
	if err2 != nil {
		// Wrap the second failure but preserve the validation-error
		// type so callers using errors.Is / IsValidationError can
		// still classify the underlying cause.
		return newproject.TurnResult{}, fmt.Errorf("onboarder: validation retry failed: %w", err2)
	}
	return out, nil
}

// buildRetryHint composes the user-message variant the persona
// sends on the validation-retry pass. The hint reads as a short
// directive prepended to the operator's original message; the
// model treats it as part of the conversational instruction
// because the system prompt's output contract has primed it to
// emit a corrected plan.
func buildRetryHint(originalMessage string, valErr error) string {
	hint := "[onboarder retry] The previous plan was rejected by the validator: " + valErr.Error() + ". Re-emit a corrected plan that satisfies the schema and acceptance contract."
	if originalMessage == "" {
		return hint
	}
	return hint + "\n\nOperator message: " + originalMessage
}

// Discard releases any resources held by the persona. Idempotent —
// calling Discard multiple times is safe. Today this is a no-op;
// the surface exists so callers can "spawn → run → discard" as the
// design doc requires without needing to know whether the persona
// has resources to clean up.
func (p *Persona) Discard() {
	// Reserved for future client implementations that hold
	// long-lived state.
}

// IsNoClient reports whether err is the no-client diagnostic
// (returned by Spawn / Resolver). The HTTP layer uses this to map
// onto the 503 + MissingClientDiagnostic response.
func IsNoClient(err error) bool {
	return errors.Is(err, ErrNoLLMClient)
}
