// Package providers: see doc.go for the overview.
package providers

import (
	"context"
	"errors"
	"time"

	"github.com/MikeBengtson/gemba/internal/core/promptctx"
)

// FuncProvider adapts a plain function into a promptctx.Provider. It
// is the no-boilerplate escape hatch for glue code and tests — the
// caller supplies the refresh logic inline instead of declaring a new
// struct.
type FuncProvider struct {
	id  promptctx.ProviderID
	ttl time.Duration
	fn  func(ctx context.Context, opts map[string]any) (*promptctx.Result, error)
}

// NewFuncProvider returns a FuncProvider. ttl must be > 0 so Cache
// behavior is well-defined; fn must return a non-nil Result on success
// whose ProviderID equals id (or is empty — Cache will stamp it).
func NewFuncProvider(
	id promptctx.ProviderID,
	ttl time.Duration,
	fn func(ctx context.Context, opts map[string]any) (*promptctx.Result, error),
) (*FuncProvider, error) {
	if id == "" {
		return nil, errors.New("promptctx/providers: NewFuncProvider: id is empty")
	}
	if ttl <= 0 {
		return nil, errors.New("promptctx/providers: NewFuncProvider: ttl must be positive")
	}
	if fn == nil {
		return nil, errors.New("promptctx/providers: NewFuncProvider: fn is nil")
	}
	return &FuncProvider{id: id, ttl: ttl, fn: fn}, nil
}

// ID implements promptctx.Provider.
func (p *FuncProvider) ID() promptctx.ProviderID { return p.id }

// TTL implements promptctx.Provider.
func (p *FuncProvider) TTL() time.Duration { return p.ttl }

// Refresh implements promptctx.Provider by delegating to the wrapped
// function.
func (p *FuncProvider) Refresh(ctx context.Context, opts map[string]any) (*promptctx.Result, error) {
	return p.fn(ctx, opts)
}
