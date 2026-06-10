// ResumeContext synthesis (gm-rfy, design §Ending a walk).
//
// On Pause, the persona dispatcher needs a compact summary it
// can inject as the next-turn rehydration so the persona can
// resume with the right mental model. The "LLM-condensed"
// version lands when the PM persona prompt extension does
// (gm-4p6); this package ships a deterministic synopsis that
// covers the lossless basics (status, agenda counts, last few
// turns, decisions) so a paused walk can resume even if the
// LLM rehydration call fails or is disabled.

package walk

import (
	"fmt"
	"strings"
	"time"
)

// DefaultRecentTurns is how many trailing transcript entries
// the deterministic synopsis includes verbatim. Six gives the
// persona enough context to resume mid-thought without bloating
// the prompt.
const DefaultRecentTurns = 6

// ResumeOptions tunes BuildResumeContext. Zero values resolve
// to defaults.
type ResumeOptions struct {
	// RecentTurns caps how many tail turns the synopsis quotes.
	RecentTurns int
	// MaxContentRunes truncates each quoted turn's content. 0
	// means "no truncation". 240 is the reasonable default;
	// keeps the rehydration prompt under a few-thousand tokens
	// even on a long walk.
	MaxContentRunes int
}

// BuildResumeContext returns a deterministic synopsis of the
// walk's current state. Output is plain text formatted for
// direct injection into a persona prompt — no JSON, no
// markdown headers.
//
// The synopsis covers: status, agenda counts by bucket, the
// active agenda item (if any), counts of decisions by kind, the
// running cost, and the trailing N turns. Lossless on the
// numeric fields; truncates only the transcript.
func BuildResumeContext(w Walk, opts ResumeOptions) string {
	if opts.RecentTurns <= 0 {
		opts.RecentTurns = DefaultRecentTurns
	}
	if opts.MaxContentRunes < 0 {
		opts.MaxContentRunes = 0
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Walk %s — %s\n", w.ID, w.Status)
	if w.Label != "" {
		fmt.Fprintf(&b, "Label: %s\n", w.Label)
	}
	fmt.Fprintf(&b, "Workspace: %s · Initiated by: %s · Started: %s\n",
		w.Workspace, w.InitiatedBy, w.StartedAt.UTC().Format(time.RFC3339))

	queued, active, decided, deferred, dismissed := 0, 0, 0, 0, 0
	var activeItem *AgendaItem
	for i := range w.Agenda {
		a := w.Agenda[i]
		switch a.Status {
		case AgendaQueued:
			queued++
		case AgendaActive:
			active++
			ai := a
			activeItem = &ai
		case AgendaDecided:
			decided++
		case AgendaDeferred:
			deferred++
		case AgendaDismissed:
			dismissed++
		}
	}
	fmt.Fprintf(&b, "Agenda: %d queued · %d active · %d decided · %d deferred · %d dismissed\n",
		queued, active, decided, deferred, dismissed)
	if activeItem != nil {
		fmt.Fprintf(&b, "Active item: [%s] %s (source: %s)\n",
			activeItem.ID, truncRunes(activeItem.Topic, 120), activeItem.Source.Kind)
	}

	if len(w.Decisions) > 0 {
		ratify, modify, reject, defer_, handoff := 0, 0, 0, 0, 0
		for _, d := range w.Decisions {
			switch d.Kind {
			case DecisionRatify:
				ratify++
			case DecisionModify:
				modify++
			case DecisionReject:
				reject++
			case DecisionDefer:
				defer_++
			case DecisionHandoff:
				handoff++
			}
		}
		fmt.Fprintf(&b, "Decisions: %d ratify · %d modify · %d reject · %d defer · %d handoff\n",
			ratify, modify, reject, defer_, handoff)
	}

	if w.Cost.Dollars > 0 || w.Cost.TokensIn > 0 || w.Cost.TokensOut > 0 {
		fmt.Fprintf(&b, "Cost: $%.2f · %d in / %d out tokens\n",
			w.Cost.Dollars, w.Cost.TokensIn, w.Cost.TokensOut)
	}

	if len(w.BeadsTouched) > 0 {
		ids := make([]string, 0, len(w.BeadsTouched))
		for _, id := range w.BeadsTouched {
			ids = append(ids, string(id))
		}
		fmt.Fprintf(&b, "Beads touched: %s\n", strings.Join(ids, ", "))
	}

	tail := tailTurns(w.Transcript, opts.RecentTurns)
	if len(tail) > 0 {
		fmt.Fprintln(&b, "Recent transcript:")
		for _, t := range tail {
			content := t.Content
			if opts.MaxContentRunes > 0 {
				content = truncRunes(content, opts.MaxContentRunes)
			}
			speaker := string(t.Speaker)
			if t.Speaker == SpeakerPersona && t.PersonaID != "" {
				speaker = t.PersonaID
			}
			fmt.Fprintf(&b, "  %s [%s]: %s\n",
				t.At.UTC().Format(time.RFC3339), speaker, content)
		}
	}
	return b.String()
}

func tailTurns(in []WalkTurn, n int) []WalkTurn {
	if n <= 0 || len(in) == 0 {
		return nil
	}
	if len(in) <= n {
		out := make([]WalkTurn, len(in))
		copy(out, in)
		return out
	}
	out := make([]WalkTurn, n)
	copy(out, in[len(in)-n:])
	return out
}

func truncRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
