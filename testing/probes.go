package gembatesting

import "fmt"

// probeT is the minimal testing-surface the conformance probes depend
// on. It is a subset of *testing.T so that:
//
//   - The exported RunWorkPlaneConformance / RunOrchestrationConformance
//     entry points can pass *testing.T verbatim — no shim needed.
//   - The programmatic RunWorkPlaneProbes / RunOrchestrationProbes
//     entry points (gm-e3.5 `gemba adaptor test`) can pass a recorder
//     that captures messages for structured reporting.
//
// Every probe in this package MUST take probeT rather than *testing.T;
// that is the invariant that keeps the two entry points in lock-step.
type probeT interface {
	Helper()
	Error(args ...any)
	Errorf(format string, args ...any)
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

// fatalExit is the sentinel a probeRecorder panics with to mimic
// testing.T's Fatal semantics (which call runtime.Goexit). runProbe
// recovers it so the next probe can still run.
type fatalExit struct{}

// probeRecorder implements probeT without failing a test binary. Errors
// and Fatals accumulate into messages; Fatal additionally short-circuits
// the current probe via panic(fatalExit{}).
type probeRecorder struct {
	failed   bool
	messages []string
}

func (p *probeRecorder) Helper() {}

func (p *probeRecorder) Error(args ...any) {
	p.failed = true
	p.messages = append(p.messages, fmt.Sprint(args...))
}

func (p *probeRecorder) Errorf(format string, args ...any) {
	p.failed = true
	p.messages = append(p.messages, fmt.Sprintf(format, args...))
}

func (p *probeRecorder) Fatal(args ...any) {
	p.failed = true
	p.messages = append(p.messages, fmt.Sprint(args...))
	panic(fatalExit{})
}

func (p *probeRecorder) Fatalf(format string, args ...any) {
	p.failed = true
	p.messages = append(p.messages, fmt.Sprintf(format, args...))
	panic(fatalExit{})
}

// runProbe executes fn against a fresh recorder and returns the collected
// result. An unexpected panic (anything that is not fatalExit) propagates
// so bugs in the harness surface instead of silently becoming a failure.
func runProbe(name string, fn func(probeT)) ProbeResult {
	rec := &probeRecorder{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(fatalExit); !ok {
					panic(r)
				}
			}
		}()
		fn(rec)
	}()
	return ProbeResult{
		Name:     name,
		Passed:   !rec.failed,
		Messages: rec.messages,
	}
}
