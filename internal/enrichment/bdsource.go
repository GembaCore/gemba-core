package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

// BdJSONSource is a [BeadSource] backed by `bd list --json`. Walks
// every bead the bd binary surfaces; the runner uses the id, title,
// and description fields for [BeadInput].
//
// The exec hook is injected so tests can stub `bd` without spawning
// a subprocess. Production calls fall through to os/exec.
type BdJSONSource struct {
	// Bin is the path to the bd binary. Empty → "bd" looked up
	// on PATH.
	Bin string

	// Cwd is the working directory the bd subprocess runs in. The
	// bd CLI resolves its workspace from cwd, so this gates which
	// beads database the source reads from. Empty → inherit.
	Cwd string

	// IncludeAll mirrors `bd list --all`: includes closed beads.
	// Default (false) limits the source to active beads.
	IncludeAll bool

	// exec is the subprocess hook. Tests stub; production leaves nil.
	exec func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Compile-time interface check.
var _ BeadSource = (*BdJSONSource)(nil)

// bdRecord mirrors the fields the source actually reads from the
// `bd list --json` output. Adding a field is a one-line change here
// and a one-line change in [Iter] to surface it on [BeadInput].
type bdRecord struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// Iter implements [BeadSource]. Reads + parses the bd JSON in one
// pass; for the workspace sizes the planner cares about (a few
// thousand beads max) the materialise-then-yield approach is fine
// and keeps the parse error path simple.
func (s *BdJSONSource) Iter(ctx context.Context, yield func(BeadInput) bool) error {
	bin := s.Bin
	if bin == "" {
		bin = "bd"
	}
	args := []string{"list", "--json"}
	if s.IncludeAll {
		args = append(args, "--all")
	}
	out, err := s.run(ctx, bin, args...)
	if err != nil {
		return fmt.Errorf("bd list: %w", err)
	}

	var rows []bdRecord
	if len(out) > 0 {
		if err := json.Unmarshal(out, &rows); err != nil {
			return fmt.Errorf("bd list: parse JSON: %w", err)
		}
	}
	for _, r := range rows {
		if r.ID == "" {
			continue
		}
		ok := yield(BeadInput{
			BeadID: r.ID,
			Title:  r.Title,
			Body:   r.Description,
		})
		if !ok {
			return nil
		}
	}
	return nil
}

func (s *BdJSONSource) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if s.exec != nil {
		return s.exec(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if s.Cwd != "" {
		cmd.Dir = s.Cwd
	}
	out, err := cmd.Output()
	if err != nil {
		// Surface the missing-binary case so the CLI can branch:
		// no bd installed → can't backfill, surface a friendly
		// message.
		var pathErr *exec.Error
		if errors.As(err, &pathErr) {
			return nil, fmt.Errorf("%s not on PATH: %w", name, err)
		}
		return nil, err
	}
	return out, nil
}

// MemoryBeadSource is a [BeadSource] backed by an in-memory slice.
// Used by tests + by callers that want to feed a curated list
// (e.g. a JSONL file an external tool produced).
type MemoryBeadSource struct {
	Beads []BeadInput
}

// Iter implements [BeadSource].
func (s *MemoryBeadSource) Iter(_ context.Context, yield func(BeadInput) bool) error {
	for _, b := range s.Beads {
		if !yield(b) {
			return nil
		}
	}
	return nil
}
