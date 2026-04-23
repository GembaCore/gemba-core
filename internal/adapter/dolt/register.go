package dolt

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
)

func init() {
	registry.Register(registry.Adaptor{
		Name:   adaptorName,
		Plane:  registry.WorkPlane,
		Detect: detect,
		Probe:  probe,
	})
}

// probeDB holds the *sql.DB the serve command installs after
// NewWorkPlane succeeds. The registry's Detect/Probe hooks run
// without access to the serve config, so we carry the pool in a
// package-level hook rather than dialing a second time on every
// health check. SetProbeDB(nil) disables the hook — what tests do
// so they can observe the "not configured" branch.
var (
	probeMu sync.RWMutex
	probeDB *sql.DB
)

// SetProbeDB installs the *sql.DB the runtime health check should
// exercise. Wire this from `gemba serve` after the dolt adaptor
// boots so /api/adaptors reflects live Dolt state. Passing nil
// clears the hook — the probe then reports "not configured".
func SetProbeDB(db *sql.DB) {
	probeMu.Lock()
	probeDB = db
	probeMu.Unlock()
}

// detect is the startup-time capability check. The dolt adaptor is
// opt-in via --dolt-url, so detect reports false unless a pool has
// been installed through SetProbeDB. That keeps the adaptor
// invisible to operators who haven't asked for it while still
// surfacing on /api/adaptors the moment they do.
func detect() registry.DetectResult {
	probeMu.RLock()
	db := probeDB
	probeMu.RUnlock()
	if db == nil {
		return registry.DetectResult{
			Reason: "dolt: not configured (pass --dolt-url to enable)",
		}
	}
	return registry.DetectResult{Ok: true}
}

// probeTimeout bounds the runtime health ping. Short enough that a
// hung Dolt surfaces as degraded rather than stalling the health
// endpoint.
var probeTimeout = 2 * time.Second

// probe runs a cheap ping against the installed *sql.DB. Typical
// failure signatures:
//   - Dolt process killed → "connection refused" in <100ms
//   - Dolt hung → deadline exceeded
//   - Pool exhausted → context cancel from pool acquire
func probe() registry.DetectResult {
	probeMu.RLock()
	db := probeDB
	probeMu.RUnlock()
	if db == nil {
		return registry.DetectResult{
			Reason: "dolt: not configured (pass --dolt-url to enable)",
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return registry.DetectResult{
				Reason: "dolt: ping timed out (server may be hung; see `gt dolt status`)",
			}
		}
		return registry.DetectResult{
			Reason: "dolt: " + firstLine(err.Error()),
		}
	}
	return registry.DetectResult{Ok: true}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
