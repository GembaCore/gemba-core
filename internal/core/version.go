// Package core: see doc.go for the overview.
package core

// ProtocolVersion is the adaptor contract version the core advertises at
// startup. Every adaptor MUST populate CapabilityManifest.ProtocolVersion
// (WorkPlane) or OrchestrationCapabilityManifest.OrchestrationAPIVersion
// (OrchestrationPlane) with the exact same value.
//
// Mismatches are rejected at registration time (gm-e3.4) with an
// actionable error, before any query is served. Bump only when the
// semantic contract changes in a way adaptors must track — adding new
// optional manifest fields does not warrant a bump; changing a method
// signature or an error contract does.
//
// Resolves DD-12.
const ProtocolVersion = "1.0.0"
