// Package core: see doc.go for the overview.
package core

// Flags is a flat-boolean projection of CapabilityManifest intended for
// UI gating (gm-root DD-15, gm-cpk). The rich CapabilityManifest is the
// right shape for server-side decisions — StateMap, EdgeExtensions,
// FieldExtensions, and so on carry the structural detail core needs to
// route work — but it is unwieldy for simple JSX gates such as
// `<Capability has="has_sprints">`. Flags answers one question per
// boolean: "should this UI control be considered available for the
// active work-plane?".
//
// The foolery spike (docs/prior-art/foolery.md) demonstrated that a flat
// `BackendCapabilities` struct with named booleans (`canSync`,
// `canDelete`, ...) is directly consumable in JSX. Flags is gemba's
// equivalent and gives a well-defined upgrade path for external UI
// consumers (VS Code, Foolery-backend, third-party dashboards) that want
// the projection without re-deriving it from the rich manifest.
//
// Flags is pure: the value is deterministic from the manifest. Callers
// MUST treat it as a cache — always regenerate from the current manifest
// rather than persisting it across reconnects.
type Flags struct {
	// HasSprints — the adaptor emits first-class Sprint records. When
	// false, the UI hides the sprint lane chrome and ListSprints-backed
	// widgets. Derived from CapabilityManifest.SprintNative.
	HasSprints bool `json:"has_sprints"`

	// HasBudget — the adaptor enforces a TokenBudget with the three-tier
	// inform/warn/stop ladder (gm-root DD-14). When false, the UI may
	// still display a configured budget but the stop tier has no runtime
	// effect. Derived from CapabilityManifest.TokenBudgetEnforced.
	HasBudget bool `json:"has_budget"`

	// HasEvidence — core synthesizes Evidence records from transport
	// artifacts for this adaptor (gm-root DD-13). When true, the UI can
	// expect the evidence panel to populate even for backends that do
	// not natively emit Evidence; when false, evidence comes from the
	// adaptor itself. Either way the Evidence panel is renderable, so
	// consumers that only gate visibility should treat this as "on".
	// Derived from CapabilityManifest.EvidenceSynthesisRequired.
	HasEvidence bool `json:"has_evidence"`

	// HasDeclaredState — the manifest declares a non-empty StateMap, so
	// the UI can translate native statuses to the five core
	// StateCategory buckets without guessing. Effectively always true
	// for a Validate()-clean manifest, but surfaced explicitly so the UI
	// can fall back gracefully against a manifest that slipped through.
	// Derived from len(CapabilityManifest.StateMap) > 0.
	HasDeclaredState bool `json:"has_declared_state"`

	// HasExtensionEdges — the adaptor declares at least one
	// EdgeExtension beyond the three core edges. When false, the UI
	// does not need to load `web/src/extensions/<adaptor-id>/` edge
	// widgets. Derived from len(CapabilityManifest.EdgeExtensions) > 0.
	HasExtensionEdges bool `json:"has_extension_edges"`

	// HasExtensionFields — the adaptor declares at least one
	// FieldExtension on WorkItem.Custom. Derived from
	// len(CapabilityManifest.FieldExtensions) > 0.
	HasExtensionFields bool `json:"has_extension_fields"`

	// HasExtensionRelFields — the adaptor declares at least one
	// RelationshipExtension (per-edge metadata). Derived from
	// len(CapabilityManifest.RelationshipExtensions) > 0.
	HasExtensionRelFields bool `json:"has_extension_rel_fields"`
}

// Flags returns the flat-boolean projection of the manifest. Pure and
// deterministic: no side effects, no I/O, no randomness. Callers may
// call it on every render — it allocates only the returned struct.
//
// The method is defined on the value receiver so both pointer and value
// CapabilityManifest values can call it; the manifest is small enough
// that the copy cost is negligible next to the UI work downstream.
func (m CapabilityManifest) Flags() Flags {
	return Flags{
		HasSprints:            m.SprintNative,
		HasBudget:             m.TokenBudgetEnforced,
		HasEvidence:           m.EvidenceSynthesisRequired,
		HasDeclaredState:      len(m.StateMap) > 0,
		HasExtensionEdges:     len(m.EdgeExtensions) > 0,
		HasExtensionFields:    len(m.FieldExtensions) > 0,
		HasExtensionRelFields: len(m.RelationshipExtensions) > 0,
	}
}
