// Package noop provides minimal in-memory WorkPlane and
// OrchestrationPlaneAdaptor implementations used by the conformance
// harness (testing/) as a self-test, and by transports that need a
// valid zero-state adaptor while the real backend is being wired.
//
// The implementations are intentionally small: just enough to pass the
// generic probes in github.com/GembaCore/gemba-core/testing. They are NOT
// production adaptors — no event bus, no persistence, no concurrency
// beyond a single mutex.
package noop
