package newproject

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// LLMClient is the narrow surface the newproject skill needs from
// the host's LLM provider. The Onboarder persona (gm-root.17.10)
// resolves a real client at invocation time and injects it; this
// package never deals with credentials, retries, or vendor SDKs.
//
// A single Complete call corresponds to one model turn:
//   - systemPrompt is the role + schema scaffolding (from
//     SystemPrompt()).
//   - userPrompt is the operator's new message + the prior state
//     rendered as JSON (from UserPrompt()).
//
// Implementations MUST return the model's raw text. The skill
// parses + validates that text into a NewProjectState; transport
// failures (network, auth, rate limit) MUST surface as the
// returned error so the host can distinguish them from validation
// failures.
type LLMClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// LLMClientFunc adapts a plain function to the LLMClient
// interface. Useful for tests and for hosts that already have a
// chat-completion closure handy.
type LLMClientFunc func(ctx context.Context, systemPrompt, userPrompt string) (string, error)

// Complete implements LLMClient.
func (f LLMClientFunc) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return f(ctx, systemPrompt, userPrompt)
}

// TurnResult is what one successful turn yields. The host returns
// these to the SPA wholesale (as the TurnResponse body in
// web/src/api/newproject.ts) -- the SPA replaces its local copy
// of the state on every turn rather than merging diffs.
type TurnResult struct {
	State NewProjectState
	Reply string
}

// Run executes one conversational turn. Inputs:
//
//   - prior: the conversation state before this turn. On the very
//     first turn, pass EmptyState(); the model produces an opener.
//   - operatorMessage: the operator's new message. May be empty
//     on turn 0 (the host kicks off with a generic greeting).
//   - client: the host-resolved LLM client.
//
// Outputs:
//
//   - On success, a TurnResult whose State.Turn == prior.Turn + 1.
//   - On a *ValidationError, the model produced a malformed plan;
//     the host SHOULD retry once with a "the previous plan was
//     invalid: <reason>" hint, then surface the failure to the
//     operator.
//   - On any other error (transport, JSON decode), the host
//     surfaces it as a 500 to the SPA -- these are skill / infra
//     bugs, not retryable.
//
// Run is pure modulo the LLMClient call. It does NOT mutate prior
// (the SPA's contract assumes the server replaces wholesale, but
// the in-process host may keep prior pinned for diffing -- so
// Run treats it as read-only).
func Run(ctx context.Context, prior NewProjectState, operatorMessage string, client LLMClient) (TurnResult, error) {
	if client == nil {
		return TurnResult{}, errors.New("newproject: LLMClient is nil")
	}

	user, err := UserPrompt(prior, operatorMessage)
	if err != nil {
		return TurnResult{}, err
	}

	raw, err := client.Complete(ctx, SystemPrompt(), user)
	if err != nil {
		return TurnResult{}, fmt.Errorf("newproject: llm client: %w", err)
	}

	env, err := decodeEnvelope(raw)
	if err != nil {
		return TurnResult{}, err
	}

	// First-turn empty plans are tolerated -- the model is
	// allowed to ask a clarifying question before drafting any
	// milestones. After turn 0, an empty plan is a regression we
	// flag so the host can retry.
	requireNonEmpty := prior.Turn > 0
	if err := ValidateState(&env.State, requireNonEmpty); err != nil {
		return TurnResult{}, err
	}

	// Enforce monotonic Turn increment. The model is instructed
	// to do this; if it doesn't, fix it in flight rather than
	// retrying -- the value is host-derived ground truth.
	expected := prior.Turn + 1
	if env.State.Turn != expected {
		env.State.Turn = expected
	}

	if strings.TrimSpace(env.Reply) == "" {
		return TurnResult{}, &ValidationError{
			Field:   "reply",
			Message: "must not be empty -- the SPA renders this as the assistant's conversational message",
		}
	}

	return TurnResult{State: env.State, Reply: env.Reply}, nil
}

// decodeEnvelope unwraps the model's text. Tolerates the common
// cases the model gets wrong: a leading code fence, trailing
// whitespace, leading prose before the JSON.
//
// Strict on the JSON shape itself (DisallowUnknownFields) so a
// model that hallucinates extra keys produces a clear validation
// error the host can show on retry, rather than silently
// dropping data.
func decodeEnvelope(raw string) (*turnEnvelope, error) {
	body := strings.TrimSpace(raw)

	// Strip a leading ```json ... ``` fence if present. The
	// system prompt forbids fences, but Anthropic in particular
	// occasionally adds them anyway when the user message looked
	// "code-y". Defensive trim keeps the happy path total.
	body = trimCodeFence(body)

	// Find the first '{' so any leading prose is dropped. The
	// remainder is fed to the JSON decoder -- if the model
	// emitted prose AFTER the JSON, the strict decoder catches
	// it as a trailing-data error, which we want.
	if i := strings.IndexByte(body, '{'); i > 0 {
		body = body[i:]
	}

	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var env turnEnvelope
	if err := dec.Decode(&env); err != nil {
		return nil, &ValidationError{
			Message: fmt.Sprintf("decode JSON envelope: %v", err),
		}
	}
	// The decoder returns nil for a successful decode that ate
	// only the first JSON value; if there's trailing content we
	// flag it.
	if dec.More() {
		return nil, &ValidationError{
			Message: "extra content after the JSON envelope -- emit JSON only, no trailing prose",
		}
	}
	return &env, nil
}

// trimCodeFence strips a single leading ```...``` wrapper if
// present. Handles both ```json and bare ``` openers. Idempotent
// on already-clean input.
func trimCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence line.
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}
