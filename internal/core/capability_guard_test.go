package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// guardManifest returns a Validate()-clean manifest with every optional
// capability TURNED OFF so each test can flip only the fields it cares
// about. Matches baseManifest() in manifest_test.go but is named
// separately to keep guard tests decoupled.
func guardManifest() CapabilityManifest {
	return CapabilityManifest{
		AdaptorName:     "noop",
		AdaptorVersion:  "0.1.0",
		ProtocolVersion: "v1",
		Transport:       TransportJSONL,
		StateMap:        StateMap{"open": StateUnstarted},
	}
}

func TestCheckCapabilityAlwaysAllowedOps(t *testing.T) {
	m := guardManifest()
	ops := []Operation{
		OpDescribe,
		OpListWorkItems,
		OpGetWorkItem,
		OpCreateWorkItem,
		OpUpdateWorkItem,
	}
	for _, op := range ops {
		t.Run(string(op), func(t *testing.T) {
			d := CheckCapability(m, op)
			if !d.Allowed {
				t.Errorf("op %q must be unconditionally allowed, got %+v", op, d)
			}
			if d.Reason != "" {
				t.Errorf("op %q allowed decision must not carry a reason: %q", op, d.Reason)
			}
		})
	}
}

func TestCheckCapabilityListSprints(t *testing.T) {
	m := guardManifest()
	if d := CheckCapability(m, OpListSprints); d.Allowed {
		t.Errorf("list_sprints must deny when sprint_native=false, got %+v", d)
	}
	m.SprintNative = true
	if d := CheckCapability(m, OpListSprints); !d.Allowed {
		t.Errorf("list_sprints must allow when sprint_native=true, got %+v", d)
	}
}

func TestCheckCapabilityReadBudgetRollup(t *testing.T) {
	m := guardManifest()
	// Both flags off: denied.
	d := CheckCapability(m, OpReadBudgetRollup)
	if d.Allowed {
		t.Errorf("read_budget_rollup must deny with both flags off, got %+v", d)
	}
	if !strings.Contains(d.Reason, "sprint_native") {
		t.Errorf("deny reason should cite sprint_native first: %q", d.Reason)
	}

	// Sprint on, budget off: still denied, now for budget reason.
	m.SprintNative = true
	d = CheckCapability(m, OpReadBudgetRollup)
	if d.Allowed {
		t.Errorf("read_budget_rollup must deny when token_budget_enforced=false, got %+v", d)
	}
	if !strings.Contains(d.Reason, "token_budget_enforced") {
		t.Errorf("deny reason should cite token_budget_enforced: %q", d.Reason)
	}

	// Both on: allowed.
	m.TokenBudgetEnforced = true
	if d := CheckCapability(m, OpReadBudgetRollup); !d.Allowed {
		t.Errorf("read_budget_rollup must allow when both flags are true, got %+v", d)
	}
}

func TestCheckCapabilityAttachEvidence(t *testing.T) {
	m := guardManifest()
	if d := CheckCapability(m, OpAttachEvidence); d.Allowed {
		t.Errorf("attach_evidence must deny when evidence_synthesis_required=false, got %+v", d)
	}
	m.EvidenceSynthesisRequired = true
	if d := CheckCapability(m, OpAttachEvidence); !d.Allowed {
		t.Errorf("attach_evidence must allow when evidence_synthesis_required=true, got %+v", d)
	}
}

func TestCheckCapabilityUnknownOperation(t *testing.T) {
	m := guardManifest()
	d := CheckCapability(m, Operation("list_unicorns"))
	if d.Allowed {
		t.Error("unknown operations must default to denied")
	}
	if !strings.Contains(d.Reason, "unknown capability operation") {
		t.Errorf("unknown op reason drift: %q", d.Reason)
	}
}

func TestCheckCapabilityIsPure(t *testing.T) {
	m := guardManifest()
	m.SprintNative = true
	m.TokenBudgetEnforced = true
	m.EvidenceSynthesisRequired = true
	// Repeated calls return the same decision; guard has no state.
	for i := 0; i < 3; i++ {
		if d := CheckCapability(m, OpReadBudgetRollup); !d.Allowed {
			t.Fatalf("iter %d: CheckCapability must be deterministic: %+v", i, d)
		}
	}
}

func TestIsKnownOperation(t *testing.T) {
	for _, op := range allOperations {
		if !IsKnownOperation(op) {
			t.Errorf("IsKnownOperation(%q)=false for canonical op", op)
		}
	}
	if IsKnownOperation(Operation("imaginary")) {
		t.Error("IsKnownOperation should return false for unknown ops")
	}
}

