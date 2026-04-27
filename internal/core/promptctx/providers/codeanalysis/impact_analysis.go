// Package codeanalysis: see doc.go for the overview.
package codeanalysis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/core/codeanalysis"
	"github.com/MikeBengtson/gemba/internal/core/promptctx"
)

// ImpactAnalysisProvider renders the cascade-of-change for a
// symbol (gm-bro). Architect + Code Reviewer consume it by
// default to size a proposed change.
//
// Options:
//
//   - "symbol" (string, REQUIRED) — the qualified symbol whose
//     blast-radius is being requested.
//   - "direction" (string, optional, default "upstream") — one of
//     "upstream" / "downstream" / "both". Invalid values fall
//     back to the default with a Note recorded in Payload.
type ImpactAnalysisProvider struct {
	backend          codeanalysis.Provider
	repo             codeanalysis.RepoRef
	ttl              time.Duration
	defaultDirection codeanalysis.ImpactDirection
}

// NewImpactAnalysisProvider builds an ImpactAnalysisProvider.
// ttl defaults to 1m when <= 0. defaultDirection defaults to
// upstream when empty — that's the "what would BREAK if I
// change this?" question Architect and Code Reviewer ask most
// often.
func NewImpactAnalysisProvider(backend codeanalysis.Provider, repo codeanalysis.RepoRef, defaultDirection codeanalysis.ImpactDirection, ttl time.Duration) (*ImpactAnalysisProvider, error) {
	if backend == nil {
		return nil, errors.New("promptctx/providers/codeanalysis: NewImpactAnalysisProvider: backend is nil")
	}
	if repo.Name == "" {
		return nil, errors.New("promptctx/providers/codeanalysis: NewImpactAnalysisProvider: repo.Name is empty")
	}
	if defaultDirection == "" {
		defaultDirection = codeanalysis.ImpactUpstream
	}
	if !defaultDirection.IsValid() {
		return nil, fmt.Errorf("promptctx/providers/codeanalysis: NewImpactAnalysisProvider: invalid defaultDirection %q", defaultDirection)
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &ImpactAnalysisProvider{
		backend:          backend,
		repo:             repo,
		ttl:              ttl,
		defaultDirection: defaultDirection,
	}, nil
}

// ID implements promptctx.Provider.
func (p *ImpactAnalysisProvider) ID() promptctx.ProviderID {
	return promptctx.ProviderImpactAnalysis
}

// TTL implements promptctx.Provider.
func (p *ImpactAnalysisProvider) TTL() time.Duration { return p.ttl }

// Refresh implements promptctx.Provider. Requires "symbol".
func (p *ImpactAnalysisProvider) Refresh(ctx context.Context, opts map[string]any) (*promptctx.Result, error) {
	symbol, ok := stringOption(opts["symbol"])
	if !ok || symbol == "" {
		return nil, errors.New("impact_analysis: option \"symbol\" is required")
	}
	dir := p.defaultDirection
	dirNote := ""
	if v, ok := stringOption(opts["direction"]); ok && v != "" {
		cand := codeanalysis.ImpactDirection(v)
		if cand.IsValid() {
			dir = cand
		} else {
			dirNote = fmt.Sprintf("ignored invalid direction %q; using %q", v, dir)
		}
	}
	report, err := p.backend.Impact(ctx, p.repo, symbol, dir)
	if err != nil {
		return nil, fmt.Errorf("impact_analysis: Impact(%s, %s, %s): %w", p.repo.Name, symbol, dir, err)
	}

	payload := map[string]any{
		"repo":           p.repo,
		"symbol":         symbol,
		"direction":      string(dir),
		"depth":          report.Depth,
		"affected":       report.Affected,
		"affected_count": len(report.Affected),
		"risk":           string(report.Risk),
		"notes":          report.Notes,
	}
	if dirNote != "" {
		payload["direction_note"] = dirNote
	}
	return &promptctx.Result{
		ProviderID: promptctx.ProviderImpactAnalysis,
		Content:    renderImpact(p.repo, symbol, dir, report, dirNote),
		Payload:    payload,
	}, nil
}

// renderImpact formats the impact report as a compact list. The
// rendered form is deliberately bullet-friendly so the prompt
// envelope can splice it without re-flowing.
func renderImpact(repo codeanalysis.RepoRef, symbol string, dir codeanalysis.ImpactDirection, report codeanalysis.ImpactReport, dirNote string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Impact analysis for %q in %s (%s", symbol, repo.Name, dir)
	if report.Depth > 0 {
		fmt.Fprintf(&b, ", depth=%d", report.Depth)
	}
	b.WriteString("):\n")
	if dirNote != "" {
		fmt.Fprintf(&b, "- %s\n", dirNote)
	}
	if len(report.Affected) == 0 {
		b.WriteString("- no affected symbols surfaced by the backend\n")
	} else {
		fmt.Fprintf(&b, "- affected (%d):\n", len(report.Affected))
		for _, s := range report.Affected {
			b.WriteString("  - ")
			b.WriteString(s.Name)
			if s.File != "" {
				fmt.Fprintf(&b, " (%s", s.File)
				if s.Line > 0 {
					fmt.Fprintf(&b, ":%d", s.Line)
				}
				b.WriteString(")")
			}
			b.WriteString("\n")
		}
	}
	if report.Risk != "" {
		fmt.Fprintf(&b, "- risk: %s\n", report.Risk)
	}
	for _, n := range report.Notes {
		b.WriteString("- note: ")
		b.WriteString(n)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
