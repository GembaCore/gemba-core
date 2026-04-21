package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	eventlog "github.com/MikeBengtson/gemba/internal/events/log"
)

// withRegistry swaps the process-wide adaptor registry for the duration of
// a test. Doctor reads the real registry via package init() side effects,
// so tests need to reset and repopulate it to assert outcomes.
func withRegistry(t *testing.T, adaptors ...registry.Adaptor) {
	t.Helper()
	registry.Reset()
	for _, a := range adaptors {
		registry.Register(a)
	}
	t.Cleanup(registry.Reset)
}

func TestDoctor_NoPair_ReturnsError(t *testing.T) {
	withRegistry(t,
		registry.Adaptor{
			Name:  "beads",
			Plane: registry.WorkPlane,
			Detect: func() registry.DetectResult {
				return registry.DetectResult{Ok: true}
			},
		},
		registry.Adaptor{
			Name:  "gastown",
			Plane: registry.OrchestrationPlane,
			Detect: func() registry.DetectResult {
				return registry.DetectResult{Reason: "no HQ"}
			},
		},
	)

	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error when no orchestration adaptor detects, got nil")
	}
	if !strings.Contains(err.Error(), "no WorkPlane + OrchestrationPlane pair") {
		t.Errorf("error missing expected text: %v", err)
	}
	if !strings.Contains(out.String(), "✓ beads") {
		t.Errorf("output missing detected WorkPlane: %s", out.String())
	}
	if !strings.Contains(out.String(), "✗ gastown — no HQ") {
		t.Errorf("output missing failed OrchestrationPlane reason: %s", out.String())
	}
}

func TestDoctor_PairDetected_Succeeds(t *testing.T) {
	withRegistry(t,
		registry.Adaptor{
			Name:  "beads",
			Plane: registry.WorkPlane,
			Detect: func() registry.DetectResult {
				return registry.DetectResult{Ok: true}
			},
		},
		registry.Adaptor{
			Name:  "gastown",
			Plane: registry.OrchestrationPlane,
			Detect: func() registry.DetectResult {
				return registry.DetectResult{Ok: true}
			},
		},
	)

	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "pair detected") {
		t.Errorf("output missing success line: %s", out.String())
	}
}

func TestDoctor_ReportsEventLog(t *testing.T) {
	withRegistry(t,
		registry.Adaptor{
			Name:   "beads",
			Plane:  registry.WorkPlane,
			Detect: func() registry.DetectResult { return registry.DetectResult{Ok: true} },
		},
		registry.Adaptor{
			Name:   "gastown",
			Plane:  registry.OrchestrationPlane,
			Detect: func() registry.DetectResult { return registry.DetectResult{Ok: true} },
		},
	)

	// Seed an event log with a couple of rotations so stats are non-zero.
	dir := t.TempDir()
	w, err := eventlog.Open(eventlog.Policy{
		Dir:          dir,
		MaxFileBytes: 40,
		MaxFiles:     3,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := w.WriteEvent([]byte(`{"ev":"test"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--eventlog-dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o := out.String()
	if !strings.Contains(o, "Event log") {
		t.Fatalf("expected event log section, got:\n%s", o)
	}
	if !strings.Contains(o, "rotated files:") {
		t.Fatalf("expected rotated files count, got:\n%s", o)
	}
	if !strings.Contains(o, "oldest retained:") {
		t.Fatalf("expected oldest retained line, got:\n%s", o)
	}
	if !strings.Contains(o, filepath.Clean(dir)) {
		t.Fatalf("expected doctor to echo the eventlog dir %q, got:\n%s", dir, o)
	}
}

func TestDoctor_EmptyRegistry_Fails(t *testing.T) {
	withRegistry(t) // register nothing

	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err == nil {
		t.Fatalf("want error with empty registry, got nil")
	}
	if !strings.Contains(out.String(), "(none registered)") {
		t.Errorf("output missing empty marker: %s", out.String())
	}
}
