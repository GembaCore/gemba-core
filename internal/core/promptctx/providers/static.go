// Package providers: see doc.go for the overview.
package providers

import (
	"context"
	"errors"
	"time"

	"github.com/GembaCore/gemba-core/internal/core/promptctx"
)

// StaticProvider serves a fixed Content string. Useful as a baseline
// for tests, for pinning a project_summary to a committed blurb, and
// for any "the answer doesn't change at runtime" case.
type StaticProvider struct {
	id      promptctx.ProviderID
	content string
	ttl     time.Duration
}

// NewStaticProvider returns a StaticProvider for the given ID serving
// content. TTL may be zero, in which case the provider declares a
// very long TTL (24h) — static content rarely needs re-fetching.
func NewStaticProvider(id promptctx.ProviderID, content string, ttl time.Duration) (*StaticProvider, error) {
	if id == "" {
		return nil, errors.New("promptctx/providers: NewStaticProvider: id is empty")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &StaticProvider{id: id, content: content, ttl: ttl}, nil
}

// ID implements promptctx.Provider.
func (p *StaticProvider) ID() promptctx.ProviderID { return p.id }

// TTL implements promptctx.Provider.
func (p *StaticProvider) TTL() time.Duration { return p.ttl }

// Refresh implements promptctx.Provider. Options are ignored.
func (p *StaticProvider) Refresh(_ context.Context, _ map[string]any) (*promptctx.Result, error) {
	return &promptctx.Result{
		ProviderID: p.id,
		Content:    p.content,
	}, nil
}
