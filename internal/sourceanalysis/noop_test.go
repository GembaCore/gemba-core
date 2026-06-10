package sourceanalysis

import (
	"context"
	"errors"
	"testing"
)

func TestNoop_PassesContract(t *testing.T) {
	RunContract(t, NewNoop(), Target{Repository: "gemba", Path: "main.go"})
}

func TestNoop_AllQueriesReturnErrUnavailable(t *testing.T) {
	n := NewNoop()
	ctx := context.Background()

	if _, err := n.Dependents(ctx, Target{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Dependents err = %v, want ErrUnavailable", err)
	}
	if _, err := n.Dependencies(ctx, Target{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Dependencies err = %v, want ErrUnavailable", err)
	}
	if _, err := n.PublicContractChanges(ctx, Diff{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("PublicContractChanges err = %v, want ErrUnavailable", err)
	}
}

func TestNoop_DescribeReportsUnavailable(t *testing.T) {
	caps, err := NewNoop().Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe returned error: %v", err)
	}
	if caps.Backend != "noop" {
		t.Errorf("Backend = %q, want %q", caps.Backend, "noop")
	}
	if caps.Available {
		t.Error("Available = true, want false")
	}
	if caps.Reason == "" {
		t.Error("Reason is empty; noop MUST self-explain")
	}
}

func TestNoop_CustomReason(t *testing.T) {
	n := &Noop{Reason: "custom: gitnexus disabled in this rig"}
	caps, _ := n.Describe(context.Background())
	if caps.Reason != "custom: gitnexus disabled in this rig" {
		t.Errorf("custom reason not preserved; got %q", caps.Reason)
	}
}
