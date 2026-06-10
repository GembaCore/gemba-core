// Tests for the target-overlap algorithm (gm-s47n.4.1).
//
// Table-driven across the three result shapes the algorithm can
// emit (NoOverlap / Overlap / Maybe) and across the three precision
// layers (exact, prefix tree, complex). The Maybe cases double as a
// smoke test for the safety-net FS path via the fakeFS below.

package targets

import (
	"errors"
	"reflect"
	"sort"
	"testing"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b Pattern
		want Result
	}{
		// ── EXACT layer ────────────────────────────────────────────
		{"literal-equal", "src/foo.go", "src/foo.go", Overlap},
		{"literal-different", "src/foo.go", "src/bar.go", NoOverlap},
		{"literal-different-dirs", "src/foo.go", "lib/foo.go", NoOverlap},
		{"literal-substring-not-prefix", "src/foo.go", "src/foo.go.bak", NoOverlap},

		// ── PREFIX TREE layer ──────────────────────────────────────
		{"prefix-glob-vs-literal-inside", "src/**", "src/foo.go", Overlap},
		{"prefix-glob-vs-literal-outside", "src/**", "lib/foo.go", NoOverlap},
		{"prefix-glob-vs-literal-deep", "src/**", "src/a/b/c/d.go", Overlap},
		{"prefix-glob-vs-prefix-glob-equal", "src/**", "src/**", Overlap},
		{"prefix-glob-vs-prefix-glob-nested", "src/**", "src/foo/**", Overlap},
		{"prefix-glob-vs-prefix-glob-disjoint", "src/**", "lib/**", NoOverlap},
		{"prefix-glob-only-overlapping-name", "src/**", "src-other/**", NoOverlap},
		{"global-glob-vs-literal", "**", "anything/at/all.go", Overlap},
		{"global-glob-vs-prefix", "**", "src/**", Overlap},
		{"global-glob-vs-global-glob", "**", "**", Overlap},

		// ── COMPLEX vs LITERAL (decided exactly) ───────────────────
		{"single-wildcard-vs-literal-match", "src/*.go", "src/foo.go", Overlap},
		{"single-wildcard-vs-literal-no-match-ext", "src/*.go", "src/foo.ts", NoOverlap},
		{"single-wildcard-vs-literal-no-match-dir", "src/*.go", "lib/foo.go", NoOverlap},
		{"single-wildcard-vs-literal-deeper-no", "src/*.go", "src/a/foo.go", NoOverlap},
		{"question-mark-vs-literal-match", "src/?ar.go", "src/bar.go", Overlap},
		{"question-mark-vs-literal-no-match", "src/?ar.go", "src/baar.go", NoOverlap},
		{"recursive-suffix-vs-matching-literal", "src/**/foo.go", "src/a/b/foo.go", Overlap},
		{"recursive-suffix-vs-non-matching-literal", "src/**/foo.go", "src/a/bar.go", NoOverlap},
		{"recursive-suffix-vs-zero-segments", "src/**/foo.go", "src/foo.go", Overlap},

		// ── PREFIX-GLOB vs COMPLEX (early-exit by prefix mismatch) ─
		{"prefix-glob-vs-complex-disjoint-roots", "src/**", "lib/*.go", NoOverlap},
		{"prefix-glob-vs-complex-shared-root-needs-fs", "src/**", "src/*.go", Maybe},
		{"prefix-glob-deep-vs-complex-needs-fs", "src/foo/**", "src/foo/*.go", Maybe},

		// ── COMPLEX vs COMPLEX (always Maybe without FS) ───────────
		{"complex-vs-complex-disjoint-extensions-still-maybe", "src/*.go", "src/*.ts", Maybe},
		{"complex-vs-complex-recursive-suffix", "**/foo.go", "lib/**/bar.go", Maybe},

		// ── Normalisation ──────────────────────────────────────────
		{"leading-slash-stripped", "/src/foo.go", "src/foo.go", Overlap},
		{"dot-slash-stripped", "./src/foo.go", "src/foo.go", Overlap},
		{"double-slash-collapsed", "src//foo.go", "src/foo.go", Overlap},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.want {
				t.Errorf("Compare(%q, %q) = %v; want %v", tt.a, tt.b, got, tt.want)
			}
			// Result is symmetric — assert that to catch ordering bugs
			// in compareParsed's swap.
			if got := Compare(tt.b, tt.a); got != tt.want {
				t.Errorf("Compare(%q, %q) [swapped] = %v; want %v", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

func TestCompareSets(t *testing.T) {
	tests := []struct {
		name string
		a, b []Pattern
		want Result
	}{
		{
			name: "empty sets are NoOverlap",
			a:    nil,
			b:    nil,
			want: NoOverlap,
		},
		{
			name: "any pair-overlap propagates",
			a:    []Pattern{"src/foo.go", "lib/bar.go"},
			b:    []Pattern{"docs/x.md", "lib/bar.go"},
			want: Overlap,
		},
		{
			name: "all-pair NoOverlap is NoOverlap",
			a:    []Pattern{"src/foo.go", "src/bar.go"},
			b:    []Pattern{"docs/x.md", "lib/baz.go"},
			want: NoOverlap,
		},
		{
			name: "single Maybe with no Overlap propagates as Maybe",
			a:    []Pattern{"src/*.go"},
			b:    []Pattern{"src/*.ts"},
			want: Maybe,
		},
		{
			name: "Overlap beats Maybe (short-circuit)",
			a:    []Pattern{"src/foo.go", "src/*.go"},
			b:    []Pattern{"src/foo.go", "src/*.ts"},
			want: Overlap,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareSets(tt.a, tt.b); got != tt.want {
				t.Errorf("CompareSets(%v, %v) = %v; want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ── Filesystem safety-net path ─────────────────────────────────────

// fakeFS expands patterns by table lookup. Lets the safety-net tests
// exercise CompareWith without a real working tree.
type fakeFS map[Pattern][]string

func (f fakeFS) Glob(pattern Pattern) ([]string, error) {
	if list, ok := f[pattern]; ok {
		return list, nil
	}
	return nil, nil
}

type errFS struct{ err error }

func (f errFS) Glob(_ Pattern) ([]string, error) { return nil, f.err }

func TestCompareWith_ResolvesMaybe(t *testing.T) {
	// Two complex patterns that the pure analysis returns Maybe for;
	// fs has them sharing a concrete file → Overlap.
	fs := fakeFS{
		"src/*.go":   {"src/a.go", "src/b.go", "src/share.go"},
		"src/sh*.go": {"src/share.go"},
	}
	got, err := CompareWith("src/*.go", "src/sh*.go", fs)
	if err != nil {
		t.Fatalf("CompareWith error: %v", err)
	}
	if got != Overlap {
		t.Errorf("got %v; want Overlap (intersection on src/share.go)", got)
	}
}

func TestCompareWith_NoOverlapWhenDisjoint(t *testing.T) {
	fs := fakeFS{
		"src/*.go": {"src/a.go", "src/b.go"},
		"src/*.ts": {"src/c.ts", "src/d.ts"},
	}
	got, err := CompareWith("src/*.go", "src/*.ts", fs)
	if err != nil {
		t.Fatalf("CompareWith error: %v", err)
	}
	if got != NoOverlap {
		t.Errorf("got %v; want NoOverlap (disjoint expansions)", got)
	}
}

func TestCompareWith_NilFsLeavesMaybe(t *testing.T) {
	got, err := CompareWith("src/*.go", "src/*.ts", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != Maybe {
		t.Errorf("got %v; want Maybe (nil fs cannot resolve)", got)
	}
}

func TestCompareWith_PassesThroughDeterministic(t *testing.T) {
	// FS should never be consulted when the pure analysis already
	// returns a deterministic answer. Use an errFS to detect.
	fs := errFS{err: errors.New("should not be called")}
	got, err := CompareWith("src/foo.go", "src/foo.go", fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != Overlap {
		t.Errorf("got %v; want Overlap from exact-match short-circuit", got)
	}
}

func TestCompareWith_PropagatesFSError(t *testing.T) {
	fs := errFS{err: errors.New("disk on fire")}
	_, err := CompareWith("src/*.go", "src/*.ts", fs)
	if err == nil {
		t.Fatal("expected error from fs.Glob; got nil")
	}
}

func TestCompareSetsWith_ShortCircuitsOnOverlap(t *testing.T) {
	// First pair returns Maybe → resolved via fs. fs makes them
	// overlap. The second pair never runs (no Glob entry needed).
	calls := []Pattern{}
	fs := callRecorderFS{
		entries: fakeFS{
			"src/sh*.go": {"src/share.go"},
			"src/*.go":   {"src/a.go", "src/share.go"},
		},
		recorded: &calls,
	}
	got, err := CompareSetsWith(
		[]Pattern{"src/*.go", "lib/*.go"},
		[]Pattern{"src/sh*.go", "lib/*.ts"},
		fs,
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != Overlap {
		t.Fatalf("got %v; want Overlap", got)
	}
	// Both globs from the first pair were consulted; the second pair
	// (lib/*.go vs lib/*.ts) was short-circuited.
	want := []Pattern{"src/*.go", "src/sh*.go"}
	sort.Slice(calls, func(i, j int) bool { return calls[i] < calls[j] })
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("fs.Glob calls = %v; want exactly %v (short-circuit)", calls, want)
	}
}

type callRecorderFS struct {
	entries  fakeFS
	recorded *[]Pattern
}

func (c callRecorderFS) Glob(pattern Pattern) ([]string, error) {
	*c.recorded = append(*c.recorded, pattern)
	return c.entries.Glob(pattern)
}

// ── Internal-coverage smoke tests ──────────────────────────────────

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"*.go", "foo.go", true},
		{"*.go", "foo.ts", false},
		{"src/*.go", "src/foo.go", true},
		{"src/*.go", "src/sub/foo.go", false},
		{"src/**", "src", true},
		{"src/**", "src/x/y/z.go", true},
		{"src/**/foo.go", "src/foo.go", true},
		{"src/**/foo.go", "src/a/b/foo.go", true},
		{"src/**/foo.go", "src/a/b/bar.go", false},
		{"**/foo.go", "foo.go", true},
		{"**/foo.go", "a/b/foo.go", true},
		{"src/?ar.go", "src/bar.go", true},
		{"src/?ar.go", "src/baar.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"->"+tt.name, func(t *testing.T) {
			if got := matchPattern(tt.pattern, tt.name); got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v; want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		in     Pattern
		kind   patternKind
		prefix string
	}{
		{"src/foo.go", kindLiteral, ""},
		{"src/**", kindPrefixGlob, "src"},
		{"**", kindPrefixGlob, ""},
		{"src/*.go", kindComplex, ""},
		{"src/**/foo.go", kindComplex, ""},
		{"src/foo/**/bar.go", kindComplex, ""},
		{"/src/foo.go", kindLiteral, ""}, // normalisation strips leading /
	}
	for _, tt := range tests {
		t.Run(string(tt.in), func(t *testing.T) {
			got := parse(tt.in)
			if got.kind != tt.kind {
				t.Errorf("kind = %v; want %v", got.kind, tt.kind)
			}
			if got.prefix != tt.prefix {
				t.Errorf("prefix = %q; want %q", got.prefix, tt.prefix)
			}
		})
	}
}
