package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Bead is the minimal projection of a bd bead that the reconciler needs.
// It is intentionally a subset of the bd JSON shape — we ignore fields
// that are not relevant to the diff.
type Bead struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Type     string   `json:"type"`
	Status   string   `json:"status"`
	Parent   string   `json:"parent,omitempty"`
	Priority string   `json:"priority,omitempty"`
	Assignee string   `json:"assignee,omitempty"`
	Labels   []string `json:"labels,omitempty"`
	Anchor   string   `json:"anchor,omitempty"` // surfaced from spec:<slug>#<anchor> label, if present
}

// BeadLister fetches the current bead set for a spec label.
//
// The default implementation shells out to `bd list --json --label spec:<slug>`.
// Tests stub this interface to bypass the real bd binary.
type BeadLister interface {
	List(ctx context.Context, label string) ([]Bead, error)
}

// CommandRunner runs an external command. Tests inject a stub to avoid
// shelling out.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultRunner shells out to the named binary.
func DefaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// bdLister is the production BeadLister that calls `bd list --json`.
type bdLister struct {
	bin    string
	runner CommandRunner
}

// NewBeadLister returns a BeadLister that shells out to `bd`. If bin is
// empty, "bd" is used; the binary is resolved from PATH. If runner is nil,
// DefaultRunner is used.
func NewBeadLister(bin string, runner CommandRunner) BeadLister {
	if bin == "" {
		bin = "bd"
	}
	if runner == nil {
		runner = DefaultRunner
	}
	return &bdLister{bin: bin, runner: runner}
}

func (l *bdLister) List(ctx context.Context, label string) ([]Bead, error) {
	out, err := l.runner(ctx, l.bin, "list", "--json", "--label", label)
	if err != nil {
		return nil, fmt.Errorf("reconcile: bd list failed: %w", err)
	}
	return parseBeadList(out)
}

// parseBeadList accepts either a top-level JSON array of beads or an
// object with an "issues" field (bd's actual output shape).
func parseBeadList(data []byte) ([]Bead, error) {
	if len(data) == 0 {
		return nil, nil
	}
	// Try array first.
	var arr []Bead
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, nil
	}
	// Fallback: { "issues": [...] } shape.
	var wrap struct {
		Issues []Bead `json:"issues"`
	}
	if err := json.Unmarshal(data, &wrap); err == nil {
		return wrap.Issues, nil
	}
	return nil, fmt.Errorf("reconcile: bd list output not recognized")
}

// LoadBeads is a convenience wrapper that builds the default lister and
// calls List. Tests should construct a stub BeadLister and call its List
// method directly rather than going through LoadBeads.
func LoadBeads(ctx context.Context, label string) ([]Bead, error) {
	return NewBeadLister("", nil).List(ctx, label)
}

// StaticBeadLister is a BeadLister backed by a fixed slice — useful for
// tests and for the CLI --bd-stub flag.
type StaticBeadLister struct {
	Beads []Bead
}

// List ignores label and returns the static slice.
func (s *StaticBeadLister) List(ctx context.Context, label string) ([]Bead, error) {
	return s.Beads, nil
}
