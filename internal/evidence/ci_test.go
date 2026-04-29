package evidence

import (
	"context"
	"errors"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

func TestCICollector_Collect(t *testing.T) {
	runJSON := `[
        {"databaseId":1001,"name":"CI","displayTitle":"feat(evidence): library (gm-e11.6)",
         "conclusion":"success","status":"completed","url":"https://github.com/Mike/gemba/actions/runs/1001",
         "workflowName":"CI","headBranch":"feat/gm-e11.6","headSha":"abc","createdAt":"2026-04-27T10:00:00Z",
         "updatedAt":"2026-04-27T10:05:00Z","event":"push"},
        {"databaseId":1002,"name":"CI","displayTitle":"chore: unrelated",
         "conclusion":"failure","status":"completed","url":"https://github.com/Mike/gemba/actions/runs/1002",
         "workflowName":"CI","headBranch":"main","headSha":"def","createdAt":"2026-04-27T09:00:00Z",
         "updatedAt":"2026-04-27T09:05:00Z","event":"push"},
        {"databaseId":1003,"name":"CI","displayTitle":"branch ref",
         "conclusion":"success","status":"completed","url":"https://github.com/Mike/gemba/actions/runs/1003",
         "workflowName":"CI","headBranch":"GM-E11.6-fix","headSha":"ghi","createdAt":"2026-04-26T09:00:00Z",
         "updatedAt":"2026-04-26T09:05:00Z","event":"pull_request"}
    ]`

	tests := []struct {
		name      string
		target    Target
		runnerOut string
		runnerErr error
		want      int
		wantErr   bool
		check     func(t *testing.T, ev []core.Evidence)
	}{
		{
			name: "filters to runs that mention native id (title or branch, case-insensitive)",
			target: Target{
				ID: "gemba/gemba/gm-e11.6", NativeID: "gm-e11.6",
				GitHubRepo: "Mike/gemba",
			},
			runnerOut: runJSON,
			want:      2, // 1001 (title) and 1003 (branch case-insensitive)
			check: func(t *testing.T, ev []core.Evidence) {
				if ev[0].Kind != core.EvidenceTestResult {
					t.Errorf("kind: %s", ev[0].Kind)
				}
				if ev[0].ID != "ghrun:Mike/gemba#1001" {
					t.Errorf("id: %s", ev[0].ID)
				}
				if ev[0].Payload["conclusion"] != "success" {
					t.Errorf("conclusion: %v", ev[0].Payload["conclusion"])
				}
				if ev[0].Payload[PayloadKeySynthesized] != true {
					t.Errorf("not flagged synthesized")
				}
			},
		},
		{
			name: "no matches",
			target: Target{
				NativeID: "gm-zz.9", GitHubRepo: "Mike/gemba",
			},
			runnerOut: runJSON,
			want:      0,
		},
		{
			name: "missing native id is silent",
			target: Target{
				NativeID: "", GitHubRepo: "Mike/gemba",
			},
			want: 0,
		},
		{
			name: "missing repo is silent",
			target: Target{
				NativeID: "gm-e11.6",
			},
			want: 0,
		},
		{
			name:      "runner error",
			target:    Target{NativeID: "gm-e11.6", GitHubRepo: "Mike/gemba"},
			runnerErr: errors.New("gh boom"),
			wantErr:   true,
		},
		{
			name:      "bad json",
			target:    Target{NativeID: "gm-e11.6", GitHubRepo: "Mike/gemba"},
			runnerOut: "{",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &CICollector{
				Runner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if tt.runnerErr != nil {
						return nil, tt.runnerErr
					}
					if name != "gh" {
						t.Errorf("expected gh, got %s", name)
					}
					return []byte(tt.runnerOut), nil
				},
			}
			ev, err := c.Collect(context.Background(), tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if len(ev) != tt.want {
				t.Fatalf("got %d, want %d (ev=%+v)", len(ev), tt.want, ev)
			}
			if tt.check != nil && len(ev) > 0 {
				tt.check(t, ev)
			}
		})
	}
}

func TestContainsFold(t *testing.T) {
	cases := []struct {
		s, sub string
		want   bool
	}{
		{"feat: gm-e11.6", "gm-e11.6", true},
		{"FEAT: GM-E11.6", "gm-e11.6", true},
		{"feat: gm-e11.6", "GM-E11.6", true},
		{"unrelated", "gm-e11.6", false},
		{"", "x", false},
		{"x", "", true},
		{"short", "longerthanstring", false},
	}
	for _, c := range cases {
		if got := containsFold(c.s, c.sub); got != c.want {
			t.Errorf("containsFold(%q,%q)=%v want %v", c.s, c.sub, got, c.want)
		}
	}
}

func TestCICollector_Source(t *testing.T) {
	if (&CICollector{}).Source() != SourceCI {
		t.Fatal("source mismatch")
	}
}
