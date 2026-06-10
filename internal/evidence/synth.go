package evidence

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/GembaCore/gemba-core/core"
)

// Synthesizer is the facade adaptors call. It owns an ordered slice
// of [Collector]s, runs them in parallel against a [Target], and
// merges the results in registration order.
//
// A nil/empty Synthesizer is valid (zero-value): Collect always
// returns (nil, nil).
type Synthesizer struct {
	collectors []Collector
	// OnError, if non-nil, is invoked once per failing collector.
	// Default behavior logs at warn level and continues.
	OnError func(src Source, err error)
}

// NewSynthesizer returns a Synthesizer pre-loaded with the three
// default collectors (git log, GitHub PRs, GitHub Actions CI).
func NewSynthesizer() *Synthesizer {
	return &Synthesizer{
		collectors: []Collector{
			NewGitLogCollector(),
			NewGitHubPRCollector(),
			NewCICollector(),
		},
	}
}

// NewSynthesizerWith returns a Synthesizer with the supplied
// collectors. The order is preserved in the merged output.
func NewSynthesizerWith(cs ...Collector) *Synthesizer {
	return &Synthesizer{collectors: append([]Collector(nil), cs...)}
}

// Add appends a collector. Returns the receiver for fluent chaining.
func (s *Synthesizer) Add(c Collector) *Synthesizer {
	s.collectors = append(s.collectors, c)
	return s
}

// Collectors exposes the registered collectors (read-only view).
func (s *Synthesizer) Collectors() []Collector {
	out := make([]Collector, len(s.collectors))
	copy(out, s.collectors)
	return out
}

// Collect runs every registered collector concurrently and merges
// their results in registration order. Errors are reported via
// OnError (or the default logger) and never abort the merge — a
// failing collector simply contributes zero evidence.
func (s *Synthesizer) Collect(ctx context.Context, t Target) ([]core.Evidence, error) {
	if s == nil || len(s.collectors) == 0 {
		return nil, nil
	}
	results := make([][]core.Evidence, len(s.collectors))
	errs := make([]error, len(s.collectors))
	var wg sync.WaitGroup
	for i, c := range s.collectors {
		wg.Add(1)
		go func(i int, c Collector) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errs[i] = fmt.Errorf("collector %s panicked: %v", c.Source(), r)
				}
			}()
			ev, err := c.Collect(ctx, t)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = ev
		}(i, c)
	}
	wg.Wait()

	// Report errors. Aggregate the failure set so the caller can
	// distinguish total failure from partial.
	var firstErr error
	for i, err := range errs {
		if err == nil {
			continue
		}
		src := s.collectors[i].Source()
		if s.OnError != nil {
			s.OnError(src, err)
		} else {
			slog.Warn("evidence: collector failed",
				"source", string(src), "err", err)
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", src, err)
		}
	}

	// Merge in registration order.
	var merged []core.Evidence
	for _, batch := range results {
		merged = append(merged, batch...)
	}

	// If every collector failed AND nothing was returned, surface
	// the first error so the caller knows synthesis was a total
	// loss. Otherwise we're best-effort.
	if len(merged) == 0 && firstErr != nil && allFailed(errs) {
		return nil, firstErr
	}
	return merged, nil
}

func allFailed(errs []error) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if e == nil {
			return false
		}
	}
	return true
}
