package walk_summary

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/walk"
)

// Generate renders the markdown summary for a completed walk. Pure:
// no file I/O, no clock reads beyond in.Now. Returns an error only
// when the input walk is structurally unusable (empty ID); empty
// agenda / decisions / cost are all valid (a "we ended without
// deciding anything" walk is a real outcome and gets a short
// summary).
func Generate(in Input) (Output, error) {
	w := in.Walk
	if strings.TrimSpace(string(w.ID)) == "" {
		return Output{}, errors.New("walk_summary: walk.id is empty")
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	endedAt := now
	if w.EndedAt != nil {
		endedAt = *w.EndedAt
	}

	rel := in.PathHint
	if rel == "" {
		rel = derivePath(w, endedAt)
	} else {
		rel = strings.TrimPrefix(rel, "/")
	}

	body := renderMarkdown(w, endedAt)
	return Output{RelativePath: rel, Markdown: body}, nil
}

// derivePath computes docs/walks/<YYYY-MM-DD>-<slug>.md from the
// walk's ended-at timestamp + label (or id when label is empty or
// slugifies to nothing). Always returns a forward-slash path so the
// applier can mkdir-p across platforms uniformly.
func derivePath(w walk.Walk, endedAt time.Time) string {
	stem := slug(w.Label)
	if stem == "" {
		stem = slug(string(w.ID))
	}
	if stem == "" {
		// Last resort: a literal "walk" so the path is still valid
		// markdown and the operator can rename it post-hoc.
		stem = "walk"
	}
	return path.Join("docs", "walks", fmt.Sprintf("%s-%s.md", endedAt.UTC().Format("2006-01-02"), stem))
}

// countDecisions tallies decisions by kind. Unknown kinds are
// silently ignored — the walk package's enum is the source of truth
// and a new kind there will surface here as a count gap rather than
// a render error.
func countDecisions(ds []walk.WalkDecision) decisionCounts {
	var c decisionCounts
	for _, d := range ds {
		switch d.Kind {
		case walk.DecisionRatify:
			c.Ratify++
		case walk.DecisionModify:
			c.Modify++
		case walk.DecisionReject:
			c.Reject++
		case walk.DecisionDefer:
			c.Defer++
		case walk.DecisionHandoff:
			c.Handoff++
		}
	}
	return c
}

// countAgenda groups agenda items by status + source kind. Resolved
// covers items with status decided; carried-forward bundles queued
// + deferred (both will reappear on the next walk's agenda); the
// per-source map is populated for every item regardless of status
// so a "where did this walk's work come from?" question is one
// lookup deep.
func countAgenda(items []walk.AgendaItem) agendaCounts {
	c := agendaCounts{
		BySource: make(map[walk.AgendaSourceKind]int, 8),
	}
	for _, a := range items {
		c.Total++
		c.BySource[a.Source.Kind]++
		switch a.Status {
		case walk.AgendaDecided:
			c.Resolved++
		case walk.AgendaQueued, walk.AgendaDeferred:
			c.CarriedForward++
		case walk.AgendaDismissed:
			c.Dismissed++
		}
	}
	return c
}

// renderMarkdown is the templating step. Sectioned with H2 headings
// so the artifact reads cleanly in any markdown viewer; the lists
// use plain `-` bullets so a future renderer doesn't need a custom
// extension.
func renderMarkdown(w walk.Walk, endedAt time.Time) string {
	dec := countDecisions(w.Decisions)
	ag := countAgenda(w.Agenda)
	duration := endedAt.Sub(w.StartedAt)

	title := w.Label
	if strings.TrimSpace(title) == "" {
		title = string(w.ID)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "**Walk ID:** `%s`  \n", w.ID)
	fmt.Fprintf(&b, "**Workspace:** %s  \n", emptyDash(w.Workspace))
	fmt.Fprintf(&b, "**Initiated by:** %s  \n", emptyDash(string(w.InitiatedBy)))
	fmt.Fprintf(&b, "**Primary persona:** %s  \n", emptyDash(w.PrimaryPersona))
	if len(w.AssistingPersonas) > 0 {
		fmt.Fprintf(&b, "**Assisting:** %s  \n", strings.Join(w.AssistingPersonas, ", "))
	}
	fmt.Fprintf(&b, "**Started:** %s  \n", w.StartedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "**Ended:** %s (%s)  \n", endedAt.UTC().Format(time.RFC3339), formatDuration(duration))
	fmt.Fprintf(&b, "**Status:** %s\n\n", w.Status)

	b.WriteString("## Decisions\n\n")
	fmt.Fprintf(&b, "- Ratified: %d\n", dec.Ratify)
	fmt.Fprintf(&b, "- Modified: %d\n", dec.Modify)
	fmt.Fprintf(&b, "- Rejected: %d\n", dec.Reject)
	fmt.Fprintf(&b, "- Deferred: %d\n", dec.Defer)
	fmt.Fprintf(&b, "- Handoff: %d\n\n", dec.Handoff)

	b.WriteString("## Agenda\n\n")
	fmt.Fprintf(&b, "- Total items: %d\n", ag.Total)
	fmt.Fprintf(&b, "- Resolved: %d\n", ag.Resolved)
	fmt.Fprintf(&b, "- Carried forward: %d\n", ag.CarriedForward)
	if ag.Dismissed > 0 {
		fmt.Fprintf(&b, "- Dismissed: %d\n", ag.Dismissed)
	}
	if len(ag.BySource) > 0 {
		b.WriteString("\nBy source:\n")
		// Stable order for deterministic test output.
		kinds := make([]string, 0, len(ag.BySource))
		for k := range ag.BySource {
			kinds = append(kinds, string(k))
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			fmt.Fprintf(&b, "- %s: %d\n", k, ag.BySource[walk.AgendaSourceKind(k)])
		}
	}
	b.WriteString("\n")

	b.WriteString("## Beads touched\n\n")
	if len(w.BeadsTouched) == 0 {
		b.WriteString("_None._\n\n")
	} else {
		for _, id := range w.BeadsTouched {
			fmt.Fprintf(&b, "- `%s`\n", id)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Cost\n\n")
	fmt.Fprintf(&b, "$%.4f · %d in / %d out tokens\n\n", w.Cost.Dollars, w.Cost.TokensIn, w.Cost.TokensOut)

	b.WriteString("## Narrative\n\n")
	b.WriteString(narrative(w, dec, ag, duration))
	b.WriteString("\n")

	return b.String()
}

// narrative is the deterministic v1 paragraph. Keeps to one paragraph
// regardless of input — the LLM-authored long-form replacement will
// slot in here without changing call sites. The wording reads
// gracefully for the "did nothing" case (zero decisions, zero items)
// because every clause is conditional on its underlying count.
func narrative(w walk.Walk, dec decisionCounts, ag agendaCounts, duration time.Duration) string {
	var parts []string

	if ag.Total == 0 && dec.total() == 0 {
		parts = append(parts, fmt.Sprintf(
			"This %s walk ended without any agenda items or decisions on record.",
			formatDuration(duration)))
	} else {
		parts = append(parts, fmt.Sprintf(
			"During this %s walk the operator reviewed %s.",
			formatDuration(duration),
			pluralize(ag.Total, "agenda item", "agenda items")))
	}

	if dec.total() > 0 {
		var clauses []string
		if dec.Ratify > 0 {
			clauses = append(clauses, fmt.Sprintf("ratifying %d", dec.Ratify))
		}
		if dec.Modify > 0 {
			clauses = append(clauses, fmt.Sprintf("modifying %d", dec.Modify))
		}
		if dec.Reject > 0 {
			clauses = append(clauses, fmt.Sprintf("rejecting %d", dec.Reject))
		}
		if dec.Defer > 0 {
			clauses = append(clauses, fmt.Sprintf("deferring %d", dec.Defer))
		}
		if dec.Handoff > 0 {
			clauses = append(clauses, fmt.Sprintf("handing off %d", dec.Handoff))
		}
		parts = append(parts, "The operator resolved "+joinClauses(clauses)+".")
	}

	if ag.CarriedForward > 0 {
		parts = append(parts, fmt.Sprintf(
			"%s remain on the agenda and will carry forward to the next walk.",
			pluralize(ag.CarriedForward, "item", "items")))
	}

	if len(w.BeadsTouched) > 0 {
		parts = append(parts, fmt.Sprintf(
			"Work touched %s.",
			pluralize(len(w.BeadsTouched), "bead", "beads")))
	}

	if w.Cost.Dollars > 0 || w.Cost.TokensIn > 0 || w.Cost.TokensOut > 0 {
		parts = append(parts, fmt.Sprintf(
			"Cost: $%.4f across %d input and %d output tokens.",
			w.Cost.Dollars, w.Cost.TokensIn, w.Cost.TokensOut))
	}

	return strings.Join(parts, " ")
}

// joinClauses formats a list of clauses with Oxford-comma commas:
// "ratifying 3", "ratifying 3 and modifying 1", "ratifying 3,
// modifying 1, and rejecting 2". Pure formatting helper.
func joinClauses(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}

// pluralize picks "1 thing" vs. "N things". Avoids a side-table by
// taking both forms inline; the caller knows whether the noun is
// regular or irregular.
func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// formatDuration renders a Duration in a human-friendly compact form
// — minutes/hours rather than "12m34.567890123s". Anything under a
// minute is "Ns"; under an hour is "MmSs"; otherwise "HhMm".
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d / time.Minute)
		secs := int((d % time.Minute) / time.Second)
		if secs == 0 {
			return fmt.Sprintf("%dm", mins)
		}
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	hours := int(d / time.Hour)
	mins := int((d % time.Hour) / time.Minute)
	if mins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, mins)
}

// emptyDash returns "—" for empty strings so the metadata block
// renders without literal blanks (which markdown viewers compress
// out, breaking the "key: value" grid).
func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
