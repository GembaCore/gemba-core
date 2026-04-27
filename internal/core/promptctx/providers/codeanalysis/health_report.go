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

// HealthReportProvider renders the per-repo health report —
// cycles, untested modules, god-classes, undocumented APIs
// (gm-bro). QA + Documentarian consume it by default.
//
// Options:
//
//   - "max_items" (int, optional, default 10) — per-section cap
//     for rendered items. Full counts are always surfaced in
//     Payload.
type HealthReportProvider struct {
	backend         codeanalysis.Provider
	repo            codeanalysis.RepoRef
	ttl             time.Duration
	defaultMaxItems int
	now             func() time.Time
}

// NewHealthReportProvider builds a HealthReportProvider. ttl
// defaults to 5m when <= 0 — health metrics are slow-moving but
// can shift after a major refactor; 5m is a comfortable balance.
func NewHealthReportProvider(backend codeanalysis.Provider, repo codeanalysis.RepoRef, ttl time.Duration) (*HealthReportProvider, error) {
	if backend == nil {
		return nil, errors.New("promptctx/providers/codeanalysis: NewHealthReportProvider: backend is nil")
	}
	if repo.Name == "" {
		return nil, errors.New("promptctx/providers/codeanalysis: NewHealthReportProvider: repo.Name is empty")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &HealthReportProvider{
		backend:         backend,
		repo:            repo,
		ttl:             ttl,
		defaultMaxItems: 10,
		now:             time.Now,
	}, nil
}

// ID implements promptctx.Provider.
func (p *HealthReportProvider) ID() promptctx.ProviderID {
	return promptctx.ProviderHealthReport
}

// TTL implements promptctx.Provider.
func (p *HealthReportProvider) TTL() time.Duration { return p.ttl }

// Refresh implements promptctx.Provider.
func (p *HealthReportProvider) Refresh(ctx context.Context, opts map[string]any) (*promptctx.Result, error) {
	maxItems := p.defaultMaxItems
	if v, ok := opts["max_items"]; ok {
		if n, ok := intOption(v); ok && n > 0 {
			maxItems = n
		}
	}
	report, err := p.backend.Health(ctx, p.repo)
	if err != nil {
		return nil, fmt.Errorf("health_report: Health(%s): %w", p.repo.Name, err)
	}

	stale := report.IsStale(p.now())
	payload := map[string]any{
		"repo":              p.repo,
		"fetched_at":        report.FetchedAt,
		"stale_after":       report.StaleAfter,
		"stale":             stale,
		"cycle_count":       report.CycleCount,
		"untested_modules":  report.UntestedModules,
		"god_classes":       report.GodClasses,
		"undocumented_apis": report.UndocumentedAPIs,
		"stale_code":        report.StaleCode,
		"notes":             report.Notes,
	}
	return &promptctx.Result{
		ProviderID: promptctx.ProviderHealthReport,
		Content:    renderHealth(p.repo, report, stale, maxItems),
		Payload:    payload,
	}, nil
}

// renderHealth formats the health report by section. Empty
// sections are reported as "(none)" so the persona can cite a
// clean bill of health rather than guess at omitted dimensions.
func renderHealth(repo codeanalysis.RepoRef, h codeanalysis.HealthReport, stale bool, maxItems int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Health report for %s", repo.Name)
	if !h.FetchedAt.IsZero() {
		fmt.Fprintf(&b, " (fetched %s", h.FetchedAt.UTC().Format(time.RFC3339))
		if stale {
			b.WriteString(", STALE")
		}
		b.WriteString(")")
	}
	b.WriteString(":\n")
	fmt.Fprintf(&b, "- cycles: %d\n", h.CycleCount)
	writeSection(&b, "untested modules", len(h.UntestedModules), maxItems, func(i int) string {
		return h.UntestedModules[i]
	})
	writeSection(&b, "god classes", len(h.GodClasses), maxItems, func(i int) string {
		return formatSym(h.GodClasses[i])
	})
	writeSection(&b, "undocumented APIs", len(h.UndocumentedAPIs), maxItems, func(i int) string {
		return formatSym(h.UndocumentedAPIs[i])
	})
	writeSection(&b, "stale code", len(h.StaleCode), maxItems, func(i int) string {
		return formatSym(h.StaleCode[i])
	})
	for _, n := range h.Notes {
		b.WriteString("- note: ")
		b.WriteString(n)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// writeSection writes "<title> (n)" then up to maxItems items.
// When n == 0 the line collapses to "<title>: (none)".
func writeSection(b *strings.Builder, title string, n, maxItems int, get func(i int) string) {
	if n == 0 {
		fmt.Fprintf(b, "- %s: (none)\n", title)
		return
	}
	fmt.Fprintf(b, "- %s (%d):\n", title, n)
	limit := n
	if maxItems > 0 && limit > maxItems {
		limit = maxItems
	}
	for i := 0; i < limit; i++ {
		fmt.Fprintf(b, "  - %s\n", get(i))
	}
	if limit < n {
		fmt.Fprintf(b, "  - … %d more (truncated to %d)\n", n-limit, limit)
	}
}

// formatSym renders a SymbolRef with optional file:line.
func formatSym(s codeanalysis.SymbolRef) string {
	if s.File == "" {
		return s.Name
	}
	if s.Line > 0 {
		return fmt.Sprintf("%s (%s:%d)", s.Name, s.File, s.Line)
	}
	return fmt.Sprintf("%s (%s)", s.Name, s.File)
}
