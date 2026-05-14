package quota

import (
	"errors"
	"testing"
	"time"
)

func TestLimitsForTier_Table(t *testing.T) {
	cases := []struct {
		name      string
		tier      Tier
		wantRPS   int
		wantVMs   int
		wantWS    int
		wantMins  int
		wantBurst int
	}{
		{"free defaults", TierFree, 2, 1, 1, 60, 5},
		{"solo", TierSolo, 10, 3, 5, 600, 30},
		{"teams", TierTeams, 50, 10, 25, 3000, 100},
		{"enterprise", TierEnterprise, 500, 100, 200, 30000, 1000},
		{"unknown falls back to free", Tier("garbage"), 2, 1, 1, 60, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LimitsForTier(tc.tier)
			if got.RPS != tc.wantRPS {
				t.Errorf("RPS = %d, want %d", got.RPS, tc.wantRPS)
			}
			if got.Burst != tc.wantBurst {
				t.Errorf("Burst = %d, want %d", got.Burst, tc.wantBurst)
			}
			if got.MaxConcurrentVMs != tc.wantVMs {
				t.Errorf("MaxConcurrentVMs = %d, want %d", got.MaxConcurrentVMs, tc.wantVMs)
			}
			if got.MaxWorkspaces != tc.wantWS {
				t.Errorf("MaxWorkspaces = %d, want %d", got.MaxWorkspaces, tc.wantWS)
			}
			if got.MonthlyVMMinutes != tc.wantMins {
				t.Errorf("MonthlyVMMinutes = %d, want %d", got.MonthlyVMMinutes, tc.wantMins)
			}
		})
	}
}

func TestParseTier(t *testing.T) {
	cases := []struct {
		in      string
		want    Tier
		wantErr bool
	}{
		{"free", TierFree, false},
		{"FREE", TierFree, false},
		{" solo ", TierSolo, false},
		{"Teams", TierTeams, false},
		{"enterprise", TierEnterprise, false},
		{"", TierFree, false},
		{"platinum", "", true},
	}
	for _, tc := range cases {
		got, err := ParseTier(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseTier(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("ParseTier(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if tc.wantErr && !errors.Is(err, ErrUnknownTier) {
			t.Errorf("ParseTier(%q) err = %v, want ErrUnknownTier", tc.in, err)
		}
	}
}

func TestBucket_BurstAndRefill(t *testing.T) {
	// Manual-clock bucket: rps=2, burst=3. Burst should admit 3 calls
	// immediately, then deny until a token refills at +500ms.
	now := time.Unix(0, 0)
	b := NewBucket(2, 3)
	b.SetClock(func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("call %d: expected allow during burst", i)
		}
	}
	if b.Allow() {
		t.Fatal("4th call should deny (burst exhausted)")
	}
	// Advance the clock by 500ms — exactly one token's worth at rps=2.
	now = now.Add(500 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("expected allow after 500ms refill")
	}
	if b.Allow() {
		t.Fatal("immediately after refill should deny")
	}
}

func TestBucket_RetryAfterRoundsUp(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBucket(1, 1)
	b.SetClock(func() time.Time { return now })
	if !b.Allow() {
		t.Fatal("first call should allow")
	}
	if b.Allow() {
		t.Fatal("second immediate call should deny")
	}
	d := b.RetryAfter()
	if d < time.Second {
		t.Errorf("RetryAfter = %v, want >= 1s floor", d)
	}
}

func TestBucket_UnlimitedWhenRPSZero(t *testing.T) {
	b := NewBucket(0, 0)
	for i := 0; i < 100; i++ {
		if !b.Allow() {
			t.Fatalf("call %d: zero-config bucket should always allow", i)
		}
	}
}

func TestBucket_Reset(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBucket(1, 2)
	b.SetClock(func() time.Time { return now })
	_ = b.Allow()
	_ = b.Allow()
	if b.Allow() {
		t.Fatal("burst should be exhausted")
	}
	b.Reset()
	if !b.Allow() || !b.Allow() {
		t.Fatal("Reset should refill the bucket to burst")
	}
}

func TestBucketStore_PerTenantIsolation(t *testing.T) {
	s := NewBucketStore()
	a := s.Get("t-aaaa", 1, 1)
	bk := s.Get("t-bbbb", 1, 1)
	if !a.Allow() {
		t.Fatal("a first call allow")
	}
	if a.Allow() {
		t.Fatal("a second call deny")
	}
	// b's bucket is independent.
	if !bk.Allow() {
		t.Fatal("b first call allow")
	}
	// Get returns the same instance for the same id.
	if got := s.Get("t-aaaa", 1, 1); got != a {
		t.Errorf("Get should return cached bucket")
	}
}

func TestBucketStore_Replace(t *testing.T) {
	s := NewBucketStore()
	a := s.Get("t-aaaa", 1, 1)
	_ = a.Allow()
	if a.Allow() {
		t.Fatal("burst exhausted")
	}
	a2 := s.Replace("t-aaaa", 1, 1)
	if a == a2 {
		t.Errorf("Replace should install a fresh bucket")
	}
	if !a2.Allow() {
		t.Fatal("fresh bucket should allow first call")
	}
}

func TestMemCounters_Projection(t *testing.T) {
	c := NewMemCounters()
	if got := c.ConcurrentVMs("t-x"); got != 0 {
		t.Errorf("default ConcurrentVMs = %d, want 0", got)
	}
	c.SetConcurrentVMs("t-x", 3)
	c.SetMonthlyVMMinutes("t-x", 120)
	c.SetWorkspaces("t-x", 7)
	if got := c.ConcurrentVMs("t-x"); got != 3 {
		t.Errorf("ConcurrentVMs = %d, want 3", got)
	}
	if got := c.MonthlyVMMinutes("t-x"); got != 120 {
		t.Errorf("MonthlyVMMinutes = %d, want 120", got)
	}
	if got := c.Workspaces("t-x"); got != 7 {
		t.Errorf("Workspaces = %d, want 7", got)
	}
}
