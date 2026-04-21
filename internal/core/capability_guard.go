// Package core: see doc.go for the overview.
package core

import (
	"context"
	"fmt"
)

// Operation is the closed set of adaptor-boundary operations whose
// availability depends on the adaptor's CapabilityManifest (gm-4qf,
// resolves DD-12 of the t3code audit). Only operations the manifest
// actually gates appear here; unconditionally-available operations
// (Describe, Get/List/Create/Update WorkItem) are included for
// completeness so callers can run the guard uniformly across every
// boundary call.
//
// Values are stable wire tokens used as map keys and `Detail["operation"]`
// on AdaptorError payloads. Keep snake_case so SPA consumers can branch
// on them without a translation table.
type Operation string

const (
	// OpDescribe — Describe(). Always allowed; listed so uniform guard
	// wrappers can name every boundary method.
	OpDescribe Operation = "describe"
	// OpListWorkItems — ListWorkItems(). Always allowed.
	OpListWorkItems Operation = "list_work_items"
	// OpGetWorkItem — GetWorkItem(). Always allowed.
	OpGetWorkItem Operation = "get_work_item"
	// OpCreateWorkItem — CreateWorkItem(). Always allowed.
	OpCreateWorkItem Operation = "create_work_item"
	// OpUpdateWorkItem — UpdateWorkItem(). Always allowed.
	OpUpdateWorkItem Operation = "update_work_item"
	// OpAttachEvidence — AttachEvidence(). Gated by
	// CapabilityManifest.EvidenceSynthesisRequired: when the manifest
	// declares that the adaptor provides its own Evidence
	// (EvidenceSynthesisRequired=false), core MUST NOT call AttachEvidence.
	OpAttachEvidence Operation = "attach_evidence"
	// OpListSprints — ListSprints(). Gated by
	// CapabilityManifest.SprintNative.
	OpListSprints Operation = "list_sprints"
	// OpReadBudgetRollup — ReadBudgetRollup(). Gated by BOTH SprintNative
	// (rollups are per-sprint) and TokenBudgetEnforced (no budget, no
	// rollup).
	OpReadBudgetRollup Operation = "read_budget_rollup"
)

// allOperations is the authoritative list used by IsKnownOperation and
// tests that want to iterate every gated op. Keep in sync with the const
// block above.
var allOperations = []Operation{
	OpDescribe,
	OpListWorkItems,
	OpGetWorkItem,
	OpCreateWorkItem,
	OpUpdateWorkItem,
	OpAttachEvidence,
	OpListSprints,
	OpReadBudgetRollup,
}

// IsKnownOperation reports whether op is one of the canonical Operations.
// An unknown op is treated as a denied-by-default by the guard: a caller
// that asks about a made-up op shouldn't silently get Allowed=true.
func IsKnownOperation(op Operation) bool {
	for _, known := range allOperations {
		if known == op {
			return true
		}
	}
	return false
}

// GuardDecision is the pure output of CheckCapability. A caller that
// only needs a boolean can check Allowed; a caller that wants to surface
// a reason (logs, 403 response body, SPA banner) uses Reason.
//
// Allowed=true implies Reason is empty; Allowed=false guarantees a
// non-empty Reason so the error path never loses its explanation.
type GuardDecision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// CheckCapability reports whether op is allowed for a manifest. Pure:
// no side effects, no I/O, no randomness. Safe to call on every request.
//
// The function is the primary capability gate (gm-4qf). The rule is:
// core enforces AT THE PORT — before a call reaches the adaptor — by
// translating the adaptor's declarative CapabilityManifest into a
// per-op allow/deny decision. Adaptors themselves MUST also fail-fast
// on undeclared ops (defense in depth); see EnforceCapability for the
// canonical error construction.
//
// Lesson from the t3code audit: every adapter implementing its own
// enforcement drifts into four subtly different behaviors. Gating
// centrally here removes that drift and keeps the decision one
// Go-testable pure function away from the contract.
func CheckCapability(m CapabilityManifest, op Operation) GuardDecision {
	switch op {
	case OpDescribe,
		OpListWorkItems,
		OpGetWorkItem,
		OpCreateWorkItem,
		OpUpdateWorkItem:
		// Core CRUD surface. Always part of the contract; manifest does
		// not gate.
		return GuardDecision{Allowed: true}

	case OpAttachEvidence:
		if !m.EvidenceSynthesisRequired {
			return GuardDecision{
				Allowed: false,
				Reason: "attach_evidence denied: manifest evidence_synthesis_required=false " +
					"(adaptor provides its own Evidence in GetWorkItem)",
			}
		}
		return GuardDecision{Allowed: true}

	case OpListSprints:
		if !m.SprintNative {
			return GuardDecision{
				Allowed: false,
				Reason:  "list_sprints denied: manifest sprint_native=false",
			}
		}
		return GuardDecision{Allowed: true}

	case OpReadBudgetRollup:
		// Rollups hang off sprints AND require a real enforced budget.
		// Both declarations are required; deny on the first that fails
		// so the reason is actionable.
		if !m.SprintNative {
			return GuardDecision{
				Allowed: false,
				Reason: "read_budget_rollup denied: manifest sprint_native=false " +
					"(rollups are per-sprint)",
			}
		}
		if !m.TokenBudgetEnforced {
			return GuardDecision{
				Allowed: false,
				Reason:  "read_budget_rollup denied: manifest token_budget_enforced=false",
			}
		}
		return GuardDecision{Allowed: true}

	default:
		return GuardDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("unknown capability operation %q", op),
		}
	}
}

