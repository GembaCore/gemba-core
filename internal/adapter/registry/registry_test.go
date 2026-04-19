package registry

import "testing"

func TestRegisterAndListSorted(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Register(Adaptor{Name: "zeta", Plane: WorkPlane})
	Register(Adaptor{Name: "alpha", Plane: OrchestrationPlane})
	Register(Adaptor{Name: "beads", Plane: WorkPlane})

	got := List()
	if len(got) != 3 {
		t.Fatalf("want 3 adaptors, got %d", len(got))
	}

	// orchestration sorts before work ("orchestration" < "work"), then by name.
	want := []string{"alpha", "beads", "zeta"}
	for i, a := range got {
		if a.Name != want[i] {
			t.Errorf("index %d: got %q, want %q", i, a.Name, want[i])
		}
	}
}

// gm-b1: Status() must run Probe when present and fall back to Detect when
// not. The banner polls this surface, so the fallback matters: adaptors
// that haven't been updated with a runtime probe still report something
// useful rather than silently showing "unknown".
func TestStatus_PrefersProbeOverDetect(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Register(Adaptor{
		Name:   "has-probe",
		Plane:  WorkPlane,
		Detect: func() DetectResult { return DetectResult{Ok: true} },
		Probe:  func() DetectResult { return DetectResult{Ok: false, Reason: "daemon down"} },
	})
	Register(Adaptor{
		Name:   "detect-only",
		Plane:  OrchestrationPlane,
		Detect: func() DetectResult { return DetectResult{Ok: true} },
	})
	Register(Adaptor{
		Name:  "nothing",
		Plane: OrchestrationPlane,
	})

	got := Status()
	if len(got) != 3 {
		t.Fatalf("want 3 statuses, got %d", len(got))
	}

	// List() sorts orchestration before work, name within plane.
	byName := map[string]AdaptorStatus{}
	for _, s := range got {
		byName[s.Name] = s
	}

	if byName["has-probe"].Healthy {
		t.Errorf("has-probe: Probe must override Detect; got Healthy=true")
	}
	if byName["has-probe"].Reason != "daemon down" {
		t.Errorf("has-probe: want Reason=%q, got %q", "daemon down", byName["has-probe"].Reason)
	}
	if !byName["detect-only"].Healthy {
		t.Errorf("detect-only: want Healthy=true (fallback to Detect), got false")
	}
	if byName["nothing"].Healthy {
		t.Errorf("nothing: no probe or detect must report unhealthy")
	}
	if byName["nothing"].Reason == "" {
		t.Errorf("nothing: must report a reason explaining the missing probe")
	}
}

// gm-b1: PlaneHealth drives the mutation gate. Returns the single
// registered adaptor's status for the plane; ok=false when none
// registered so callers can distinguish "degraded" from "not configured".
func TestPlaneHealth_ReportsPerPlane(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Register(Adaptor{
		Name:  "beads",
		Plane: WorkPlane,
		Probe: func() DetectResult { return DetectResult{Ok: false, Reason: "Dolt unreachable"} },
	})

	s, ok := PlaneHealth(WorkPlane)
	if !ok {
		t.Fatalf("WorkPlane should be registered")
	}
	if s.Healthy {
		t.Errorf("WorkPlane should be unhealthy")
	}
	if s.Reason != "Dolt unreachable" {
		t.Errorf("want Reason=%q, got %q", "Dolt unreachable", s.Reason)
	}

	if _, ok := PlaneHealth(OrchestrationPlane); ok {
		t.Errorf("OrchestrationPlane has no registered adaptor; want ok=false")
	}
}

func TestListReturnsCopy(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Register(Adaptor{Name: "x", Plane: WorkPlane})
	got := List()
	got[0].Name = "mutated"

	again := List()
	if again[0].Name != "x" {
		t.Fatalf("List() must not share backing storage: got %q", again[0].Name)
	}
}
