// Package providers: see doc.go for the overview.
package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/core/promptctx"
)

// BeadSnapshot is the per-repo snapshot a BeadDatabaseSource returns.
// It is intentionally narrow — the Persona layer does not need the
// full WorkItem tree, only the summary surface a specialist reads to
// answer their question.
type BeadSnapshot struct {
	Repo         string // repo/db identifier (e.g. "gemba")
	OpenCount    int
	InProgress   int
	Blocked      int
	RecentTitles []string // up to ~N open/in-progress titles
	GeneratedAt  time.Time
}

// BeadDatabaseSource yields a BeadSnapshot for each requested repo.
// Per the bead description, "a project may span multiple repos each
// with its own bead DB", so scope is a set of repo identifiers.
//
// Unknown repos MUST be returned as a BeadSnapshot with Repo set and
// zero counts rather than as an error — the caller logs the gap and
// keeps going.
type BeadDatabaseSource interface {
	Snapshot(ctx context.Context, repos []string) ([]BeadSnapshot, error)
}

// BeadDatabaseProvider renders per-repo bead database snapshots as a
// compact narrative (gm-eiw seed).
//
// Options:
//
//   - "scope" ([]string, required) — the set of repo DB identifiers
//     this persona cares about. Order is preserved in the rendered
//     output. If absent, the provider's default scope is used.
type BeadDatabaseProvider struct {
	src          BeadDatabaseSource
	defaultScope []string
	ttl          time.Duration
}

// NewBeadDatabaseProvider returns a provider backed by src. At least
// one of defaultScope or the per-call "scope" option must supply
// repo identifiers; otherwise Refresh returns an error.
func NewBeadDatabaseProvider(src BeadDatabaseSource, defaultScope []string, ttl time.Duration) (*BeadDatabaseProvider, error) {
	if src == nil {
		return nil, errors.New("promptctx/providers: NewBeadDatabaseProvider: src is nil")
	}
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	// Defensive copy — the persona config layer owns the slice.
	cp := append([]string(nil), defaultScope...)
	return &BeadDatabaseProvider{src: src, defaultScope: cp, ttl: ttl}, nil
}

// ID implements promptctx.Provider.
func (p *BeadDatabaseProvider) ID() promptctx.ProviderID {
	return promptctx.ProviderBeadDatabase
}

// TTL implements promptctx.Provider.
func (p *BeadDatabaseProvider) TTL() time.Duration { return p.ttl }

// Refresh implements promptctx.Provider.
func (p *BeadDatabaseProvider) Refresh(ctx context.Context, opts map[string]any) (*promptctx.Result, error) {
	scope, err := resolveScope(opts, p.defaultScope)
	if err != nil {
		return nil, err
	}
	snaps, err := p.src.Snapshot(ctx, scope)
	if err != nil {
		return nil, err
	}
	return &promptctx.Result{
		ProviderID: promptctx.ProviderBeadDatabase,
		Content:    renderBeadSnapshots(snaps),
		Payload: map[string]any{
			"scope": scope,
			"repos": len(snaps),
		},
	}, nil
}

func resolveScope(opts map[string]any, fallback []string) ([]string, error) {
	if v, ok := opts["scope"]; ok {
		switch s := v.(type) {
		case []string:
			if len(s) == 0 {
				break
			}
			return append([]string(nil), s...), nil
		case []any:
			out := make([]string, 0, len(s))
			for i, elem := range s {
				str, ok := elem.(string)
				if !ok {
					return nil, fmt.Errorf("promptctx/providers: bead_database scope[%d] is not a string", i)
				}
				out = append(out, str)
			}
			if len(out) > 0 {
				return out, nil
			}
		default:
			return nil, fmt.Errorf("promptctx/providers: bead_database scope has unsupported type %T", v)
		}
	}
	if len(fallback) == 0 {
		return nil, errors.New("promptctx/providers: bead_database requires a non-empty scope (option or default)")
	}
	return append([]string(nil), fallback...), nil
}

func renderBeadSnapshots(snaps []BeadSnapshot) string {
	if len(snaps) == 0 {
		return "(no bead databases in scope)"
	}
	var b strings.Builder
	for i, s := range snaps {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "repo: %s — open=%d in_progress=%d blocked=%d",
			s.Repo, s.OpenCount, s.InProgress, s.Blocked)
		if !s.GeneratedAt.IsZero() {
			fmt.Fprintf(&b, " (as of %s)", s.GeneratedAt.Format(time.RFC3339))
		}
		for _, t := range s.RecentTitles {
			b.WriteString("\n  - ")
			b.WriteString(t)
		}
	}
	return b.String()
}
