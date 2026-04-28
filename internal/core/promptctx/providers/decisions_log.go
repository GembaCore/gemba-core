// Package providers: see doc.go for the overview.
package providers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/core/promptctx"
)

// Decision is one ratified design decision surfaced in the log. The
// shape mirrors the DD-<n> citation format used in workspace design
// docs and is intentionally flat — the log is narrative
// scaffolding, not a typed audit record.
type Decision struct {
	ID         string // e.g. "DD-24"
	Title      string // short headline
	Summary    string // one-paragraph rationale
	RatifiedAt time.Time
	Citations  []string // free-form: bead IDs, file paths, RFC sections
}

// DecisionsLogSource yields the latest ratified decisions in
// reverse-chronological order (most recent first). Adapters MAY page
// internally — the provider asks for at most lastN.
type DecisionsLogSource interface {
	// Decisions returns the most recent lastN decisions. If lastN <= 0,
	// adapters MUST apply their own sensible default.
	Decisions(ctx context.Context, lastN int) ([]Decision, error)
}

// DecisionsLogProvider renders the decisions log as narrative context
// (gm-eiw seed).
//
// Options:
//
//   - "last_n" (int, optional, default 20) — how many decisions to
//     include in the rendered log.
type DecisionsLogProvider struct {
	src          DecisionsLogSource
	defaultLastN int
	ttl          time.Duration
}

// NewDecisionsLogProvider returns a provider backed by src. ttl
// defaults to 2m when <= 0.
func NewDecisionsLogProvider(src DecisionsLogSource, defaultLastN int, ttl time.Duration) (*DecisionsLogProvider, error) {
	if src == nil {
		return nil, errors.New("promptctx/providers: NewDecisionsLogProvider: src is nil")
	}
	if defaultLastN <= 0 {
		defaultLastN = 20
	}
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	return &DecisionsLogProvider{src: src, defaultLastN: defaultLastN, ttl: ttl}, nil
}

// ID implements promptctx.Provider.
func (p *DecisionsLogProvider) ID() promptctx.ProviderID {
	return promptctx.ProviderDecisionsLog
}

// TTL implements promptctx.Provider.
func (p *DecisionsLogProvider) TTL() time.Duration { return p.ttl }

// Refresh implements promptctx.Provider.
func (p *DecisionsLogProvider) Refresh(ctx context.Context, opts map[string]any) (*promptctx.Result, error) {
	lastN := p.defaultLastN
	if v, ok := opts["last_n"]; ok {
		if n, ok := intOption(v); ok && n > 0 {
			lastN = n
		}
	}
	decisions, err := p.src.Decisions(ctx, lastN)
	if err != nil {
		return nil, err
	}
	payload := make(map[string]any, 2)
	payload["count"] = len(decisions)
	payload["last_n"] = lastN
	return &promptctx.Result{
		ProviderID: promptctx.ProviderDecisionsLog,
		Content:    renderDecisions(decisions),
		Payload:    payload,
	}, nil
}

// renderDecisions formats the slice as a compact, grep-friendly log.
// Personas read this as prose context, so the Markdown-ish form is
// fine; no HTML/escape concerns at this layer.
func renderDecisions(ds []Decision) string {
	if len(ds) == 0 {
		return "(no ratified decisions yet)"
	}
	var b strings.Builder
	for i, d := range ds {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("- ")
		b.WriteString(d.ID)
		if d.Title != "" {
			b.WriteString(": ")
			b.WriteString(d.Title)
		}
		if !d.RatifiedAt.IsZero() {
			b.WriteString(" (")
			b.WriteString(d.RatifiedAt.Format("2006-01-02"))
			b.WriteString(")")
		}
		if d.Summary != "" {
			b.WriteString("\n  ")
			b.WriteString(d.Summary)
		}
		if len(d.Citations) > 0 {
			b.WriteString("\n  cites: ")
			b.WriteString(strings.Join(d.Citations, ", "))
		}
	}
	return b.String()
}
