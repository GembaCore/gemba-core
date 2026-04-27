// Walk-specific persona prompt extensions (gm-4p6, design
// docs/design/gemba-walk.md §During a walk + §Resumability).
//
// The PM persona normally answers a single skill consult and
// returns. During a Gemba walk it runs a different shape of
// conversation: one agenda item at a time, frame → propose →
// yield to the operator → apply. The system prompt needs a
// short walk-mode framing fragment so the model behaves
// accordingly; the resume turn needs the deterministic synopsis
// from walk.BuildResumeContext prepended to the operator's
// message so the persona can pick up where the paused walk left
// off.
//
// Both helpers are pure text composition. They never call the
// LLM, never mutate state, and have no side effects. The
// dispatcher composes [WalkPromptExtension] into the system
// prompt when [ComposeRequest.WalkActive] is true; the HTTP
// resume handler (gm-wgv's domain) calls [RenderResumePrompt]
// to wrap the operator's first post-resume message.
//
// v1 scope (per gm-4p6): SuggestedActions only — the persona
// continues to propose mutations as SuggestedAction lines, the
// operator clicks to apply. The agentic mutation_authority path
// (gm-uf7) is not yet wired; when it lands the walk extension
// can be expanded with mutation-authority framing without
// breaking this contract.

package persona

import (
	"strings"

	"github.com/MikeBengtson/gemba/internal/walk"
)

// walkSystemPromptFragment is the text appended to the persona
// system prompt when the consult runs inside an active Gemba
// walk. The fragment teaches the model the four-step item
// pattern (frame → propose → yield → apply) and the brevity
// expectation, leaving every other rule from the persona's base
// system_prompt untouched.
//
// Kept as a const (not a template) on purpose: the walk shape
// is invariant, and a runtime-substituted fragment would just
// add a layer of indirection that persona authors would have to
// reason about. If a future walk variant needs different
// framing, branch on the variant in [WalkPromptExtension] and
// keep this fragment as the default.
const walkSystemPromptFragment = `
## Gemba walk mode

You are running a Gemba walk — an interactive multi-topic decisioning
conversation with the operator. Walk one agenda item at a time. For
each item:

1. **Frame** the topic in one sentence (what it is, why it's on the
   agenda).
2. **Summarize** the source context briefly (the escalation, the bead,
   the gate failure — whatever surfaced it).
3. **Propose** 1-3 concrete actions the operator can take. Each action
   is a SuggestedAction (verb + path + body) the operator clicks to
   apply. Keep the action set small and load-bearing; do not pad.
4. **Yield** to the operator. Do not chain to the next agenda item on
   your own. The operator will respond with ratify / modify / reject /
   defer / handoff and you act on that decision before moving on.

Brevity is load-bearing: the operator may have twenty items to walk.
Prefer one tight paragraph plus the action set over a wall of
analysis. Cite beads / escalations by ID, never title alone.

If the next user turn opens with a "Resume context:" preamble, that is
the lossless synopsis of the walk so far (status, agenda counts,
recent transcript). Read it before responding so you pick up where the
walk left off rather than re-introducing the agenda.

You may not file beads or write to the WorkPlane directly during a
walk; SuggestedAction is the only mutation surface. The operator (or a
Manager-variety persona, when wired) executes the action.
`

// WalkPromptExtension returns the walk-mode system-prompt
// fragment to append to the persona's base system prompt when
// the consult runs inside an active walk. Returns the empty
// string when walkID is empty so a caller can do
//
//	personaSystem += WalkPromptExtension(string(walk.IDFromContext(ctx)))
//
// without an extra branch — non-walk consults pay nothing.
//
// The fragment includes the walk id as an inline cue for the
// model so a future debug trail / regret-log query can grep the
// prompt for the walk's id and find every consult that ran
// under it.
func WalkPromptExtension(walkID string) string {
	id := strings.TrimSpace(walkID)
	if id == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(walkSystemPromptFragment, "\n"))
	b.WriteString("\n\nActive walk id: ")
	b.WriteString(id)
	b.WriteString("\n")
	return b.String()
}

// resumePreamble is the marker the persona is told to look for
// in [walkSystemPromptFragment]. Lifting it to a constant keeps
// the system-prompt fragment and the resume-render helper from
// drifting apart.
const resumePreamble = "Resume context:"

// RenderResumePrompt prepends a deterministic resume synopsis
// (from [walk.BuildResumeContext]) to the operator's actual
// message so the persona has lossless state on the next turn.
// Format:
//
//	Resume context:
//	<BuildResumeContext output>
//	---
//	<userMsg>
//
// userMsg is trimmed of leading/trailing whitespace; an empty
// userMsg is allowed (sometimes the resume turn is just "pick
// up where you left off" with no new content). The synopsis is
// always emitted — if the walk's transcript is empty,
// [walk.BuildResumeContext] still returns the status line so the
// persona knows it's resuming.
//
// Plumbing: the HTTP /walks/:id/resume handler (gm-wgv) calls
// this once per resume event and stores the result as the next
// user-role turn before invoking the dispatcher.
func RenderResumePrompt(w walk.Walk, userMsg string) string {
	synopsis := strings.TrimSpace(walk.BuildResumeContext(w, walk.ResumeOptions{}))

	var b strings.Builder
	b.WriteString(resumePreamble)
	b.WriteByte('\n')
	if synopsis != "" {
		b.WriteString(synopsis)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	if msg := strings.TrimSpace(userMsg); msg != "" {
		b.WriteString(msg)
		b.WriteByte('\n')
	}
	return b.String()
}
