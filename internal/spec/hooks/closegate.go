// Package hooks implements operator-facing safety gates that wrap bd
// lifecycle events with spec-awareness. The flagship is the ASDD
// closure gate (gm-v0sp.12):
//
// An epic bead may not close while any T-NN (story) mapped to the same
// spec is still open. A story counts as "resolved" when either:
//
//   - its bd bead status is "closed", OR
//   - its lockfile Mapping has Resolution == "wontdo".
//
// Anything else (open, in_progress, blocked, unknown) blocks the close.
//
// Operators can override with Force=true + a non-empty Reason; the gate
// emits a `spec.close.gate.bypass` slog audit record so the override is
// traceable.
package hooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/GembaCore/gemba-core/internal/spec/lockfile"
	"github.com/GembaCore/gemba-core/internal/spec/parser"
	"github.com/GembaCore/gemba-core/internal/spec/reconcile"
)

// ErrForceWithoutReason is returned (and reported by the CLI) when the
// caller asks for Force=true but supplies an empty ForceReason. Refusing
// a silent override is part of the audit contract.
var ErrForceWithoutReason = errors.New("closegate: force override requires a non-empty reason")

// OpenStory is one story bead that the gate considers unresolved.
type OpenStory struct {
	Anchor string
	BeadID string
	Status string
}

// Result is the gate decision.
//
//   - Allow=true  ⇒ caller may proceed with bd close.
//   - Allow=false ⇒ caller MUST NOT close; OpenStories carries the
//     blockers and Reason explains why.
//
// On a forced bypass Allow=true and Reason carries the operator-supplied
// reason verbatim.
type Result struct {
	Allow       bool
	OpenStories []OpenStory
	Reason      string
}

// CheckOptions configures the closure gate.
type CheckOptions struct {
	// BeadLister fetches bd bead statuses by spec label. Required.
	BeadLister reconcile.BeadLister
	// Label is the spec label to query bd with. If empty, the gate
	// falls back to "spec:<spec_id>" derived from the parsed spec.
	Label string
	// Force, when true with a non-empty ForceReason, returns Allow=true
	// and writes an audit event regardless of open stories.
	Force bool
	// ForceReason is the operator's justification recorded in the audit
	// trail. Required when Force=true.
	ForceReason string
	// Logger receives the bypass audit event. If nil, slog.Default is
	// used.
	Logger *slog.Logger
}

// Check loads spec+lock, projects all story anchors, queries bd for
// their statuses through BeadLister, and decides whether epicID may be
// closed.
//
// epicID is the bd ID of the epic the caller wants to close. It is not
// itself looked up — Check only inspects the *story* beads that live
// under the same spec — but it appears in audit events so an operator
// can correlate the gate decision with the bd transition.
func Check(ctx context.Context, specPath, lockPath, epicID string, opts CheckOptions) (*Result, error) {
	if opts.Force && strings.TrimSpace(opts.ForceReason) == "" {
		return nil, ErrForceWithoutReason
	}
	if opts.BeadLister == nil {
		return nil, errors.New("closegate: BeadLister is required")
	}

	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("closegate: read spec: %w", err)
	}
	spec, err := parser.Parse(specBytes)
	if err != nil {
		return nil, fmt.Errorf("closegate: parse spec: %w", err)
	}
	lock, err := lockfile.Load(lockPath)
	if err != nil {
		return nil, fmt.Errorf("closegate: load lock: %w", err)
	}

	// Index lock mappings by anchor for O(1) lookup.
	byAnchor := make(map[string]lockfile.Mapping, len(lock.Mappings))
	for _, m := range lock.Mappings {
		byAnchor[m.Anchor] = m
	}

	// Decide which label to query bd with.
	label := opts.Label
	if label == "" {
		label = "spec:" + lock.SpecID
	}
	beads, err := opts.BeadLister.List(ctx, label)
	if err != nil {
		return nil, fmt.Errorf("closegate: bd list: %w", err)
	}
	statusByID := make(map[string]string, len(beads))
	for _, b := range beads {
		statusByID[b.ID] = b.Status
	}

	var open []OpenStory
	for _, s := range spec.Stories {
		m, ok := byAnchor[s.Anchor]
		if !ok || m.BeadID == "" {
			// Unmapped story — not gated by definition; the reconciler
			// is the one that establishes mappings. Skip.
			continue
		}
		if strings.EqualFold(m.Resolution, "wontdo") {
			continue
		}
		status := statusByID[m.BeadID]
		if strings.EqualFold(status, "closed") {
			continue
		}
		open = append(open, OpenStory{
			Anchor: s.Anchor,
			BeadID: m.BeadID,
			Status: status,
		})
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if opts.Force {
		logger.Info("spec.close.gate.bypass",
			slog.String("event", "spec.close.gate.bypass"),
			slog.String("spec_id", lock.SpecID),
			slog.String("epic_id", epicID),
			slog.String("reason", opts.ForceReason),
			slog.Int("open_stories", len(open)),
		)
		return &Result{Allow: true, OpenStories: open, Reason: opts.ForceReason}, nil
	}

	if len(open) == 0 {
		return &Result{Allow: true}, nil
	}
	return &Result{
		Allow:       false,
		OpenStories: open,
		Reason:      fmt.Sprintf("closure gate: %d open stories block epic %s", len(open), epicID),
	}, nil
}
