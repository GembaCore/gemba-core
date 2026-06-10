// gm-o9t8.4.2.1 — read-only projection of per-tenant resource usage.
//
// Counters is the narrow contract the tier-aware middleware uses to
// decide whether a tenant has already exhausted a concurrent-resource
// ceiling (VMs, workspaces, monthly minutes). Implementations project
// the underlying tenant store / billing ledger; the middleware itself
// stays read-only and side-effect free.
//
// MemCounters is the in-memory implementation used by tests and by
// single-instance dev servers that have not yet wired the billing
// projection.
package quota

import "sync"

// Counters is the projection contract. All methods return 0 for a
// tenant not yet tracked — the middleware then treats the tenant as
// "no usage so far" and admits the request (subject to the bucket).
type Counters interface {
	// ConcurrentVMs reports how many sandbox VMs are currently running
	// for tid. Used to gate VM-creation routes against the tier's
	// MaxConcurrentVMs ceiling.
	ConcurrentVMs(tid string) int
	// MonthlyVMMinutes reports the total VM-minutes billed for tid in
	// the current calendar month. Used to gate VM-creation routes
	// against the tier's MonthlyVMMinutes soft cap.
	MonthlyVMMinutes(tid string) int
	// Workspaces reports the current live workspace count for tid.
	// Used to gate workspace-creation routes against MaxWorkspaces.
	Workspaces(tid string) int
}

// MemCounters is an in-memory Counters implementation. The maps are
// keyed by tenant id (string) and protected by a single RWMutex —
// adequate for tests and for the slice-a single-instance scope.
type MemCounters struct {
	mu       sync.RWMutex
	vms      map[string]int
	minutes  map[string]int
	wkspaces map[string]int
}

// NewMemCounters returns an empty in-memory counter projection.
func NewMemCounters() *MemCounters {
	return &MemCounters{
		vms:      map[string]int{},
		minutes:  map[string]int{},
		wkspaces: map[string]int{},
	}
}

// SetConcurrentVMs overrides the VM count for tid. Test-only.
func (m *MemCounters) SetConcurrentVMs(tid string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vms[tid] = n
}

// SetMonthlyVMMinutes overrides the monthly-minutes count for tid.
// Test-only.
func (m *MemCounters) SetMonthlyVMMinutes(tid string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.minutes[tid] = n
}

// SetWorkspaces overrides the workspace count for tid. Test-only.
func (m *MemCounters) SetWorkspaces(tid string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wkspaces[tid] = n
}

// ConcurrentVMs implements Counters.
func (m *MemCounters) ConcurrentVMs(tid string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.vms[tid]
}

// MonthlyVMMinutes implements Counters.
func (m *MemCounters) MonthlyVMMinutes(tid string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.minutes[tid]
}

// Workspaces implements Counters.
func (m *MemCounters) Workspaces(tid string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.wkspaces[tid]
}
