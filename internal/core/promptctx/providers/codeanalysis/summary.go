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

// SummaryProvider emits the "code analysis summary" fragment:
// module inventory headline + health top-line for the configured
// repo (gm-bro). PM consumes it by default.
//
// Construct with [NewSummaryProvider]; the backend
// [codeanalysis.Provider] and the [codeanalysis.RepoRef] are
// injected at boot so the dispatcher does not need to know about
// either.
//
// Options:
//
//   - "max_modules" (int, optional, default 20) — cap on the
//     number of modules rendered in Content. The full count is
//     still surfaced in Payload.
type SummaryProvider struct {
	backend codeanalysis.Provider
	repo    codeanalysis.RepoRef
	ttl     time.Duration

	defaultMaxModules int
}

// NewSummaryProvider builds a SummaryProvider. backend MUST be
// non-nil and repo.Name MUST be non-empty. ttl defaults to 5m
// when <= 0 — module inventory is cheap to recompute, so a short
// TTL keeps the rendered context honest after a reindex.
func NewSummaryProvider(backend codeanalysis.Provider, repo codeanalysis.RepoRef, ttl time.Duration) (*SummaryProvider, error) {
	if backend == nil {
		return nil, errors.New("promptctx/providers/codeanalysis: NewSummaryProvider: backend is nil")
	}
	if repo.Name == "" {
		return nil, errors.New("promptctx/providers/codeanalysis: NewSummaryProvider: repo.Name is empty")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &SummaryProvider{
		backend:           backend,
		repo:              repo,
		ttl:               ttl,
		defaultMaxModules: 20,
	}, nil
}

// ID implements promptctx.Provider.
func (p *SummaryProvider) ID() promptctx.ProviderID {
	return promptctx.ProviderCodeAnalysisSummary
}

// TTL implements promptctx.Provider.
func (p *SummaryProvider) TTL() time.Duration { return p.ttl }

// Refresh implements promptctx.Provider. It calls Modules +
// Health on the backend and renders both as a single fragment.
// Either backend error short-circuits — the persona expects an
// authoritative answer when this provider is enabled.
func (p *SummaryProvider) Refresh(ctx context.Context, opts map[string]any) (*promptctx.Result, error) {
	maxModules := p.defaultMaxModules
	if v, ok := opts["max_modules"]; ok {
		if n, ok := intOption(v); ok && n > 0 {
			maxModules = n
		}
	}

	modules, err := p.backend.Modules(ctx, p.repo)
	if err != nil {
		return nil, fmt.Errorf("code_analysis_summary: Modules(%s): %w", p.repo.Name, err)
	}
	health, err := p.backend.Health(ctx, p.repo)
	if err != nil {
		return nil, fmt.Errorf("code_analysis_summary: Health(%s): %w", p.repo.Name, err)
	}

	payload := map[string]any{
		"repo":              p.repo,
		"module_count":      len(modules),
		"modules":           modules,
		"cycle_count":       health.CycleCount,
		"untested_count":    len(health.UntestedModules),
		"god_class_count":   len(health.GodClasses),
		"undocumented_apis": len(health.UndocumentedAPIs),
		"fetched_at":        health.FetchedAt,
		"notes":             health.Notes,
	}
	return &promptctx.Result{
		ProviderID: promptctx.ProviderCodeAnalysisSummary,
		Content:    renderSummary(p.repo, modules, health, maxModules),
		Payload:    payload,
	}, nil
}

// renderSummary formats the inventory + health top-line as
// Markdown-ish prose. Modules are listed, capped at maxModules;
// the cap and full count are mentioned when truncation occurs.
func renderSummary(repo codeanalysis.RepoRef, modules []codeanalysis.Module, health codeanalysis.HealthReport, maxModules int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Code-analysis summary for %s", repo.Name)
	if repo.Path != "" {
		fmt.Fprintf(&b, " (%s)", repo.Path)
	}
	b.WriteString(":\n")

	if len(modules) == 0 {
		b.WriteString("- modules: (backend reported no modules)\n")
	} else {
		fmt.Fprintf(&b, "- modules: %d", len(modules))
		shown := modules
		truncated := false
		if maxModules > 0 && len(shown) > maxModules {
			shown = shown[:maxModules]
			truncated = true
		}
		b.WriteString("\n")
		for _, m := range shown {
			b.WriteString("  - ")
			b.WriteString(m.Name)
			if m.Path != "" && m.Path != m.Name {
				fmt.Fprintf(&b, " (%s)", m.Path)
			}
			if m.LOC > 0 {
				fmt.Fprintf(&b, ", %d LOC", m.LOC)
			}
			if m.TestCoverage >= 0 {
				fmt.Fprintf(&b, ", coverage %.0f%%", m.TestCoverage*100)
			}
			b.WriteString("\n")
		}
		if truncated {
			fmt.Fprintf(&b, "  - … %d more (truncated to %d)\n", len(modules)-maxModules, maxModules)
		}
	}

	fmt.Fprintf(&b, "- health: cycles=%d, untested=%d, god_classes=%d, undocumented_apis=%d",
		health.CycleCount,
		len(health.UntestedModules),
		len(health.GodClasses),
		len(health.UndocumentedAPIs),
	)
	if !health.FetchedAt.IsZero() {
		fmt.Fprintf(&b, " (fetched %s)", health.FetchedAt.UTC().Format(time.RFC3339))
	}
	b.WriteString("\n")
	for _, n := range health.Notes {
		b.WriteString("- note: ")
		b.WriteString(n)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// intOption parses an int option that may arrive as int, int64,
// or float64 (TOML decoders vary). Mirrors the helper in the
// sibling promptctx/providers package without importing it (would
// create an awkward back-reference).
func intOption(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// stringOption parses a string option, returning ("", false) if
// the value is missing or non-string.
func stringOption(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
