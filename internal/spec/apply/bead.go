// Package apply executes a reconcile.Plan against bd, gated by the spec's
// nonce. Every mutation carries an X-GEMBA-Confirm header derived from the
// spec nonce; duplicate (nonce, anchor) tuples are skipped (idempotency),
// and partial failures trigger a Rollback that reverses prior successes.
package apply

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GembaCore/gemba-core/internal/spec/reconcile"
)

// BeadMutator is the narrow interface Apply depends on to mutate beads.
// Production wires the BDMutator that shells out to `bd ... --json`;
// tests inject a stub that records calls and can simulate failure.
type BeadMutator interface {
	// Create makes a new bead matching op. Returns the new bead's ID.
	Create(ctx context.Context, op reconcile.Op) (id string, err error)
	// Update mutates an existing bead per op.FieldDiffs (non-informational
	// entries only). The bead ID is op.BeadID.
	Update(ctx context.Context, op reconcile.Op) error
	// Close marks a bead as closed (used for orphan pruning) with reason.
	Close(ctx context.Context, op reconcile.Op, reason string) error
	// Rollback reverses any successful ops captured in the receipt.
	// Implementations should be best-effort: log and continue on per-op
	// failure rather than aborting the whole rollback.
	Rollback(ctx context.Context, receipt *Receipt) error
}

// Auditor is the narrow audit interface Apply consumes. Implementations
// MUST be non-blocking — failures here never abort apply.
type Auditor interface {
	Emit(ctx context.Context, event string, payload map[string]any)
}

