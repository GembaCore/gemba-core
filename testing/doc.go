// Package testing publishes Gemba's conformance test harness as an
// importable library so third-party adaptor authors can run our contract
// tests inside their own `go test` suites (gm-2am, Foolery-spike lesson).
//
// Import as:
//
//	import gembatesting "github.com/MikeBengtson/gemba/testing"
//
// The alias avoids the obvious clash with the standard-library `testing`
// package, which every harness consumer also imports.
//
// # Entry points
//
//   - RunWorkPlaneConformance(t, impl, fixture)
//   - RunOrchestrationConformance(t, impl, fixture)
//
// Each function runs the probe groups defined in docs/adaptors/*.md as
// t.Run subtests. A fixture argument lets the adaptor pre-seed the data
// the probes need (known-missing ids, a live session id) so that
// adaptors with no way to construct fixture state still pass the
// fixture-independent groups.
//
// # Conformance groups
//
// WorkPlane (docs/adaptors/workplane.md):
//
//   - Group A — manifest validity, JSON round-trip, Describe idempotency.
//   - Group E — capability denial: gated ops the manifest opts out of
//     surface as `capability_denied` AdaptorError (gm-4qf).
//   - Group F — error algebra: every observed boundary error is a
//     tagged *core.AdaptorError (gm-faz).
//
// Orchestration (docs/adaptors/orchestration.md):
//
//   - Group A — manifest validity, required-axes coverage.
//   - Group B — session lifecycle: EndSession idempotency (same-nonce
//     replay + terminal-absorbing), CloseReason populated, active-turn
//     protection, scope teardown, ListPendingRequests exists.
//   - Group F — error algebra: ListPendingRequests on a bogus session
//     id returns a tagged KindSessionNotFound.
//
// # Stability
//
// The function signatures are stable across minor releases; new probe
// groups MAY be added behind new subtests without bumping the major
// version, but existing passing adaptors MUST keep passing. Breaking
// probe changes land with a bump of [core.ProtocolVersion].
package gembatesting
