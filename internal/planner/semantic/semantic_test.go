// Tests for the semantic-conflict detector (gm-s47n.4.2).
//
// Stubs out SourceAnalysis with a small map-backed fake so each
// scenario reads as a table: which file depends on which → expected
// detector verdict. The real GitNexus binding is exercised in
// internal/sourceanalysis; here we pin the dependents-vs-targets
// intersection logic + the graceful-degradation path.

package semantic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner/conflicts"
	"github.com/MikeBengtson/gemba/internal/planner/targets"
	"github.com/MikeBengtson/gemba/internal/sourceanalysis"
)

// fakeAnalysis maps a file path to the set of files that depend on
// it. Repository is enforced: queries with the wrong repo return
// nil. Calling Describe / Dependencies / PublicContractChanges in
// these tests is unexpected — we only exercise Dependents.
type fakeAnalysis struct {
	repo string
	// dependents[file] = files that depend on file.
	dependents map[string][]string
	// err, when set, is returned by every Dependents call.
	err error
}

func (f *fakeAnalysis) Dependents(_ context.Context, t sourceanalysis.Target) ([]sourceanalysis.Target, error) {
	if f.err != nil {
		return nil, f.err
	}
	if t.Repository != f.repo {
		return nil, nil
	}
	deps := f.dependents[t.Path]
	out := make([]sourceanalysis.Target, len(deps))
	for i, d := range deps {
		out[i] = sourceanalysis.Target{Repository: f.repo, Path: d}
	}
	return out, nil
}

func (f *fakeAnalysis) Dependencies(_ context.Context, _ sourceanalysis.Target) ([]sourceanalysis.Target, error) {
	return nil, nil
}

func (f *fakeAnalysis) PublicContractChanges(_ context.Context, _ sourceanalysis.Diff) ([]sourceanalysis.Symbol, error) {
	return nil, nil
}

func (f *fakeAnalysis) Describe(_ context.Context) (sourceanalysis.Capabilities, error) {
	return sourceanalysis.Capabilities{Backend: "fake", Available: true}, nil
}

func bead(id core.WorkItemID, ts ...targets.Pattern) conflicts.Bead {
	return conflicts.Bead{ID: id, Targets: ts}
}

// ── basic intersection direction ───────────────────────────────────

