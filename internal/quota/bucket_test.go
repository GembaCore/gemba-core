package quota

import (
	"sync"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/tenant"
)

func TestLimiter_AllowConsumesBurst(t *testing.T) {
	l := NewLimiter()
	l.Set("t-aaaaaaaa", Config{Rate: 1, Burst: 3})
	tid := tenant.ID("t-aaaaaaaa")
	for i := 0; i < 3; i++ {
		ok, _ := l.Allow(tid)
		if !ok {
			t.Fatalf("expected allow on call %d", i)
		}
	}
	ok, retry := l.Allow(tid)
	if ok {
		t.Fatalf("expected deny once burst exhausted")
	}
	if retry < time.Second {
		t.Fatalf("expected retry-after >= 1s, got %v", retry)
	}
}

func TestLimiter_Refill(t *testing.T) {
	l := NewLimiter()
	tid := tenant.ID("t-bbbbbbbb")
	l.Set(tid, Config{Rate: 10, Burst: 2})
	now := time.Unix(1_700_000_000, 0)
	l.SetClock(func() time.Time { return now })

	if ok, _ := l.Allow(tid); !ok {
		t.Fatal("call 1 should allow")
	}
	if ok, _ := l.Allow(tid); !ok {
		t.Fatal("call 2 should allow")
	}
	if ok, _ := l.Allow(tid); ok {
		t.Fatal("call 3 should deny (burst exhausted)")
	}
	// Advance 500ms — refill = 10 * 0.5 = 5 tokens, capped at burst=2.
	now = now.Add(500 * time.Millisecond)
	if ok, _ := l.Allow(tid); !ok {
		t.Fatal("post-refill call should allow")
	}
}

func TestLimiter_Independent(t *testing.T) {
	l := NewLimiter()
	l.SetDefaults(Config{Rate: 1, Burst: 1})
	if ok, _ := l.Allow("t-aaaaaaaa"); !ok {
		t.Fatal("t-aaaaaaaa first call must allow")
	}
	// Different tenant has its own bucket and should still allow.
	if ok, _ := l.Allow("t-cccccccc"); !ok {
		t.Fatal("t-cccccccc first call must allow")
	}
	if ok, _ := l.Allow("t-aaaaaaaa"); ok {
		t.Fatal("t-aaaaaaaa second call must deny (burst=1)")
	}
}

func TestLimiter_ZeroConfigIsUnlimited(t *testing.T) {
	l := NewLimiter()
	l.SetDefaults(Config{})
	for i := 0; i < 1000; i++ {
		if ok, _ := l.Allow("t-dddddddd"); !ok {
			t.Fatalf("zero config should be unlimited, denied at %d", i)
		}
	}
}

func TestLimiter_ThreadSafe(t *testing.T) {
	l := NewLimiter()
	l.SetDefaults(Config{Rate: 1000, Burst: 1000})
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _ = l.Allow("t-eeeeeeee")
			}
		}()
	}
	wg.Wait()
}

func TestLimiter_ConfigChangeResetsBucket(t *testing.T) {
	l := NewLimiter()
	tid := tenant.ID("t-ffffffff")
	l.Set(tid, Config{Rate: 1, Burst: 1})
	if ok, _ := l.Allow(tid); !ok {
		t.Fatal("first call must allow")
	}
	if ok, _ := l.Allow(tid); ok {
		t.Fatal("second call must deny")
	}
	// Bump quota; the bucket should reset so a new burst is available.
	l.Set(tid, Config{Rate: 10, Burst: 5})
	if ok, _ := l.Allow(tid); !ok {
		t.Fatal("after reconfig, first call must allow")
	}
}
