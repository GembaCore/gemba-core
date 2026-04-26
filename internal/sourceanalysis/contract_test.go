package sourceanalysis

import (
	"context"
	"testing"
	"time"
)

// fakeBackend is a tiny in-test SourceAnalysis used only to exercise
// RunContract here in the package — proves the harness itself works
// before any real binding (gm-s47n.3.2 noop, gm-s47n.3.3 GitNexus)
// exists to plug into it.
type fakeBackend struct {
	available bool
}

func (f *fakeBackend) Dependents(_ context.Context, _ Target) ([]Target, error) {
	if !f.available {
		return nil, ErrUnavailable
	}
	return []Target{}, nil
}

func (f *fakeBackend) Dependencies(_ context.Context, _ Target) ([]Target, error) {
	if !f.available {
		return nil, ErrUnavailable
	}
	return []Target{}, nil
}

func (f *fakeBackend) PublicContractChanges(_ context.Context, _ Diff) ([]Symbol, error) {
	if !f.available {
		return nil, ErrUnavailable
	}
	return []Symbol{}, nil
}

func (f *fakeBackend) Describe(_ context.Context) (Capabilities, error) {
	return Capabilities{
		Backend:        "fake",
		Available:      f.available,
		IndexUpdatedAt: time.Unix(0, 0).UTC(),
	}, nil
}

func TestContract_Available(t *testing.T) {
	RunContract(t, &fakeBackend{available: true}, Target{Repository: "gemba", Path: "internal/sourceanalysis/interface.go"})
}

func TestContract_Unavailable(t *testing.T) {
	RunContract(t, &fakeBackend{available: false}, Target{Repository: "gemba"})
}