func TestDetect_AModifiesFileBDependsOn(t *testing.T) {
	// auth.go is depended on by handlers.go. A modifies auth.go;
	// B modifies handlers.go. → conflict (A's changes can break B).
	d := &Detector{
		Source: &fakeAnalysis{
			repo: "gemba",
			dependents: map[string][]string{
				"src/auth.go": {"src/handlers.go"},
			},
		},
		Repository: "gemba",
	}
	hit, evidence, err := d.Detect(context.Background(),
		bead("gm-1", "src/auth.go"),
		bead("gm-2", "src/handlers.go"),
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !hit {
		t.Fatal("expected semantic conflict; got false")
	}
	if !strings.Contains(evidence, "src/auth.go") || !strings.Contains(evidence, "src/handlers.go") {
		t.Errorf("evidence missing file names: %q", evidence)
	}
}

func TestDetect_SymmetricDirection(t *testing.T) {
	// Same scenario as above with the bead order swapped — should
	// still detect (the detector checks both directions).
	d := &Detector{
		Source: &fakeAnalysis{
			repo: "gemba",
			dependents: map[string][]string{
				"src/auth.go": {"src/handlers.go"},
			},
		},
		Repository: "gemba",
	}
	hit, _, err := d.Detect(context.Background(),
		bead("gm-2", "src/handlers.go"),
		bead("gm-1", "src/auth.go"),
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !hit {
		t.Fatal("expected detection regardless of input order")
	}
}

func TestDetect_NoSharedDependentEdge(t *testing.T) {
	d := &Detector{
		Source: &fakeAnalysis{
			repo: "gemba",
			dependents: map[string][]string{
				"src/foo.go": {"src/bar.go"},
				"src/baz.go": {"src/qux.go"},
			},
		},
		Repository: "gemba",
	}
	hit, _, err := d.Detect(context.Background(),
		bead("gm-1", "src/foo.go"),
		bead("gm-2", "src/baz.go"),
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if hit {
		t.Error("expected no conflict (their dependents don't intersect targets)")
	}
}

// ── graceful degradation ───────────────────────────────────────────

func TestDetect_ErrUnavailableSwallowedAsNoConflict(t *testing.T) {
	d := &Detector{
		Source:     &fakeAnalysis{err: sourceanalysis.ErrUnavailable},
		Repository: "gemba",
	}
	hit, _, err := d.Detect(context.Background(),
		bead("gm-1", "src/auth.go"),
		bead("gm-2", "src/handlers.go"),
	)
	if err != nil {
		t.Fatalf("ErrUnavailable must be swallowed; got %v", err)
	}
	if hit {
		t.Error("ErrUnavailable should yield no conflict (graceful degrade)")
	}
}

func TestDetect_OtherErrorPropagates(t *testing.T) {
	d := &Detector{
		Source:     &fakeAnalysis{err: errors.New("source service down")},
		Repository: "gemba",
	}
	_, _, err := d.Detect(context.Background(),
		bead("gm-1", "src/auth.go"),
		bead("gm-2", "src/handlers.go"),
	)
	if err == nil {
		t.Fatal("non-ErrUnavailable error must propagate")
	}
}

// ── edge cases ─────────────────────────────────────────────────────

func TestDetect_NilDetectorReturnsNoConflict(t *testing.T) {
	var d *Detector
	hit, _, err := d.Detect(context.Background(),
		bead("gm-1", "src/auth.go"),
		bead("gm-2", "src/handlers.go"),
	)
	if err != nil || hit {
		t.Errorf("nil detector should be a quiet no-op; got hit=%v err=%v", hit, err)
	}
}

func TestDetect_EmptyTargetsNoConflict(t *testing.T) {
	d := &Detector{
		Source:     &fakeAnalysis{repo: "gemba"},
		Repository: "gemba",
	}
	hit, _, err := d.Detect(context.Background(),
		bead("gm-1"),
		bead("gm-2", "src/handlers.go"),
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if hit {
		t.Error("a bead with no targets cannot conflict semantically")
	}
}

func TestDetect_SelfReferenceIgnored(t *testing.T) {
	// Some backends include the queried file in its own Dependents
	// list. The detector must not interpret that as a conflict
	// between a bead and itself.
	d := &Detector{
		Source: &fakeAnalysis{
			repo: "gemba",
			dependents: map[string][]string{
				"src/foo.go": {"src/foo.go"}, // self-reference
			},
		},
		Repository: "gemba",
	}
	hit, _, err := d.Detect(context.Background(),
		bead("gm-1", "src/foo.go"),
		bead("gm-2", "src/foo.go"),
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if hit {
		t.Error("self-reference dependents must not flag a semantic conflict")
	}
}

func TestDetect_RepositoryScopeEnforced(t *testing.T) {
	// fakeAnalysis only emits dependents for repo="gemba". A
	// detector with a different repo configured should see no
	// dependents and report no conflict.
	d := &Detector{
		Source: &fakeAnalysis{
			repo: "gemba",
			dependents: map[string][]string{
				"src/auth.go": {"src/handlers.go"},
			},
		},
		Repository: "elsewhere",
	}
	hit, _, err := d.Detect(context.Background(),
		bead("gm-1", "src/auth.go"),
		bead("gm-2", "src/handlers.go"),
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if hit {
		t.Error("Detector.Repository must scope queries; cross-repo edges are not real")
	}
}

// ── glob expansion via FS ──────────────────────────────────────────

type fakeFS map[targets.Pattern][]string

func (f fakeFS) Glob(p targets.Pattern) ([]string, error) {
	if v, ok := f[p]; ok {
		return v, nil
	}
	return nil, nil
}

func TestDetect_GlobTargetsResolvedViaFS(t *testing.T) {
	// A's target is a glob; FS expands to src/auth.go which is
	// depended on by B's literal target.
	d := &Detector{
		Source: &fakeAnalysis{
			repo: "gemba",
			dependents: map[string][]string{
				"src/auth.go": {"src/handlers.go"},
			},
		},
		Repository: "gemba",
		FS: fakeFS{
			"src/au*.go": {"src/auth.go"},
		},
	}
	hit, _, err := d.Detect(context.Background(),
		bead("gm-1", "src/au*.go"),
		bead("gm-2", "src/handlers.go"),
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !hit {
		t.Error("expected semantic conflict via FS-resolved glob")
	}
}

func TestDetect_GlobTargetsSkippedWithoutFS(t *testing.T) {
	// Same scenario, no FS — the glob target is silently dropped
	// (cant query Dependents on a glob), so no conflict surfaces.
	// Target-overlap detection in the conflicts package is the
	// safety net here; the semantic detector's job is to under-
	// report when its inputs are imprecise.
	d := &Detector{
		Source: &fakeAnalysis{
			repo: "gemba",
			dependents: map[string][]string{
				"src/auth.go": {"src/handlers.go"},
			},
		},
		Repository: "gemba",
	}
	hit, _, err := d.Detect(context.Background(),
		bead("gm-1", "src/au*.go"),
		bead("gm-2", "src/handlers.go"),
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if hit {
		t.Error("non-literal target without FS must not produce a conflict")
	}
}

func TestDetect_PathNormalisation(t *testing.T) {
	// "./src/auth.go" and "src/auth.go" should resolve to the same
	// dependents lookup. Conservative: rely on the detector's
	// normalisation rather than asking SourceAnalysis to be
	// path-equivalence-aware.
	d := &Detector{
		Source: &fakeAnalysis{
			repo: "gemba",
			dependents: map[string][]string{
				"src/auth.go": {"src/handlers.go"},
			},
		},
		Repository: "gemba",
	}
	hit, _, err := d.Detect(context.Background(),
		bead("gm-1", "./src/auth.go"),
		bead("gm-2", "src/handlers.go"),
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !hit {
		t.Error("path normalisation should match './src/...' to 'src/...'")
	}
}

// ── integration with conflicts.Conflicts ───────────────────────────

func TestConflictsIntegration_SemanticOnlyEdge(t *testing.T) {
	// Disjoint targets so target-overlap doesn't fire; the only
	// reason for the edge is the semantic detector.
	d := &Detector{
		Source: &fakeAnalysis{
			repo: "gemba",
			dependents: map[string][]string{
				"src/auth.go": {"src/handlers.go"},
			},
		},
		Repository: "gemba",
	}
	g, err := conflicts.Conflicts(context.Background(),
		[]conflicts.Bead{
			bead("gm-1", "src/auth.go"),
			bead("gm-2", "src/handlers.go"),
		},
		conflicts.Options{Semantic: d},
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge; got %d", len(g.Edges))
	}
	if len(g.Edges[0].Reasons) != 1 || g.Edges[0].Reasons[0].Kind != conflicts.ReasonSemantic {
		t.Errorf("expected single semantic reason; got %+v", g.Edges[0].Reasons)
	}
}