// BDMutator is the production BeadMutator that shells out to `bd`.
//
// Idempotency:
//
//	Every mutation appends a record to `.gemba/.apply.log.jsonl` keyed by
//	(nonce, anchor, kind). Before invoking bd, Create/Update/Close check
//	the log and skip if the tuple is already recorded. This lets a partial
//	apply be safely replayed.
//
// Header propagation:
//
//	`bd` does not currently accept an arbitrary `-H` header. The mutator
//	therefore writes the X-GEMBA-Confirm value into the sidecar log; the
//	header semantics are emulated via the (nonce, anchor) primary key.
type BDMutator struct {
	Bin     string // empty -> "bd"
	LogPath string // empty -> "<cwd>/.gemba/.apply.log.jsonl"
	Nonce   string // X-GEMBA-Confirm
	Runner  func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// applyLogEntry records one bd mutation for idempotency replay.
type applyLogEntry struct {
	Nonce  string `json:"nonce"`
	Anchor string `json:"anchor"`
	Kind   string `json:"kind"`
	BeadID string `json:"bead_id,omitempty"`
}

func (b *BDMutator) bin() string {
	if b.Bin == "" {
		return "bd"
	}
	return b.Bin
}

func (b *BDMutator) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if b.Runner != nil {
		return b.Runner(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).Output()
}

func (b *BDMutator) logPath() string {
	if b.LogPath != "" {
		return b.LogPath
	}
	return filepath.Join(".gemba", ".apply.log.jsonl")
}

// alreadyApplied returns true when (nonce, anchor, kind) is already
// recorded in the sidecar log.
func (b *BDMutator) alreadyApplied(anchor, kind string) (bool, string) {
	f, err := os.Open(b.logPath())
	if err != nil {
		return false, ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e applyLogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		if e.Nonce == b.Nonce && e.Anchor == anchor && e.Kind == kind {
			return true, e.BeadID
		}
	}
	return false, ""
}

func (b *BDMutator) record(entry applyLogEntry) error {
	p := b.logPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// Create shells out to `bd create --json` with type/title/parent/priority
// fields derived from the Op. Skips when the (nonce, anchor) is already
// in the apply log.
func (b *BDMutator) Create(ctx context.Context, op reconcile.Op) (string, error) {
	if done, id := b.alreadyApplied(op.Anchor, "create"); done {
		return id, nil
	}
	args := []string{"create", "--json", "--type", op.Type, "--title", op.Title}
	if op.Parent != "" {
		args = append(args, "--parent", op.Parent)
	}
	if op.Priority != "" {
		args = append(args, "--priority", op.Priority)
	}
	out, err := b.run(ctx, b.bin(), args...)
	if err != nil {
		return "", fmt.Errorf("bd create: %w", err)
	}
	id, err := parseCreatedID(out)
	if err != nil {
		return "", fmt.Errorf("bd create: parse id: %w", err)
	}
	_ = b.record(applyLogEntry{Nonce: b.Nonce, Anchor: op.Anchor, Kind: "create", BeadID: id})
	return id, nil
}

// Update applies non-informational FieldDiffs via `bd update --json`.
func (b *BDMutator) Update(ctx context.Context, op reconcile.Op) error {
	if done, _ := b.alreadyApplied(op.Anchor, "update"); done {
		return nil
	}
	if op.BeadID == "" {
		return errors.New("bd update: missing bead id")
	}
	args := []string{"update", op.BeadID, "--json"}
	for k, fc := range op.FieldDiffs {
		if fc.Informational {
			continue
		}
		switch k {
		case "title":
			args = append(args, "--title", fc.To)
		case "parent":
			args = append(args, "--parent", fc.To)
		case "priority":
			args = append(args, "--priority", fc.To)
		}
	}
	if _, err := b.run(ctx, b.bin(), args...); err != nil {
		return fmt.Errorf("bd update: %w", err)
	}
	return b.record(applyLogEntry{Nonce: b.Nonce, Anchor: op.Anchor, Kind: "update", BeadID: op.BeadID})
}

// Close shells out to `bd close --json`.
func (b *BDMutator) Close(ctx context.Context, op reconcile.Op, reason string) error {
	if done, _ := b.alreadyApplied(op.Anchor, "close"); done {
		return nil
	}
	args := []string{"close", op.BeadID, "--json"}
	if reason != "" {
		args = append(args, "--reason", reason)
	}
	if _, err := b.run(ctx, b.bin(), args...); err != nil {
		return fmt.Errorf("bd close: %w", err)
	}
	return b.record(applyLogEntry{Nonce: b.Nonce, Anchor: op.Anchor, Kind: "close", BeadID: op.BeadID})
}

// Rollback best-effort reverses receipt.Created (close with rollback note)
// and re-stamps any pre-rollback Updated values. Errors are collected but
// not fatal — rollback must do as much as possible.
func (b *BDMutator) Rollback(ctx context.Context, receipt *Receipt) error {
	var firstErr error
	for _, r := range receipt.Created {
		if r.BeadID == "" {
			continue
		}
		args := []string{"close", r.BeadID, "--json", "--reason", "rollback: apply failed (nonce=" + b.Nonce + ")"}
		if _, err := b.run(ctx, b.bin(), args...); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// Updates: revert by replaying the "from" side of each FieldChange.
	for _, r := range receipt.Updated {
		if r.BeadID == "" || len(r.Reverse) == 0 {
			continue
		}
		args := []string{"update", r.BeadID, "--json"}
		for k, v := range r.Reverse {
			switch k {
			case "title", "parent", "priority":
				args = append(args, "--"+k, v)
			}
		}
		if _, err := b.run(ctx, b.bin(), args...); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// parseCreatedID extracts the bead id from `bd create --json` output. The
// shape we expect is `{"id":"gm-...","..."}` (top-level) or an `issue`
// wrapper. We try both.
func parseCreatedID(out []byte) (string, error) {
	out = []byte(strings.TrimSpace(string(out)))
	if len(out) == 0 {
		return "", errors.New("empty bd output")
	}
	var direct struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &direct); err == nil && direct.ID != "" {
		return direct.ID, nil
	}
	var wrap struct {
		Issue struct {
			ID string `json:"id"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(out, &wrap); err == nil && wrap.Issue.ID != "" {
		return wrap.Issue.ID, nil
	}
	return "", errors.New("no id field in bd output")
}
