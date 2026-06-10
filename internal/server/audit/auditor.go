// gm-o9t8.3.1.1 — minimal Auditor abstraction.
//
// Auditor narrows the surface server handlers need (append + query) so
// they can be wired against either the production hash-chained file
// Logger or a fast in-memory MemAuditor for tests. The file Logger
// satisfies the interface via the FileAuditor adaptor below — the
// adaptor exists so a future move to non-file-backed production storage
// (e.g. Dolt-backed audit) does not require touching every call site.

package audit

import (
	"context"
	"sync"
	"time"
)

// Auditor is the narrow contract server handlers depend on. Production
// wiring (cmd/gemba serve) constructs a FileAuditor wrapping a *Logger;
// tests inject MemAuditor.
type Auditor interface {
	// Append writes a new audit record. workspace/runID may be empty
	// for admin-plane events (tenant CRUD).
	Append(ctx context.Context, workspace, runID string, kind Kind, payload any) (*Record, error)
	// Query returns every record with Seq > fromSeq matching kind.
	// fromSeq=0 returns all matching. An empty kind returns all
	// records (admin-style firehose). Implementations MUST be
	// goroutine-safe with Append.
	Query(ctx context.Context, fromSeq int64, kind Kind) ([]Record, error)
}

// FileAuditor wraps the on-disk Logger to satisfy the Auditor
// interface. Append is a direct passthrough; Query reads the whole log
// and filters. The expected admin-plane query rate is low (operator
// inspection, integration tests) — a scan is fine.
type FileAuditor struct{ l *Logger }

// NewFileAuditor wraps l. Returns nil when l is nil so callers can pass
// a nil Logger to opt out of audit.
func NewFileAuditor(l *Logger) *FileAuditor {
	if l == nil {
		return nil
	}
	return &FileAuditor{l: l}
}

// Append delegates to the underlying Logger.
func (a *FileAuditor) Append(ctx context.Context, ws, runID string, kind Kind, payload any) (*Record, error) {
	return a.l.Append(ctx, ws, runID, kind, payload)
}

// Query reads all records past fromSeq and filters by kind (when set).
func (a *FileAuditor) Query(_ context.Context, fromSeq int64, kind Kind) ([]Record, error) {
	all, err := a.l.Read(fromSeq)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		return all, nil
	}
	out := all[:0]
	for _, r := range all {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out, nil
}

// MemAuditor is an in-memory Auditor for tests. It does not sign or
// chain records — Seq is a monotonic counter and Sig is left empty.
// Tests that need signature semantics should use Open(...) against a
// tempdir instead.
type MemAuditor struct {
	mu      sync.Mutex
	seq     int64
	records []Record
}

// NewMemAuditor returns an empty in-memory auditor.
func NewMemAuditor() *MemAuditor { return &MemAuditor{} }

// Append stores a record with a monotonic seq + UTC timestamp.
func (m *MemAuditor) Append(_ context.Context, ws, runID string, kind Kind, payload any) (*Record, error) {
	pBytes, err := payloadBytes(payload)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	rec := Record{
		Seq:       m.seq,
		TS:        time.Now().UTC(),
		Workspace: ws,
		RunID:     runID,
		Kind:      kind,
		Payload:   pBytes,
	}
	m.records = append(m.records, rec)
	return &rec, nil
}

// VMLifecycleAdapter bridges the firecracker supervisor's narrow
// LifecycleAuditor interface (VMEvent(ctx, name, payload)) onto an
// Auditor. The supervisor cannot import the audit package directly
// without an import cycle, so this adapter lives here and is wired by
// the server during construction (gm-o9t8.3.2.7).
type VMLifecycleAdapter struct{ a Auditor }

// NewVMLifecycleAdapter wraps a so the supervisor can call VMEvent.
// Nil a yields a nil adapter — callers should pass that nil through
// to firecracker.Options.Auditor and the supervisor will skip audit.
func NewVMLifecycleAdapter(a Auditor) *VMLifecycleAdapter {
	if a == nil {
		return nil
	}
	return &VMLifecycleAdapter{a: a}
}

// VMEvent satisfies firecracker.LifecycleAuditor. We stamp the supplied
// event name into the payload under "event", classify the record as
// KindVMLifecycle, and route wsid (when present in payload) into the
// Workspace column so log readers can filter per-workspace.
func (v *VMLifecycleAdapter) VMEvent(ctx context.Context, event string, payload map[string]any) {
	if v == nil || v.a == nil {
		return
	}
	ws, _ := payload["wsid"].(string)
	runID, _ := payload["vm_id"].(string)
	// Copy payload + stamp event so callers don't accidentally mutate
	// what they passed in.
	enriched := make(map[string]any, len(payload)+1)
	for k, val := range payload {
		enriched[k] = val
	}
	enriched["event"] = event
	_, _ = v.a.Append(ctx, ws, runID, KindVMLifecycle, enriched)
}

// Query returns a copy of every record with Seq > fromSeq that matches
// kind (empty kind matches all).
func (m *MemAuditor) Query(_ context.Context, fromSeq int64, kind Kind) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.records))
	for _, r := range m.records {
		if r.Seq <= fromSeq {
			continue
		}
		if kind != "" && r.Kind != kind {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
