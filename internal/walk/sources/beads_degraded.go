// gm-vch2: BeadsDegradedLister translates the adaptor HealthBus
// degraded-state surface into synthetic core.EscalationRequest rows
// so the gemba walk agenda surfaces "an adaptor I depend on is
// unhealthy" alongside other escalation flavours.
//
// Stable id contract: the EscalationRequest.ID is deterministic per
// degraded span. The bus reports {Name, Plane, Healthy, Reason}; we
// hash (Name, Plane, Reason) into the id so a still-degraded probe
// produces the SAME id on every BuildAgenda call (so the agenda's
// dedupe path keeps a single row even across multiple refreshes).
// When the reason changes (e.g. dolt: connect-timeout → bd: query-
// failed) the id rolls — the operator sees a fresh row, which is
// the right signal: a new flavour of degradation needs a new look.
//
// Workspace: this lister is workspace-agnostic — adaptor health is
// process-wide. The workspace argument from the walk is ignored;
// the design's per-workspace filter (gm-vch2 follow-up) would only
// apply once adaptors model per-workspace probes, which they don't
// today.

package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	"github.com/MikeBengtson/gemba/internal/core"
)

// HealthSnapshotter is the minimal HealthBus surface the lister
// reads. Defined as an interface so tests don't have to spin up
// the full ticker-driven bus.
type HealthSnapshotter interface {
	Snapshot() []registry.AdaptorStatus
}

// BeadsDegradedLister synthesizes escalations from any adaptor
// reporting Healthy=false in the most recent HealthBus snapshot.
// "Beads" in the name is historical (gm-b1's first instance was the
// bd Dolt daemon) — the lister surfaces every degraded adaptor on
// every plane.
//
// Construct via NewBeadsDegradedLister; nil-safe in the typed-nil
// receiver style so a Sources factory that wires nil contributes
// zero items without panicking.
type BeadsDegradedLister struct {
	bus HealthSnapshotter
	now func() time.Time
}

// NewBeadsDegradedLister wires the lister against a HealthBus.
// Returns nil when bus is nil — callers can pass the result
// directly through walk.Sources.Escalations composition.
func NewBeadsDegradedLister(bus HealthSnapshotter) *BeadsDegradedLister {
	if bus == nil {
		return nil
	}
	return &BeadsDegradedLister{bus: bus, now: time.Now}
}

// ListOpenEscalations satisfies walk.EscalationLister. The workspace
// argument is intentionally ignored — adaptor health is process-wide.
func (l *BeadsDegradedLister) ListOpenEscalations(_ context.Context, _ string) ([]core.EscalationRequest, error) {
	if l == nil || l.bus == nil {
		return nil, nil
	}
	now := l.now()
	snap := l.bus.Snapshot()
	out := make([]core.EscalationRequest, 0, len(snap))
	for _, s := range snap {
		if s.Healthy {
			continue
		}
		title := "Adaptor degraded: " + s.Name
		prompt := s.Reason
		if prompt == "" {
			prompt = "adaptor reported degraded health (no reason provided)"
		}
		out = append(out, core.EscalationRequest{
			ID:        beadsDegradedID(s),
			Source:    core.EscalationBeadsDegraded,
			Urgency:   core.UrgencyAdvisory,
			Title:     title,
			Prompt:    prompt,
			State:     core.EscalationOpen,
			CreatedAt: now,
		})
	}
	return out, nil
}

// beadsDegradedID returns a stable hex id for a degraded adaptor
// span. Hashing (name, plane, reason) keeps the id constant across
// repeated probes that observe the same flavour of degradation.
func beadsDegradedID(s registry.AdaptorStatus) string {
	h := sha256.New()
	h.Write([]byte("beads_degraded|"))
	h.Write([]byte(s.Name))
	h.Write([]byte("|"))
	h.Write([]byte(string(s.Plane)))
	h.Write([]byte("|"))
	h.Write([]byte(s.Reason))
	return "beads-deg-" + hex.EncodeToString(h.Sum(nil)[:8])
}
