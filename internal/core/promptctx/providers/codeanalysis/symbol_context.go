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

// SymbolContextProvider renders on-demand symbol context for a
// persona-named symbol (gm-bro). Architect and Code Reviewer
// pull this in when a request mentions a specific symbol.
//
// The seed [codeanalysis.Provider] interface does NOT expose a
// dedicated Context query, so this provider derives a 360°
// view by issuing an [codeanalysis.ImpactBoth] query and
// labelling the result. When backends grow a richer Context
// method, a separate provider can claim the same ID.
//
// Options:
//
//   - "symbol" (string, REQUIRED) — the qualified symbol the
//     backend understands (function name, type, file path —
//     whatever the backend keys on).
//   - "depth" (int, optional, advisory) — surfaced in Payload so
//     the caller can render depth-based hints; the backend
//     decides whether/how to honour it.
type SymbolContextProvider struct {
	backend codeanalysis.Provider
	repo    codeanalysis.RepoRef
	ttl     time.Duration
}

// NewSymbolContextProvider builds a SymbolContextProvider. ttl
// defaults to 1m when <= 0 — symbol context follows source-file
// edits closely, so it should expire faster than the summary.
func NewSymbolContextProvider(backend codeanalysis.Provider, repo codeanalysis.RepoRef, ttl time.Duration) (*SymbolContextProvider, error) {
	if backend == nil {
		return nil, errors.New("promptctx/providers/codeanalysis: NewSymbolContextProvider: backend is nil")
	}
	if repo.Name == "" {
		return nil, errors.New("promptctx/providers/codeanalysis: NewSymbolContextProvider: repo.Name is empty")
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &SymbolContextProvider{backend: backend, repo: repo, ttl: ttl}, nil
}

// ID implements promptctx.Provider.
func (p *SymbolContextProvider) ID() promptctx.ProviderID {
	return promptctx.ProviderSymbolContext
}

// TTL implements promptctx.Provider.
func (p *SymbolContextProvider) TTL() time.Duration { return p.ttl }

// Refresh implements promptctx.Provider. Returns a descriptive
// error when "symbol" is missing — without a target the
// provider has nothing to render.
func (p *SymbolContextProvider) Refresh(ctx context.Context, opts map[string]any) (*promptctx.Result, error) {
	symbol, ok := stringOption(opts["symbol"])
	if !ok || symbol == "" {
		return nil, errors.New("symbol_context: option \"symbol\" is required")
	}
	report, err := p.backend.Impact(ctx, p.repo, symbol, codeanalysis.ImpactBoth)
	if err != nil {
		return nil, fmt.Errorf("symbol_context: Impact(%s, %s): %w", p.repo.Name, symbol, err)
	}

	depth := 0
	if v, ok := opts["depth"]; ok {
		if n, ok := intOption(v); ok && n > 0 {
			depth = n
		}
	}

	payload := map[string]any{
		"repo":            p.repo,
		"symbol":          symbol,
		"direction":       string(codeanalysis.ImpactBoth),
		"requested_depth": depth,
		"affected":        report.Affected,
		"affected_count":  len(report.Affected),
		"risk":            string(report.Risk),
		"notes":           report.Notes,
	}
	return &promptctx.Result{
		ProviderID: promptctx.ProviderSymbolContext,
		Content:    renderSymbolContext(p.repo, symbol, report),
		Payload:    payload,
	}, nil
}

// renderSymbolContext formats the symbol's neighbourhood
// (callers/callees collapsed; the backend chose ImpactBoth) as
// a compact bullet list.
func renderSymbolContext(repo codeanalysis.RepoRef, symbol string, report codeanalysis.ImpactReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Symbol context for %q in %s:\n", symbol, repo.Name)
	if len(report.Affected) == 0 {
		b.WriteString("- no neighbouring symbols surfaced by the backend\n")
	} else {
		fmt.Fprintf(&b, "- neighbours (%d, callers+callees):\n", len(report.Affected))
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
