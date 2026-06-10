package concepts

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// BootstrapSource extracts candidate vocabulary terms from one
// observable feature of the workspace. The interface stays small so
// adding a source (e.g. an org-internal Linear label exporter) is a
// one-method change.
type BootstrapSource interface {
	// Name is a stable identifier the [Term.Source] field carries
	// so a future operator can tell which source proposed a term.
	// Convention: lower-kebab-case noun phrase.
	Name() string

	// Extract returns the candidates this source observed under root.
	// Implementations MUST return [] (not error) when there's nothing
	// to extract — a workspace without an `internal/` directory is a
	// legitimate state, not a failure.
	Extract(ctx context.Context, root string) ([]Candidate, error)
}

// Candidate is one proposed vocabulary entry. Bootstrap collects,
// normalizes, and dedupes by Name; the first source to propose a
// name wins for the Source label.
type Candidate struct {
	Name        string
	Description string
	// Source is overwritten by Bootstrap with the originating
	// BootstrapSource.Name(); implementations can leave it empty.
	Source string
}

// BootstrapOpts controls collection limits.
type BootstrapOpts struct {
	// Max caps the number of terms in the resulting vocabulary. The
	// bead description targets 30-60; default is 60. Sources are
	// queried in order so an early source's candidates fill first;
	// callers wanting different priority can reorder the slice.
	Max int
}

// DefaultBootstrapOpts is the ship-with policy: at most 60 terms.
func DefaultBootstrapOpts() BootstrapOpts {
	return BootstrapOpts{Max: 60}
}

// DefaultSources returns the bootstrap sources gemba ships. Order
// matters — earlier sources fill the cap first when [BootstrapOpts.Max]
// is small. Operators wanting a different priority compose their own
// slice.
func DefaultSources() []BootstrapSource {
	return []BootstrapSource{
		GoPackagesSource{},
		RoutePrefixesSource{},
		FixtureTaxonomySource{},
	}
}

// Bootstrap runs every source in parallel, unions the candidates,
// normalizes, dedupes, and caps. Returns a fresh [Vocabulary] with
// stable name order. Source selection is the caller's
// responsibility; pass [DefaultSources] for the ship-with set.
//
// Errors from individual sources are collected and surfaced via
// [BootstrapResult.Errors]; the vocabulary is returned even when
// some sources failed because the surviving sources still produce
// useful starter terms.
func Bootstrap(ctx context.Context, root string, sources []BootstrapSource, opts BootstrapOpts) (*Vocabulary, *BootstrapResult, error) {
	if root == "" {
		return nil, nil, fmt.Errorf("concepts: Bootstrap requires a workspace root")
	}
	if opts.Max <= 0 {
		opts.Max = DefaultBootstrapOpts().Max
	}

	// Run sources in parallel — each one walks an independent slice
	// of the workspace so no shared lock contention. Cancel context
	// short-circuits the slow ones.
	type sourceOut struct {
		name       string
		candidates []Candidate
		err        error
	}
	out := make(chan sourceOut, len(sources))
	var wg sync.WaitGroup
	for _, src := range sources {
		wg.Add(1)
		go func(src BootstrapSource) {
			defer wg.Done()
			cs, err := src.Extract(ctx, root)
			out <- sourceOut{name: src.Name(), candidates: cs, err: err}
		}(src)
	}
	wg.Wait()
	close(out)

	res := &BootstrapResult{}
	v := &Vocabulary{}
	now := time.Now().UTC()

	// Drain in source-order so candidates from the first source land
	// first when the Max cap kicks in. The channel is unordered, so
	// we re-sort by the source slice's order.
	pending := make(map[string][]Candidate, len(sources))
	for s := range out {
		if s.err != nil {
			res.Errors = append(res.Errors, BootstrapError{Source: s.name, Err: s.err})
			continue
		}
		pending[s.name] = s.candidates
	}

	for _, src := range sources {
		cs := pending[src.Name()]
		// Normalize within a source first so a source emitting
		// "foo" and "Foo" doesn't double-spend the cap.
		dedupe := make(map[string]bool)
		for _, c := range cs {
			canon := Normalize(c.Name)
			if canon == "" || dedupe[canon] {
				continue
			}
			dedupe[canon] = true
			if len(v.Terms) >= opts.Max {
				res.Skipped++
				continue
			}
			label := c.Source
			if label == "" {
				label = "bootstrap:" + src.Name()
			}
			v.Add(Term{
				Name:        canon,
				Source:      label,
				Description: c.Description,
				CreatedAt:   now,
				UpdatedAt:   now,
			})
		}
	}

	v.Sort()
	res.Total = len(v.Terms)
	// Bucket count-by-source so the operator can read the report at
	// a glance. Sorted by source name for stable output.
	bySource := make(map[string]int, len(v.Terms))
	for _, t := range v.Terms {
		bySource[t.Source]++
	}
	keys := make([]string, 0, len(bySource))
	for k := range bySource {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		res.BySource = append(res.BySource, BootstrapBucket{Source: k, Count: bySource[k]})
	}
	return v, res, nil
}

// BootstrapResult is the operator-visible report of one bootstrap
// run. Mostly diagnostic; the vocabulary itself is the load-bearing
// output.
type BootstrapResult struct {
	Total    int
	Skipped  int               // candidates dropped because Max was hit
	BySource []BootstrapBucket // count of terms attributed per source
	Errors   []BootstrapError  // per-source failures (other sources still ran)
}

type BootstrapBucket struct {
	Source string
	Count  int
}

type BootstrapError struct {
	Source string
	Err    error
}

func (e BootstrapError) Error() string {
	return e.Source + ": " + e.Err.Error()
}
