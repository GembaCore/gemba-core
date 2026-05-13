package reconcile

import (
	"bytes"
	"encoding/json"
	"sort"
)

// SchemaVersion is emitted as the `$schema` field of every marshaled Plan.
// Increment when the JSON shape changes in a non-backward-compatible way.
const SchemaVersion = "gemba.reconcile.v1"

// MarshalJSON produces a stable, anchor-sorted JSON encoding suitable for
// UI consumers and golden-file tests.
//
// Determinism guarantees:
//   - Op slices are sorted by Anchor (Diff already does this; we re-sort
//     defensively in case a caller mutates the slices before encoding).
//   - Map keys (FieldDiffs) are emitted in sorted order by encoding/json.
//   - A `$schema` key is included so consumers can detect format drift.
func (p *Plan) MarshalJSON() ([]byte, error) {
	creates := append([]Op(nil), p.Creates...)
	updates := append([]Op(nil), p.Updates...)
	orphans := append([]Op(nil), p.Orphans...)
	sort.SliceStable(creates, func(i, j int) bool { return creates[i].Anchor < creates[j].Anchor })
	sort.SliceStable(updates, func(i, j int) bool { return updates[i].Anchor < updates[j].Anchor })
	sort.SliceStable(orphans, func(i, j int) bool { return orphans[i].Anchor < orphans[j].Anchor })

	// emitOp is a struct alias of Op that JSON-encodes identically (since
	// it has the same fields and tags) without triggering Plan.MarshalJSON
	// recursion. We use a fresh struct so we can prepend $schema cleanly.
	type emitOp struct {
		Kind        OpKind                 `json:"kind"`
		Anchor      string                 `json:"anchor"`
		BeadID      string                 `json:"bead_id,omitempty"`
		Title       string                 `json:"title"`
		Type        string                 `json:"type"`
		Parent      string                 `json:"parent,omitempty"`
		Priority    string                 `json:"priority,omitempty"`
		ContentHash string                 `json:"content_hash,omitempty"`
		FieldDiffs  map[string]FieldChange `json:"field_diffs,omitempty"`
		Reason      string                 `json:"reason"`
		NonceStale  bool                   `json:"nonce_stale,omitempty"`
		Rationale   string                 `json:"rationale,omitempty"`
	}
	convert := func(ops []Op) []emitOp {
		out := make([]emitOp, len(ops))
		for i, op := range ops {
			out[i] = emitOp(op)
		}
		return out
	}

	payload := struct {
		Schema     string   `json:"$schema"`
		SpecID     string   `json:"spec_id"`
		Nonce      string   `json:"nonce"`
		LockNonce  string   `json:"lock_nonce"`
		NonceStale bool     `json:"nonce_stale"`
		Creates    []emitOp `json:"creates"`
		Updates    []emitOp `json:"updates"`
		Orphans    []emitOp `json:"orphans"`
	}{
		Schema:     SchemaVersion,
		SpecID:     p.SpecID,
		Nonce:      p.Nonce,
		LockNonce:  p.LockNonce,
		NonceStale: p.NonceStale,
		Creates:    convert(creates),
		Updates:    convert(updates),
		Orphans:    convert(orphans),
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}
