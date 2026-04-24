package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// compile-time assertion: the noopWorkPlane below satisfies the
// WorkPlane interface. If the interface shape drifts, this fails at
// build time rather than at test-run time.
var _ WorkPlane = (*noopWorkPlane)(nil)

type noopWorkPlane struct{}

func (noopWorkPlane) Describe(context.Context) (CapabilityManifest, error) {
	return CapabilityManifest{
		AdaptorName:     "noop",
		AdaptorVersion:  "0.0.0",
		ProtocolVersion: "v1",
		Transport:       TransportJSONL,
		StateMap:        StateMap{"open": StateUnstarted},
	}, nil
}
func (noopWorkPlane) ListWorkItems(context.Context, WorkItemFilter) ([]WorkItem, error) {
	return nil, nil
}
func (noopWorkPlane) GetWorkItem(context.Context, WorkItemID) (WorkItem, error) {
	return WorkItem{}, ErrNotFound
}
func (noopWorkPlane) CreateWorkItem(_ context.Context, wi WorkItem) (WorkItem, error) {
	return wi, nil
}
func (noopWorkPlane) UpdateWorkItem(_ context.Context, _ WorkItemID, _ WorkItemPatch) (WorkItem, error) {
	return WorkItem{}, ErrUnsupported
}
func (noopWorkPlane) AttachEvidence(context.Context, WorkItemID, Evidence) error {
	return ErrUnsupported
}
func (noopWorkPlane) ListSprints(context.Context) ([]Sprint, error) { return nil, nil }
func (noopWorkPlane) ReadBudgetRollup(context.Context, string) (BudgetRollup, error) {
	return BudgetRollup{}, ErrUnsupported
}
func (noopWorkPlane) Subscribe(context.Context, WorkPlaneSubscribeFilter) (<-chan WorkPlaneEvent, error) {
	return nil, ErrUnsupported
}

func TestTransportValid(t *testing.T) {
	for _, tr := range []Transport{TransportAPI, TransportJSONL, TransportMCP} {
		if !tr.Valid() {
			t.Errorf("%q expected Valid()=true", tr)
		}
	}
	if Transport("grpc").Valid() {
		t.Error("grpc should not be a valid core Transport")
	}
}

func TestTransportJSONReject(t *testing.T) {
	var tr Transport
	if err := json.Unmarshal([]byte(`"grpc"`), &tr); err == nil {
		t.Error("expected decode error for unknown transport")
	}
	if err := json.Unmarshal([]byte(`"api"`), &tr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr != TransportAPI {
		t.Errorf("got %q want %q", tr, TransportAPI)
	}
}

func TestStateMapValidate(t *testing.T) {
	good := StateMap{"open": StateUnstarted, "merged": StateCompleted}
	if err := good.Validate(); err != nil {
		t.Fatalf("good map: %v", err)
	}
	bad := StateMap{"weird": StateCategory("nonsense")}
	if err := bad.Validate(); err == nil {
		t.Error("expected error for invalid target category")
	}
}

func TestCapabilityManifestValidate(t *testing.T) {
	base := CapabilityManifest{
		AdaptorName:     "beads",
		AdaptorVersion:  "1.2.3",
		ProtocolVersion: "v1",
		Transport:       TransportJSONL,
		StateMap:        StateMap{"open": StateUnstarted},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("base: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*CapabilityManifest)
		wantErr bool
	}{
		{"missing name", func(m *CapabilityManifest) { m.AdaptorName = "" }, true},
		{"missing protocol", func(m *CapabilityManifest) { m.ProtocolVersion = "" }, true},
		{"bad transport", func(m *CapabilityManifest) { m.Transport = "grpc" }, true},
		{"empty state map", func(m *CapabilityManifest) { m.StateMap = nil }, true},
		{"bad state map", func(m *CapabilityManifest) {
			m.StateMap = StateMap{"x": StateCategory("nope")}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			m.StateMap = StateMap{"open": StateUnstarted}
			tc.mutate(&m)
			err := m.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestCapabilityManifestJSONRoundTrip(t *testing.T) {
	m := CapabilityManifest{
		AdaptorName:     "beads",
		AdaptorVersion:  "1.2.3",
		ProtocolVersion: "v1",
		Transport:       TransportJSONL,
		StateMap: StateMap{
			"open":        StateUnstarted,
			"in_progress": StateStarted,
			"closed":      StateCompleted,
		},
		EdgeExtensions: []EdgeExtension{
			{Name: "duplicate_of", Directed: true, Inverse: "duplicated_by"},
		},
		FieldExtensions: []FieldExtension{
			{Name: "priority_tier", Type: "string"},
		},
		RelationshipExtensions: []RelationshipExtension{
			{Name: "confidence", Type: "number"},
		},
		SprintNative:              true,
		TokenBudgetEnforced:       true,
		EvidenceSynthesisRequired: false,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round CapabilityManifest
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.AdaptorName != m.AdaptorName || round.Transport != m.Transport {
		t.Errorf("round-trip scalar drift: got %+v want %+v", round, m)
	}
	if len(round.StateMap) != len(m.StateMap) ||
		round.StateMap["in_progress"] != StateStarted {
		t.Errorf("round-trip state_map drift: %+v", round.StateMap)
	}
	if !round.SprintNative || !round.TokenBudgetEnforced ||
		round.EvidenceSynthesisRequired {
		t.Errorf("round-trip capability bools drift: %+v", round)
	}
}

func TestErrSentinelsWrap(t *testing.T) {
	wrapped := fmtWrap("lookup %q failed: %w", WorkItemID("gemba/gemba/nope"), ErrNotFound)
	if !errors.Is(wrapped, ErrNotFound) {
		t.Error("wrapped error should still satisfy errors.Is(ErrNotFound)")
	}
	if errors.Is(wrapped, ErrUnsupported) {
		t.Error("wrapped ErrNotFound must not match ErrUnsupported")
	}
}

// fmtWrap is a tiny shim so the test exercises %w wrapping without
// depending on a full adaptor stack.
func fmtWrap(format string, id WorkItemID, err error) error {
	return &wrapErr{msg: format, id: id, err: err}
}

type wrapErr struct {
	msg string
	id  WorkItemID
	err error
}

func (w *wrapErr) Error() string { return w.msg }
func (w *wrapErr) Unwrap() error { return w.err }
