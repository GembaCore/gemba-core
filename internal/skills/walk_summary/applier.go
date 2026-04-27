package walk_summary

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/walk"
)

// Applier writes Output.Markdown to disk under a workspace root.
// Pure-ish on purpose: the summary generator stays a value-in →
// value-out function so it's trivially testable, and every
// filesystem-touching step lives here. The applier MUST NOT mutate
// the walk struct or call back into Generate — Run owns that
// composition.
type Applier struct {
	root string
}

// NewApplier returns an applier that writes under root. Pass an
// empty root to disable file writes — callers that want "generate
// only" pass nil for the applier instead, which keeps the
// distinction explicit at the call site.
func NewApplier(root string) *Applier {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	return &Applier{root: root}
}

// Root returns the configured workspace root. Exposed for diagnostic
// logging at the call site.
func (a *Applier) Root() string {
	if a == nil {
		return ""
	}
	return a.root
}

// Apply writes out.Markdown to root/out.RelativePath, creating any
// missing parent directories. Returns the absolute path written so
// the caller can log it. The write is atomic at the filename level:
// content lands in a temp sibling, then renames over the target, so
// a crash mid-write never leaves a half-written summary readable
// from another process.
//
// Apply rejects:
//   - empty out.RelativePath (the generator promises non-empty)
//   - paths that escape root (defense in depth — slug() never
//     produces "..", but PathHint is operator-supplied)
//   - empty out.Markdown (a blank summary is almost certainly a bug)
func (a *Applier) Apply(ctx context.Context, out Output) (string, error) {
	if a == nil {
		return "", errors.New("walk_summary: nil applier")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.RelativePath) == "" {
		return "", errors.New("walk_summary: output.relative_path is empty")
	}
	if strings.TrimSpace(out.Markdown) == "" {
		return "", errors.New("walk_summary: output.markdown is empty")
	}

	rel := filepath.FromSlash(out.RelativePath)
	abs := filepath.Join(a.root, rel)
	rootAbs, err := filepath.Abs(a.root)
	if err != nil {
		return "", fmt.Errorf("walk_summary: resolve root %q: %w", a.root, err)
	}
	targetAbs, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("walk_summary: resolve target %q: %w", abs, err)
	}
	if !strings.HasPrefix(targetAbs+string(filepath.Separator), rootAbs+string(filepath.Separator)) &&
		targetAbs != rootAbs {
		return "", fmt.Errorf("walk_summary: target %q escapes root %q", targetAbs, rootAbs)
	}

	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return "", fmt.Errorf("walk_summary: mkdir %q: %w", filepath.Dir(targetAbs), err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(targetAbs), ".walk-summary-*.md.tmp")
	if err != nil {
		return "", fmt.Errorf("walk_summary: create temp: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup: on the happy path Rename consumes the temp
	// and Remove no-ops; on a write/close failure this drops the
	// orphan so a future run doesn't trip over it.
	defer os.Remove(tmpName) //nolint:errcheck

	if _, err := tmp.WriteString(out.Markdown); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("walk_summary: write %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("walk_summary: close %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, targetAbs); err != nil {
		return "", fmt.Errorf("walk_summary: rename → %q: %w", targetAbs, err)
	}
	return targetAbs, nil
}

// Skill bundles Generate + Applier behind one Run call so the End
// handler doesn't import both. A nil Applier means "generate only";
// Run still returns a populated Output with RelativePath so the
// SSE event payload can carry the would-be path for the SPA's
// preview pane.
type Skill struct {
	applier *Applier
}

// New returns a Skill bound to applier (which may be nil for
// generate-only). Stateless beyond the applier root, so a single
// instance can serve every End call concurrently.
func New(applier *Applier) *Skill { return &Skill{applier: applier} }

// HasApplier reports whether file writes are enabled. The End
// handler uses it to decide whether to log a path on success.
func (s *Skill) HasApplier() bool { return s != nil && s.applier != nil }

// Run renders the summary and (when an applier is configured)
// writes it. Returns the Output unconditionally so callers can
// surface the markdown body even when persistence is disabled, plus
// the absolute path written ("" when no applier).
//
// TODO(gm-tfy): once the Checkpoint epic_stop trigger ships, fire
// it here so the walk summary and the corresponding checkpoint are
// produced atomically (currently the trigger only exists in design,
// gm-tfy is RATIFIED but the implementation follow-ups
// (gm-checkpoint-core etc.) haven't landed).
func (s *Skill) Run(ctx context.Context, w walk.Walk, now time.Time) (Output, string, error) {
	if s == nil {
		return Output{}, "", errors.New("walk_summary: nil skill")
	}
	out, err := Generate(Input{Walk: w, Now: now})
	if err != nil {
		return Output{}, "", err
	}
	if s.applier == nil {
		return out, "", nil
	}
	abs, err := s.applier.Apply(ctx, out)
	if err != nil {
		return out, "", err
	}
	return out, abs, nil
}