func TestEnforceCapabilityReturnsTaggedError(t *testing.T) {
	m := guardManifest()
	err := EnforceCapability(m, OpListSprints)
	if err == nil {
		t.Fatal("EnforceCapability should return an error for a denied op")
	}
	ae := AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("EnforceCapability must return an *AdaptorError, got %T", err)
	}
	if ae.Kind != KindCapabilityDenied {
		t.Errorf("Kind=%q want %q", ae.Kind, KindCapabilityDenied)
	}
	if ae.Retryable {
		t.Error("capability_denied must never be retryable")
	}
	if got, ok := ae.Detail["operation"]; !ok || got != string(OpListSprints) {
		t.Errorf("Detail.operation drift: %+v", ae.Detail)
	}
	if got, ok := ae.Detail["adaptor_name"]; !ok || got != m.AdaptorName {
		t.Errorf("Detail.adaptor_name drift: %+v", ae.Detail)
	}
	// Group F (tagged shape) also passes.
	if err := AssertAdaptorError(err); err != nil {
		t.Errorf("EnforceCapability output must pass Group F: %v", err)
	}
}

func TestEnforceCapabilityAllowReturnsNil(t *testing.T) {
	m := guardManifest()
	m.SprintNative = true
	if err := EnforceCapability(m, OpListSprints); err != nil {
		t.Errorf("EnforceCapability must return nil for an allowed op: %v", err)
	}
}

// ---- GuardedWorkPlane integration test -------------------------------------

// capturingWorkPlane records which methods were reached. It lets the
// integration test assert that the guard denied the call BEFORE it hit
// the adaptor, not after.
type capturingWorkPlane struct {
	attachEvidenceCalled bool
	listSprintsCalled    bool
	readBudgetCalled     bool
	updateWorkItemCalled bool
}

func (c *capturingWorkPlane) Describe(context.Context) (CapabilityManifest, error) {
	return CapabilityManifest{}, nil
}
func (*capturingWorkPlane) ListWorkItems(context.Context, WorkItemFilter) ([]WorkItem, error) {
	return nil, nil
}
func (*capturingWorkPlane) GetWorkItem(context.Context, WorkItemID) (WorkItem, error) {
	return WorkItem{}, nil
}
func (*capturingWorkPlane) CreateWorkItem(_ context.Context, wi WorkItem) (WorkItem, error) {
	return wi, nil
}
func (c *capturingWorkPlane) UpdateWorkItem(
	context.Context, WorkItemID, WorkItemPatch,
) (WorkItem, error) {
	c.updateWorkItemCalled = true
	return WorkItem{}, nil
}
func (c *capturingWorkPlane) AttachEvidence(context.Context, WorkItemID, Evidence) error {
	c.attachEvidenceCalled = true
	return nil
}
func (c *capturingWorkPlane) ListSprints(context.Context) ([]Sprint, error) {
	c.listSprintsCalled = true
	return nil, nil
}
func (c *capturingWorkPlane) ReadBudgetRollup(context.Context, string) (BudgetRollup, error) {
	c.readBudgetCalled = true
	return BudgetRollup{}, nil
}

