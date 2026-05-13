package billing

import (
	"testing"
	"time"
)

func TestAggregator_VMMinutesPair(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := NewAggregator()
	a.Observe(Event{TenantID: "t-aaa", Kind: "vm.spawn", VMID: "vm1", TS: base})
	a.Observe(Event{TenantID: "t-aaa", Kind: "vm.destroy", VMID: "vm1", TS: base.Add(90 * time.Second)})
	got := a.Snapshot("t-aaa")
	if got.VMMinutes < 1.49 || got.VMMinutes > 1.51 {
		t.Fatalf("want ~1.5, got %v", got.VMMinutes)
	}
}

func TestAggregator_VMDestroyWithoutSpawnNoop(t *testing.T) {
	a := NewAggregator()
	a.Observe(Event{TenantID: "t-aaa", Kind: "vm.destroy", VMID: "vm1", TS: time.Now()})
	if got := a.Snapshot("t-aaa").VMMinutes; got != 0 {
		t.Fatalf("want 0, got %v", got)
	}
}

func TestAggregator_EgressBytesAccum(t *testing.T) {
	a := NewAggregator()
	a.Observe(Event{TenantID: "t-aaa", Kind: "vm.egress.bytes", Bytes: 100})
	a.Observe(Event{TenantID: "t-aaa", Kind: "vm.egress.bytes", Bytes: 250})
	if got := a.Snapshot("t-aaa").EgressBytes; got != 350 {
		t.Fatalf("want 350, got %v", got)
	}
}

func TestAggregator_AuditLogGB(t *testing.T) {
	a := NewAggregator()
	// 2 GiB worth of bytes
	a.Observe(Event{TenantID: "t-aaa", Kind: "audit.log.bytes", Bytes: float64(2) * (1 << 30)})
	if got := a.Snapshot("t-aaa").AuditLogGB; got != 2 {
		t.Fatalf("want 2 GB, got %v", got)
	}
}

func TestAggregator_TenantIsolation(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := NewAggregator()
	a.Observe(Event{TenantID: "t-aaa", Kind: "vm.spawn", VMID: "vm1", TS: base})
	a.Observe(Event{TenantID: "t-aaa", Kind: "vm.destroy", VMID: "vm1", TS: base.Add(60 * time.Second)})
	a.Observe(Event{TenantID: "t-bbb", Kind: "vm.egress.bytes", Bytes: 42})
	if got := a.Snapshot("t-aaa"); got.VMMinutes != 1 || got.EgressBytes != 0 {
		t.Fatalf("t-aaa unexpected: %+v", got)
	}
	if got := a.Snapshot("t-bbb"); got.EgressBytes != 42 || got.VMMinutes != 0 {
		t.Fatalf("t-bbb unexpected: %+v", got)
	}
}

func TestAggregator_UnknownEventNoop(t *testing.T) {
	a := NewAggregator()
	a.Observe(Event{TenantID: "t-aaa", Kind: "garbage"})
	if got := a.Snapshot("t-aaa"); got != (UsageCounters{}) {
		t.Fatalf("want zero, got %+v", got)
	}
}

func TestAggregator_DropsMissingTenant(t *testing.T) {
	a := NewAggregator()
	a.Observe(Event{Kind: "vm.egress.bytes", Bytes: 100})
	// no tenant — should accumulate nothing
	if got := a.Snapshot(""); got.EgressBytes != 0 {
		t.Fatalf("want 0, got %v", got.EgressBytes)
	}
}

func TestAggregator_NegativeBytesIgnored(t *testing.T) {
	a := NewAggregator()
	a.Observe(Event{TenantID: "t-aaa", Kind: "vm.egress.bytes", Bytes: -1})
	if got := a.Snapshot("t-aaa").EgressBytes; got != 0 {
		t.Fatalf("want 0, got %v", got)
	}
}
