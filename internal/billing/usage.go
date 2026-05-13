// Package billing aggregates per-tenant resource usage from the audit
// event stream so operators can surface metering data on the
// productization surface (gm-o9t8.4.2 Workstream B slice b).
//
// The MVP keeps three coarse counters:
//
//   - VMMinutes — wall-clock minutes a Firecracker VM was running,
//     accumulated from EventVMSpawn / EventVMDestroy pairs.
//   - EgressBytes — total bytes the egress applier observed for the
//     tenant; emitted by the egress runtime (event "vm.egress.bytes").
//   - AuditLogGB — gigabytes of audit log written for the tenant,
//     emitted by the audit subsystem (event "audit.log.bytes").
//
// The aggregator is intentionally decoupled from the audit package via
// a small Event struct; that way the audit layer can fire-and-forget a
// subscriber callback without dragging the billing package into its
// import graph.
package billing

import (
	"sync"
	"time"
)

// UsageCounters is the per-tenant snapshot the GET /usage endpoint
// returns. The values are cumulative since process start (or since the
// last persisted checkpoint, once persistence lands).
type UsageCounters struct {
	VMMinutes   float64 `json:"vm_minutes"`
	EgressBytes float64 `json:"egress_bytes"`
	AuditLogGB  float64 `json:"audit_log_gb"`
}

// Event is the narrow shape the aggregator subscribes to. The audit
// pipeline (or any other producer) constructs one of these and calls
// Aggregator.Observe; the aggregator owns the bookkeeping. Decoupling
// the wire shape from audit.Record means we don't pull every audit
// consumer into the billing import graph.
type Event struct {
	// TenantID identifies the tenant the event belongs to. Events
	// without a tenant id (admin-plane records, system events) are
	// dropped by Observe.
	TenantID string

	// Kind is the canonical event name, mirroring audit.Event.
	// Recognised values:
	//   - "vm.spawn"           — VMID present in payload; starts a timer.
	//   - "vm.destroy"         — VMID present; closes the timer and
	//                            accumulates the elapsed minutes.
	//   - "vm.egress.bytes"    — Bytes in payload; accumulates EgressBytes.
	//   - "audit.log.bytes"    — Bytes in payload; accumulates AuditLogGB.
	Kind string

	// VMID is the firecracker VM identifier for spawn/destroy events.
	// Empty for non-VM events.
	VMID string

	// Bytes carries the byte count for egress / audit byte events.
	Bytes float64

	// TS is the event timestamp; zero falls back to time.Now() so
	// callers can omit it.
	TS time.Time
}

// Aggregator is the per-tenant usage accumulator. Construct with
// NewAggregator. Observe is goroutine-safe.
type Aggregator struct {
	mu       sync.Mutex
	tenants  map[string]*tenantState
	vmStarts map[string]vmStart // key: tenantID|vmID
	now      func() time.Time
}

// vmStart tracks an in-flight VM lifecycle (between spawn and destroy).
type vmStart struct {
	tenantID string
	ts       time.Time
}

type tenantState struct {
	vmMinutes   float64
	egressBytes float64
	auditBytes  float64
}

// NewAggregator returns an empty aggregator with time.Now as its clock.
func NewAggregator() *Aggregator {
	return &Aggregator{
		tenants:  map[string]*tenantState{},
		vmStarts: map[string]vmStart{},
		now:      time.Now,
	}
}

// WithClock returns a copy of a using the supplied now function. Tests
// inject a deterministic clock so vm.spawn → vm.destroy intervals are
// predictable.
func (a *Aggregator) WithClock(now func() time.Time) *Aggregator {
	a.now = now
	return a
}

// Observe records one event. Unknown Kind or missing TenantID is a
// no-op; the aggregator never errors so audit producers can wire a
// "fire and forget" subscription without error-handling boilerplate.
func (a *Aggregator) Observe(ev Event) {
	if ev.TenantID == "" {
		return
	}
	if ev.TS.IsZero() {
		ev.TS = a.now()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.tenants[ev.TenantID]
	if !ok {
		st = &tenantState{}
		a.tenants[ev.TenantID] = st
	}
	switch ev.Kind {
	case "vm.spawn":
		if ev.VMID == "" {
			return
		}
		a.vmStarts[ev.TenantID+"|"+ev.VMID] = vmStart{tenantID: ev.TenantID, ts: ev.TS}
	case "vm.destroy":
		if ev.VMID == "" {
			return
		}
		key := ev.TenantID + "|" + ev.VMID
		s, ok := a.vmStarts[key]
		if !ok {
			return
		}
		delete(a.vmStarts, key)
		minutes := ev.TS.Sub(s.ts).Minutes()
		if minutes < 0 {
			minutes = 0
		}
		st.vmMinutes += minutes
	case "vm.egress.bytes":
		if ev.Bytes < 0 {
			return
		}
		st.egressBytes += ev.Bytes
	case "audit.log.bytes":
		if ev.Bytes < 0 {
			return
		}
		st.auditBytes += ev.Bytes
	}
}

// Snapshot returns the current counters for tenantID. Unknown tenants
// return the zero value (no allocation).
func (a *Aggregator) Snapshot(tenantID string) UsageCounters {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.tenants[tenantID]
	if !ok {
		return UsageCounters{}
	}
	return UsageCounters{
		VMMinutes:   st.vmMinutes,
		EgressBytes: st.egressBytes,
		AuditLogGB:  st.auditBytes / (1 << 30),
	}
}

// Subscriber is the small contract any source of UsageCounters input
// (audit pipeline, egress monitor) implements to feed the aggregator.
// The audit package wires its emit hook against an Aggregator.Observe
// via a thin adaptor so the billing package never imports audit.
type Subscriber interface {
	Observe(ev Event)
}

// Ensure Aggregator satisfies Subscriber at compile time.
var _ Subscriber = (*Aggregator)(nil)
