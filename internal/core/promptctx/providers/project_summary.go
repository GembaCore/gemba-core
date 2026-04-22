// Package providers: see doc.go for the overview.
package providers

import (
	"context"
	"errors"
	"time"

	"github.com/MikeBengtson/gemba/internal/core/promptctx"
)

// ProjectSummarySource supplies the narrative that ProjectSummaryProvider
// returns. Concrete adapters (documentarian-maintained README block,
// .gemba/project-summary.md, or a CMS fetch) live outside core.
type ProjectSummarySource interface {
	// Summary returns the current project summary prose. Context is
	// forwarded so network-backed sources can honor cancellation.
	Summary(ctx context.Context) (string, error)
}

// ProjectSummaryProvider returns the Documentarian-maintained
// "what is this project" narrative (gm-eiw seed).
//
// Options:
//
//   - "max_tokens" (int, optional) — soft upper bound surfaced in
//     Result.TokenEstimate. Enforcement (actual truncation) is the
//     caller's job; this provider reports the desired cap so the
//     PromptEnvelope can decide.
type ProjectSummaryProvider struct {
	src ProjectSummarySource
	ttl time.Duration
}

// NewProjectSummaryProvider returns a provider backed by src. ttl
// defaults to 5m when <= 0 — project summaries change rarely; a short
// TTL lets an in-editor edit propagate quickly without thrashing.
func NewProjectSummaryProvider(src ProjectSummarySource, ttl time.Duration) (*ProjectSummaryProvider, error) {
	if src == nil {
		return nil, errors.New("promptctx/providers: NewProjectSummaryProvider: src is nil")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &ProjectSummaryProvider{src: src, ttl: ttl}, nil
}

// ID implements promptctx.Provider.
func (p *ProjectSummaryProvider) ID() promptctx.ProviderID {
	return promptctx.ProviderProjectSummary
}

// TTL implements promptctx.Provider.
func (p *ProjectSummaryProvider) TTL() time.Duration { return p.ttl }

// Refresh implements promptctx.Provider. Recognizes "max_tokens"; any
// other option is ignored without error (persona configs may declare
// options this provider does not consume — that is not a failure).
func (p *ProjectSummaryProvider) Refresh(ctx context.Context, opts map[string]any) (*promptctx.Result, error) {
	content, err := p.src.Summary(ctx)
	if err != nil {
		return nil, err
	}
	res := &promptctx.Result{
		ProviderID: promptctx.ProviderProjectSummary,
		Content:    content,
	}
	if v, ok := opts["max_tokens"]; ok {
		if n, ok := intOption(v); ok {
			res.TokenEstimate = n
		}
	}
	return res, nil
}

// intOption parses an int option that may arrive as int, int64, or
// float64 (TOML encoders vary).
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