// EnforceCapability is the error-returning sibling of CheckCapability.
// When the op is denied, it returns a tagged *AdaptorError with
// Kind=capability_denied so callers can branch on errors.Is /
// core.AsAdaptorError without touching the human-readable message.
//
// Adaptors MUST call this (or produce an equivalent capability_denied
// error) on any boundary method the manifest opts out of; the guard is
// the primary check but adaptor-side fail-fast is the defense-in-depth
// requirement from the t3code lesson.
func EnforceCapability(m CapabilityManifest, op Operation) error {
	d := CheckCapability(m, op)
	if d.Allowed {
		return nil
	}
	return &AdaptorError{
		Kind:      KindCapabilityDenied,
		Retryable: false,
		Message:   d.Reason,
		Detail: map[string]any{
			"operation":    string(op),
			"adaptor_name": m.AdaptorName,
		},
	}
}

// GuardedWorkPlane wraps inner with a port-level CapabilityManifest
// guard. Every gated boundary call first consults CheckCapability
// against the manifest that Describe reports; denied ops return a
// capability_denied AdaptorError WITHOUT reaching inner.
//
// The wrapper is the canonical core-side enforcement point required
// by gm-4qf ("every mutation handler in the core calls the guard
// BEFORE routing to the adaptor"). Adaptors remain required to
// fail-fast on their own as defense in depth — the guarded wrapper
// and the adaptor's internal check MUST agree.
//
// The manifest is captured at construction so each call avoids a
// Describe round-trip; callers that want per-reconnect refresh should
// rebuild the wrapper. Panics on construction if inner is nil.
func GuardedWorkPlane(inner WorkPlane, manifest CapabilityManifest) WorkPlane {
	if inner == nil {
		panic("core: GuardedWorkPlane called with nil inner WorkPlane")
	}
	return &guardedWorkPlane{inner: inner, manifest: manifest}
}

type guardedWorkPlane struct {
	inner    WorkPlane
	manifest CapabilityManifest
}

func (g *guardedWorkPlane) Describe(ctx context.Context) (CapabilityManifest, error) {
	return g.inner.Describe(ctx)
}

func (g *guardedWorkPlane) ListWorkItems(ctx context.Context, f WorkItemFilter) ([]WorkItem, error) {
	return g.inner.ListWorkItems(ctx, f)
}

func (g *guardedWorkPlane) GetWorkItem(ctx context.Context, id WorkItemID) (WorkItem, error) {
	return g.inner.GetWorkItem(ctx, id)
}

func (g *guardedWorkPlane) CreateWorkItem(ctx context.Context, wi WorkItem) (WorkItem, error) {
	return g.inner.CreateWorkItem(ctx, wi)
}

func (g *guardedWorkPlane) UpdateWorkItem(
	ctx context.Context, id WorkItemID, patch WorkItemPatch,
) (WorkItem, error) {
	return g.inner.UpdateWorkItem(ctx, id, patch)
}

func (g *guardedWorkPlane) AttachEvidence(
	ctx context.Context, id WorkItemID, ev Evidence,
) error {
	if err := EnforceCapability(g.manifest, OpAttachEvidence); err != nil {
		return err
	}
	return g.inner.AttachEvidence(ctx, id, ev)
}

func (g *guardedWorkPlane) ListSprints(ctx context.Context) ([]Sprint, error) {
	if err := EnforceCapability(g.manifest, OpListSprints); err != nil {
		return nil, err
	}
	return g.inner.ListSprints(ctx)
}

func (g *guardedWorkPlane) ReadBudgetRollup(
	ctx context.Context, sprintID string,
) (BudgetRollup, error) {
	if err := EnforceCapability(g.manifest, OpReadBudgetRollup); err != nil {
		return BudgetRollup{}, err
	}
	return g.inner.ReadBudgetRollup(ctx, sprintID)
}

// AssertCapabilityDenied is the Conformance Group E helper for the
// capability-enforcement rule: given an error observed from an adaptor
// boundary call the adaptor was expected to refuse, it returns nil when
// the error is a properly-tagged capability_denied AdaptorError, and a
// descriptive error otherwise.
//
// Conformance suites wire this after calling an undeclared op against
// an adaptor. An adaptor that returns a bare error, a legacy
// ErrUnsupported sentinel without a tagged envelope, or silently
// succeeds fails Group E and must be fixed before it can ship.
//
// Note: callers that ALSO want to assert Group F (tagged-error shape)
// should still call AssertAdaptorError; this helper is the stricter
// "denied for the right reason" check. A capability_denied kind is
// required here because the generic KindUnsupported is reserved for
// coarse feature-group opt-out at the adaptor's discretion — the
// capability guard uses the more specific discriminator so the SPA can
// distinguish "adaptor refused because manifest said so" from "adaptor
// technically could do this but chose not to".
func AssertCapabilityDenied(err error) error {
	if err == nil {
		return fmt.Errorf(
			"conformance Group E: adaptor returned nil for an undeclared capability; " +
				"expected capability_denied AdaptorError")
	}
	ae := AsAdaptorError(err)
	if ae == nil {
		return fmt.Errorf(
			"conformance Group E: adaptor returned untagged error %T (%q); "+
				"undeclared ops MUST return an *AdaptorError with Kind=capability_denied",
			err, err.Error())
	}
	if ae.Kind != KindCapabilityDenied {
		return fmt.Errorf(
			"conformance Group E: AdaptorError._kind=%q, want %q; "+
				"undeclared ops MUST return capability_denied",
			ae.Kind, KindCapabilityDenied)
	}
	return nil
}
