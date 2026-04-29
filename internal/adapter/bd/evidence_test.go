package bd_test

import (
	"context"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/bd"
	"github.com/GembaCore/gemba-core/internal/evidence"
)

// stubCollector returns a fixed Evidence slice when Collect is invoked.
// Used to drive bd.WorkPlane's synthesis path without shelling out.
type stubCollector struct {
	src evidence.Source
	ev  []core.Evidence
}

func (s *stubCollector) Source() evidence.Source { return s.src }
func (s *stubCollector) Collect(_ context.Context, _ evidence.Target) ([]core.Evidence, error) {
	return s.ev, nil
}

// TestGetWorkItem_MergesSynthesizedEvidence verifies that GetWorkItem
// runs the configured synthesizer and that returned entries land on
// wi.Evidence with the canonical synthesized=true payload key. gm-e6.5.
func TestGetWorkItem_MergesSynthesizedEvidence(t *testing.T) {
	fake := newFakeBd(t)
	fake.beads["gm-syn"] = &bd.Bead{
		ID:        "gm-syn",
		Title:     "subject",
		Status:    "open",
		Priority:  2,
		IssueType: "task",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	prURL := "https://github.com/example/repo/pull/42"
	commitSHA := "abc123def456"
	commitEv := core.Evidence{
		ID:      "gitlog:" + commitSHA,
		Kind:    core.EvidenceCommit,
		Source:  "evidence:git_log",
		Ref:     commitSHA,
		Summary: "feat: implement gm-syn",
		Payload: map[string]any{
			evidence.PayloadKeySynthesized: true,
			evidence.PayloadKeySource:      string(evidence.SourceGitLog),
		},
	}
	prEv := core.Evidence{
		ID:      "ghpr:example/repo#42",
		Kind:    core.EvidenceURL,
		Source:  "evidence:github_pr",
		Ref:     prURL,
		Summary: "PR #42",
		Payload: map[string]any{
			evidence.PayloadKeySynthesized: true,
			evidence.PayloadKeySource:      string(evidence.SourceGitHubPR),
		},
	}

	synth := evidence.NewSynthesizerWith(
		&stubCollector{src: evidence.SourceGitLog, ev: []core.Evidence{commitEv}},
		&stubCollector{src: evidence.SourceGitHubPR, ev: []core.Evidence{prEv}},
	)
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")
	impl.SetSynthesizer(synth, "/tmp/repo", "example/repo")

	wi, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-syn"))
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if len(wi.Evidence) != 2 {
		t.Fatalf("len(Evidence)=%d want 2: %+v", len(wi.Evidence), wi.Evidence)
	}
	for _, e := range wi.Evidence {
		v, ok := e.Payload[evidence.PayloadKeySynthesized].(bool)
		if !ok || !v {
			t.Errorf("Evidence %q missing synthesized=true: payload=%v", e.ID, e.Payload)
		}
	}
	// Refs preserved verbatim through the merge.
	gotRefs := map[string]bool{wi.Evidence[0].Ref: true, wi.Evidence[1].Ref: true}
	if !gotRefs[commitSHA] || !gotRefs[prURL] {
		t.Errorf("Evidence refs=%v want both %q and %q", gotRefs, commitSHA, prURL)
	}
}

// TestGetWorkItem_NativeEvidenceLabelsKeepUnsynthesized covers the
// distinction the bead specifies: native evidence:* labels survive
// onto wi.Evidence WITHOUT the synthesized payload key.
func TestGetWorkItem_NativeEvidenceLabelsKeepUnsynthesized(t *testing.T) {
	fake := newFakeBd(t)
	fake.beads["gm-native-ev"] = &bd.Bead{
		ID:        "gm-native-ev",
		Title:     "native",
		Status:    "open",
		Priority:  2,
		IssueType: "task",
		Labels: []string{
			"evidence:url:https://example.com/dashboard",
			"evidence:commit:deadbeef",
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	wi, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-native-ev"))
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if len(wi.Evidence) != 2 {
		t.Fatalf("len(Evidence)=%d want 2: %+v", len(wi.Evidence), wi.Evidence)
	}
	for _, e := range wi.Evidence {
		if _, ok := e.Payload[evidence.PayloadKeySynthesized]; ok {
			t.Errorf("native Evidence %q must not carry synthesized payload key: %v",
				e.ID, e.Payload)
		}
		if e.Source != "beads:label" {
			t.Errorf("native Evidence %q: Source=%q want beads:label", e.ID, e.Source)
		}
	}
}

// TestGetWorkItem_SynthesizerFailureDoesNotFailCall asserts the
// best-effort contract: a synthesizer total-failure is logged, GetWorkItem
// still returns the bead with whatever native Evidence it had.
func TestGetWorkItem_SynthesizerFailureDoesNotFailCall(t *testing.T) {
	fake := newFakeBd(t)
	fake.beads["gm-fail"] = &bd.Bead{
		ID: "gm-fail", Title: "t", Status: "open", Priority: 2, IssueType: "task",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	synth := evidence.NewSynthesizerWith(&failingCollector{src: evidence.SourceGitLog})
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")
	impl.SetSynthesizer(synth, "/tmp/repo", "")

	wi, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-fail"))
	if err != nil {
		t.Fatalf("GetWorkItem: %v (synth failure must not fail GetWorkItem)", err)
	}
	if len(wi.Evidence) != 0 {
		t.Errorf("Evidence=%v want empty when only collector failed", wi.Evidence)
	}
}

type failingCollector struct{ src evidence.Source }

func (f *failingCollector) Source() evidence.Source { return f.src }
func (f *failingCollector) Collect(_ context.Context, _ evidence.Target) ([]core.Evidence, error) {
	return nil, errBoom
}

var errBoom = &boomErr{}

type boomErr struct{}

func (*boomErr) Error() string { return "boom" }

// TestEvidenceFromLabels covers the unit-level parser used to project
// native bd `evidence:*` labels onto core.Evidence.
func TestEvidenceFromLabels(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   int
		check  func(t *testing.T, ev []core.Evidence)
	}{
		{
			name:   "url and commit parsed",
			labels: []string{"evidence:url:https://x.test/y", "evidence:commit:abc", "area:core"},
			want:   2,
			check: func(t *testing.T, ev []core.Evidence) {
				if ev[0].Kind != core.EvidenceURL || ev[0].Ref != "https://x.test/y" {
					t.Errorf("ev[0]=%+v", ev[0])
				}
				if ev[1].Kind != core.EvidenceCommit || ev[1].Ref != "abc" {
					t.Errorf("ev[1]=%+v", ev[1])
				}
			},
		},
		{
			name:   "unknown kind becomes custom",
			labels: []string{"evidence:trace:span-42"},
			want:   1,
			check: func(t *testing.T, ev []core.Evidence) {
				if ev[0].Kind != core.EvidenceCustom {
					t.Errorf("Kind=%q want custom", ev[0].Kind)
				}
			},
		},
		{
			name:   "malformed labels are skipped",
			labels: []string{"evidence:", "evidence:url:", "evidence:url"},
			want:   0,
		},
		{
			name:   "no evidence labels yields nil",
			labels: []string{"area:core", "tier:sonnet"},
			want:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeBd(t)
			fake.beads["gm-l"] = &bd.Bead{
				ID: "gm-l", Title: "t", Status: "open", Priority: 2, IssueType: "task",
				Labels:    tc.labels,
				CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			}
			impl := bd.NewWorkPlaneWithRunner(fake.run, "")
			wi, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-l"))
			if err != nil {
				t.Fatalf("GetWorkItem: %v", err)
			}
			if len(wi.Evidence) != tc.want {
				t.Fatalf("len(Evidence)=%d want %d (got %+v)", len(wi.Evidence), tc.want, wi.Evidence)
			}
			if tc.check != nil {
				tc.check(t, wi.Evidence)
			}
		})
	}
}