// TestGuardedWorkPlaneBlocksBeforeAdaptor is the gm-4qf integration
// check called out in the bead: disable a capability in the manifest,
// invoke the consumer path, and verify the call never reaches the
// adaptor — the port-level guard MUST gate BEFORE routing.
func TestGuardedWorkPlaneBlocksBeforeAdaptor(t *testing.T) {
	inner := &capturingWorkPlane{}
	// Manifest opts out of every optional capability.
	m := guardManifest()
	plane := GuardedWorkPlane(inner, m)

	ctx := context.Background()

	// ListSprints: denied because sprint_native=false.
	if _, err := plane.ListSprints(ctx); err == nil {
		t.Error("GuardedWorkPlane must deny ListSprints for sprint_native=false")
	} else if assertErr := AssertCapabilityDenied(err); assertErr != nil {
		t.Errorf("ListSprints denial must pass Group E: %v", assertErr)
	}
	if inner.listSprintsCalled {
		t.Error("ListSprints reached inner adaptor despite denial — guard must gate BEFORE routing")
	}

	// ReadBudgetRollup: denied.
	if _, err := plane.ReadBudgetRollup(ctx, "sprint-1"); err == nil {
		t.Error("GuardedWorkPlane must deny ReadBudgetRollup")
	} else if assertErr := AssertCapabilityDenied(err); assertErr != nil {
		t.Errorf("ReadBudgetRollup denial must pass Group E: %v", assertErr)
	}
	if inner.readBudgetCalled {
		t.Error("ReadBudgetRollup reached inner adaptor despite denial")
	}

	// AttachEvidence: denied because evidence_synthesis_required=false.
	if err := plane.AttachEvidence(ctx, WorkItemID("x"), Evidence{}); err == nil {
		t.Error("GuardedWorkPlane must deny AttachEvidence")
	} else if assertErr := AssertCapabilityDenied(err); assertErr != nil {
		t.Errorf("AttachEvidence denial must pass Group E: %v", assertErr)
	}
	if inner.attachEvidenceCalled {
		t.Error("AttachEvidence reached inner adaptor despite denial")
	}

	// UpdateWorkItem: always allowed, must reach inner.
	if _, err := plane.UpdateWorkItem(ctx, WorkItemID("x"), WorkItemPatch{}); err != nil {
		t.Errorf("UpdateWorkItem is unconditional; guard should pass it through: %v", err)
	}
	if !inner.updateWorkItemCalled {
		t.Error("UpdateWorkItem was blocked by guard — unconditional ops must always route")
	}
}

func TestGuardedWorkPlaneAllowsWhenManifestDeclares(t *testing.T) {
	inner := &capturingWorkPlane{}
	m := guardManifest()
	m.SprintNative = true
	m.TokenBudgetEnforced = true
	m.EvidenceSynthesisRequired = true
	plane := GuardedWorkPlane(inner, m)

	ctx := context.Background()

	if _, err := plane.ListSprints(ctx); err != nil {
		t.Errorf("ListSprints: want allowed, got %v", err)
	}
	if !inner.listSprintsCalled {
		t.Error("ListSprints did not reach inner despite being allowed")
	}
	if _, err := plane.ReadBudgetRollup(ctx, "sprint-1"); err != nil {
		t.Errorf("ReadBudgetRollup: want allowed, got %v", err)
	}
	if !inner.readBudgetCalled {
		t.Error("ReadBudgetRollup did not reach inner")
	}
	if err := plane.AttachEvidence(ctx, WorkItemID("x"), Evidence{}); err != nil {
		t.Errorf("AttachEvidence: want allowed, got %v", err)
	}
	if !inner.attachEvidenceCalled {
		t.Error("AttachEvidence did not reach inner")
	}
}

func TestGuardedWorkPlaneNilInnerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("GuardedWorkPlane(nil, ...) must panic")
		}
	}()
	_ = GuardedWorkPlane(nil, guardManifest())
}

// ---- AssertCapabilityDenied conformance ------------------------------------

func TestAssertCapabilityDenied(t *testing.T) {
	// POSITIVE: a properly-tagged denial passes.
	good := EnforceCapability(guardManifest(), OpListSprints)
	if err := AssertCapabilityDenied(good); err != nil {
		t.Errorf("tagged capability_denied should pass Group E: %v", err)
	}

	// Wrapped tagged error still passes.
	wrapped := &wrapErr{msg: "adaptor=bd: %w", err: good}
	if err := AssertCapabilityDenied(wrapped); err != nil {
		t.Errorf("wrapped capability_denied should pass Group E: %v", err)
	}

	// NEGATIVE: nil error — adaptor silently succeeded.
	if err := AssertCapabilityDenied(nil); err == nil {
		t.Error("Group E must reject nil error on an undeclared op")
	}

	// NEGATIVE: bare error.
	if err := AssertCapabilityDenied(errors.New("nope")); err == nil {
		t.Error("Group E must reject bare errors from adaptors")
	} else if !strings.Contains(err.Error(), "Group E") {
		t.Errorf("Group E failure should cite the group: %q", err.Error())
	}

	// NEGATIVE: tagged but wrong kind — ErrUnsupported-style legacy sentinel
	// mapped onto KindUnsupported. Group E demands capability_denied
	// specifically so the SPA can distinguish the two.
	wrongKind := NewAdaptorError(KindUnsupported, "read budget")
	if err := AssertCapabilityDenied(wrongKind); err == nil {
		t.Error("Group E must reject KindUnsupported — requires capability_denied")
	} else if !strings.Contains(err.Error(), "capability_denied") {
		t.Errorf("Group E failure should name the required kind: %q", err.Error())
	}
}
