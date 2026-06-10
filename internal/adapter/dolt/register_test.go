package dolt_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/GembaCore/gemba-core/internal/adapter/dolt"
	"github.com/GembaCore/gemba-core/internal/adapter/registry"
)

// Pins the wire-up serve.go depends on (gm-* fix for the false
// "dolt: not configured" banner). The dolt adaptor's registry probe
// reads from a package-level pool installed via SetProbeDB; the WorkPlane
// must expose its *sql.DB so serve.go can hand it across without
// dialing Dolt twice.
func TestProbe_HealthyAfterSetProbeDB(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectPing()

	wp := dolt.NewWorkPlaneFromDB(db, "", "gemba")
	if got := wp.DB(); got != db {
		t.Fatalf("WorkPlane.DB() must return the wired pool; got %p want %p", got, db)
	}

	// Before SetProbeDB the probe reports the misleading "not configured"
	// reason — which is the bug the user hit.
	dolt.SetProbeDB(nil)
	for _, s := range registry.Status() {
		if s.Name != "beads-dolt" {
			continue
		}
		if s.Healthy {
			t.Fatal("beads-dolt must report unhealthy when no probe DB is wired")
		}
		if s.Reason == "" {
			t.Fatal("beads-dolt unhealthy must carry a reason")
		}
	}

	// After SetProbeDB the probe pings the live pool and reports healthy.
	dolt.SetProbeDB(wp.DB())
	defer dolt.SetProbeDB(nil)

	var seen bool
	for _, s := range registry.Status() {
		if s.Name != "beads-dolt" {
			continue
		}
		seen = true
		if !s.Healthy {
			t.Fatalf("beads-dolt should be healthy after SetProbeDB; reason=%q", s.Reason)
		}
	}
	if !seen {
		t.Fatal("beads-dolt did not appear in registry.Status()")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}
