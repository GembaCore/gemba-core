package planner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDispatchGate_DisabledRejectsEverything(t *testing.T) {
	g := NewDispatchGate(DispatchPolicy{}) // Enabled=false default
	ok, reason := g.AllowDispatch("sess-1", time.Now())
	if ok {
		t.Fatal("disabled gate must reject")
	}
	if reason != "auto-dispatch disabled" {
		t.Errorf("reason = %q", reason)
	}
}

func TestDispatchGate_EnabledAllowsFirstDispatch(t *testing.T) {
	g := NewDispatchGate(DispatchPolicy{Enabled: true})
	ok, reason := g.AllowDispatch("sess-1", time.Now())
	if !ok {
		t.Fatalf("first dispatch should be allowed; reason=%q", reason)
	}
}

func TestDispatchGate_RateLimitsRepeatWithinWindow(t *testing.T) {
	g := NewDispatchGate(DispatchPolicy{
		Enabled:               true,
		MinIntervalPerSession: 5 * time.Minute,
	})
	t0 := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)

	if ok, _ := g.AllowDispatch("sess-1", t0); !ok {
		t.Fatal("first dispatch blocked")
	}
	g.RecordDispatch("sess-1", t0)

	// 1 minute later — still inside the 5-minute window.
	t1 := t0.Add(time.Minute)
	if ok, reason := g.AllowDispatch("sess-1", t1); ok {
		t.Errorf("expected block within rate-limit window; got allow")
	} else if reason != "per-session rate limit" {
		t.Errorf("reason = %q", reason)
	}

	// 6 minutes later — past the window.
	t2 := t0.Add(6 * time.Minute)
	g.RecordCompletion("sess-1") // free the inflight slot
	if ok, reason := g.AllowDispatch("sess-1", t2); !ok {
		t.Errorf("expected allow past window; reason=%q", reason)
	}
}

func TestDispatchGate_PerSessionRateLimitDoesNotBlockOtherSessions(t *testing.T) {
	g := NewDispatchGate(DispatchPolicy{
		Enabled:               true,
		MinIntervalPerSession: 5 * time.Minute,
		MaxConcurrent:         8,
	})
	t0 := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	g.RecordDispatch("sess-1", t0)
	if ok, _ := g.AllowDispatch("sess-2", t0); !ok {
		t.Errorf("second session blocked by first session's rate limit")
	}
}

func TestDispatchGate_MaxConcurrentBlocksWhenSaturated(t *testing.T) {
	g := NewDispatchGate(DispatchPolicy{
		Enabled:               true,
		MinIntervalPerSession: time.Second,
		MaxConcurrent:         2,
	})
	now := time.Now()
	g.RecordDispatch("a", now)
	g.RecordDispatch("b", now)
	if ok, reason := g.AllowDispatch("c", now.Add(time.Hour)); ok {
		t.Errorf("expected block when inflight==max")
	} else if reason != "max-concurrent reached" {
		t.Errorf("reason = %q", reason)
	}

	// Free a slot — c can now dispatch.
	g.RecordCompletion("a")
	if ok, _ := g.AllowDispatch("c", now.Add(time.Hour)); !ok {
		t.Error("expected allow after completion frees slot")
	}
}

func TestDispatchGate_RecordCompletionClampsAtZero(t *testing.T) {
	g := NewDispatchGate(DispatchPolicy{Enabled: true})
	g.RecordCompletion("never-dispatched")
	g.RecordCompletion("never-dispatched")
	if got := g.Inflight(); got != 0 {
		t.Errorf("inflight underflowed: got %d", got)
	}
}

func TestDispatchGate_EmptySessionIDRejected(t *testing.T) {
	g := NewDispatchGate(DispatchPolicy{Enabled: true})
	ok, reason := g.AllowDispatch("", time.Now())
	if ok || reason != "empty session id" {
		t.Errorf("empty id: ok=%v reason=%q", ok, reason)
	}
}

func TestDispatchGate_SetEnabledFlipsAtRuntime(t *testing.T) {
	g := NewDispatchGate(DispatchPolicy{Enabled: true})
	if ok, _ := g.AllowDispatch("a", time.Now()); !ok {
		t.Fatal("expected allow when enabled")
	}
	g.SetEnabled(false)
	if ok, _ := g.AllowDispatch("a", time.Now()); ok {
		t.Error("kill switch did not engage")
	}
	g.SetEnabled(true)
	if ok, _ := g.AllowDispatch("a", time.Now()); !ok {
		t.Error("re-enable did not stick")
	}
}

func TestDispatchGate_PolicyReturnsCopy(t *testing.T) {
	g := NewDispatchGate(DispatchPolicy{Enabled: true, MaxConcurrent: 2})
	p := g.Policy()
	p.Enabled = false
	if !g.Policy().Enabled {
		t.Error("Policy() returned a reference; mutation leaked")
	}
}

func TestDispatchGate_DefaultsAppliedAtCheckTime(t *testing.T) {
	// Pass an all-zero policy with Enabled=true — the rate limit
	// SHOULD use DefaultMinIntervalPerSession (5min), not zero.
	g := NewDispatchGate(DispatchPolicy{Enabled: true})
	t0 := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	g.RecordDispatch("a", t0)
	t1 := t0.Add(2 * time.Minute) // 2min < 5min default
	if ok, reason := g.AllowDispatch("a", t1); ok {
		t.Errorf("expected default rate-limit to fire")
	} else if reason != "per-session rate limit" {
		t.Errorf("reason = %q", reason)
	}
}

func TestLoadPolicyFile_MissingPathReturnsZeroValueNoError(t *testing.T) {
	p, err := LoadPolicyFile(filepath.Join(t.TempDir(), "no-such-file.json"))
	if err != nil {
		t.Errorf("missing file should not error; got %v", err)
	}
	if p.Enabled {
		t.Errorf("default policy should be Enabled=false")
	}
}

func TestLoadPolicyFile_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "policy.json")
	want := DispatchPolicy{
		Enabled:               true,
		MinIntervalPerSession: 90 * time.Second,
		MaxConcurrent:         3,
	}
	if err := SavePolicyFile(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadPolicyFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != want {
		t.Errorf("round-trip lost data: got %+v want %+v", got, want)
	}
}

func TestSavePolicyFile_EmptyPathRejected(t *testing.T) {
	if err := SavePolicyFile("", DispatchPolicy{}); err == nil {
		t.Fatal("expected error on empty path")
	}
}

func TestLoadPolicyFile_MalformedJSONErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadPolicyFile(path); err == nil {
		t.Error("expected decode error on malformed JSON")
	}
}
