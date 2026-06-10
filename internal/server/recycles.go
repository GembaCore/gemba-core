// session_recycles writer (gm-s47n.12, spec §10.3).
//
// Subscribes to the OrchestrationPlane's session.recycled events and
// persists one row per recycle into the session_recycles dolt table.
// The retro pipeline (gm-s47n.8) joins these rows against
// dispatch_decisions on (prior_session_id, new_session_id) to grade
// recycle timing.
//
// Lifecycle:
//   - cmd/gemba serve calls StartRecycleWriter once at startup with
//     the bound OrchestrationPlane and a *sql.DB. Without a DB the
//     writer becomes a no-op (the events still flow to the SSE hub).
//   - The writer subscribes with Kinds=[]string{"session.recycled"},
//     loops on the channel, and inserts one row per event.
//   - ctx cancellation tears the writer down; the underlying
//     Subscribe channel closes when the OrchestrationPlane shuts down.

package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// RecycleWriter persists session.recycled events. Constructed via
// NewRecycleWriter; Run loops until ctx is cancelled.
type RecycleWriter struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewRecycleWriter wraps the given *sql.DB. nil db is allowed —
// returns a writer whose Run is a pure no-op.
func NewRecycleWriter(db *sql.DB) *RecycleWriter {
	return &RecycleWriter{db: db, logger: slog.Default()}
}

// StartRecycleWriter is the convenience constructor + launcher used
// by cmd/gemba serve. Subscribes the writer to the OrchestrationPlane
// (filtered to session.recycled events only) and runs the loop on a
// new goroutine. ctx cancellation terminates the goroutine.
//
// Best-effort: a Subscribe failure is logged but doesn't propagate —
// the writer becomes a no-op and gemba serve continues. The retro
// pipeline simply won't have recycle audit data until the next
// restart.
func StartRecycleWriter(ctx context.Context, op core.OrchestrationPlaneAdaptor, db *sql.DB) *RecycleWriter {
	rw := NewRecycleWriter(db)
	if op == nil || db == nil {
		return rw
	}
	ch, err := op.Subscribe(ctx, core.SubscribeFilter{
		Kinds: []string{"session.recycled"},
	})
	if err != nil {
		var aerr *core.AdaptorError
		if errors.As(err, &aerr) && aerr.Kind == core.KindUnsupported {
			// Adaptor doesn't support Subscribe — degrade silently.
			return rw
		}
		slog.Warn("session_recycles: Subscribe failed; writer disabled",
			"err", err)
		return rw
	}
	go rw.Run(ctx, ch)
	return rw
}

// Run reads events from in until ctx is cancelled or in is closed.
// Each session.recycled event is persisted via Insert; non-fatal
// errors are logged and the loop continues.
func (w *RecycleWriter) Run(ctx context.Context, in <-chan core.OrchestrationEvent) {
	if w == nil || w.db == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-in:
			if !ok {
				return
			}
			if ev.Kind != "session.recycled" {
				continue
			}
			if err := w.Insert(ctx, ev); err != nil {
				w.logger.Warn("session_recycles: insert failed",
					"err", err,
					"event_id", ev.ID)
			}
		}
	}
}

// Insert persists one session.recycled event. Exposed so tests can
// drive the writer without spinning up a Subscribe channel. Returns
// nil on success or a wrapped error on insert failure.
func (w *RecycleWriter) Insert(ctx context.Context, ev core.OrchestrationEvent) error {
	if w == nil || w.db == nil {
		return errors.New("recycle writer: nil db")
	}
	row := recycleRowFromEvent(ev)
	if row == nil {
		return errors.New("recycle writer: malformed event payload")
	}
	const stmt = `
		INSERT INTO session_recycles (
			id, pool_key, pane_id, prior_session_id, new_session_id,
			reason, prior_profile_json, recycled_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := w.db.ExecContext(ctx, stmt,
		row.ID, row.PoolKey, row.PaneID,
		row.PriorSessionID, row.NewSessionID,
		row.Reason, row.PriorProfileJSON,
		row.RecycledAt,
	)
	return err
}

type recycleRow struct {
	ID               string
	PoolKey          string
	PaneID           string
	PriorSessionID   string
	NewSessionID     string
	Reason           string
	PriorProfileJSON string
	RecycledAt       time.Time
}

// recycleRowFromEvent extracts the row payload from a
// session.recycled OrchestrationEvent. Returns nil when the
// payload is missing required fields — the caller logs and drops.
//
// Expected payload shape (matches native/recycle.go):
//
//	prior_session_id  string
//	new_session_id    string
//	pane_id           string
//	persona_id        string
//	agent_type        string
//	reason            string
//	prior_profile     map[string]any (optional; JSON-encoded for storage)
func recycleRowFromEvent(ev core.OrchestrationEvent) *recycleRow {
	priorID, _ := ev.Payload["prior_session_id"].(string)
	newID, _ := ev.Payload["new_session_id"].(string)
	paneID, _ := ev.Payload["pane_id"].(string)
	personaID, _ := ev.Payload["persona_id"].(string)
	reason, _ := ev.Payload["reason"].(string)
	if priorID == "" || newID == "" {
		return nil
	}
	if reason == "" {
		reason = "explicit"
	}
	rig, _ := ev.Payload["rig"].(string)
	poolKey := personaID
	if rig != "" {
		poolKey = rig + ":" + personaID
	}
	at := ev.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	id, err := newRecycleID()
	if err != nil {
		// crypto/rand failure — degrade to a deterministic id so
		// the row still inserts. The PK collision risk is
		// negligible (one row per recycle, dolt sees ~handful per
		// day per pool).
		id = "recycle-" + at.Format("20060102T150405.000000000")
	}
	row := &recycleRow{
		ID:             id,
		PoolKey:        poolKey,
		PaneID:         paneID,
		PriorSessionID: priorID,
		NewSessionID:   newID,
		Reason:         reason,
		RecycledAt:     at,
	}
	if profile, ok := ev.Payload["prior_profile"].(map[string]any); ok && len(profile) > 0 {
		if bs, err := json.Marshal(profile); err == nil {
			row.PriorProfileJSON = string(bs)
		}
	}
	return row
}

func newRecycleID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
