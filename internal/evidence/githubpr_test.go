package evidence

import (
	"context"
	"errors"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestGitHubPRCollector_Collect(t *testing.T) {
	prJSON := `[
        {"number":42,"title":"Implement evidence library (gm-e11.6)","url":"https://github.com/Mike/gemba/pull/42",
         "state":"OPEN","author":{"login":"mike"},"headRefName":"feat/gm-e11.6","baseRefName":"main",
         "createdAt":"2026-04-27T09:00:00Z","updatedAt":"2026-04-27T10:00:00Z","isDraft":false},
        {"number":43,"title":"Followup gm-e11.6","url":"https://github.com/Mike/gemba/pull/43",
         "state":"MERGED","author":{"login":"alice"},"headRefName":"fix/x","baseRefName":"main",
         "createdAt":"2026-04-26T09:00:00Z","updatedAt":"2026-04-27T08:00:00Z","isDraft":false}
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
			name: "two PRs decoded",
			target: Target{
				ID: "gemba/gemba/gm-e11.6", NativeID: "gm-e11.6",
				GitHubRepo: "Mike/gemba",
			},
			runnerOut: prJSON,
			want:      2,
			check: func(t *testing.T, ev []core.Evidence) {
				if ev[0].Kind != core.EvidenceURL {
					t.Errorf("kind: %s", ev[0].Kind)
				}
				if ev[0].Ref != "https://github.com/Mike/gemba/pull/42" {
					t.Errorf("ref: %s", ev[0].Ref)
				}
				if ev[0].ID != "ghpr:Mike/gemba#42" {
					t.Errorf("id: %s", ev[0].ID)
				}
				if ev[0].Payload["state"] != "OPEN" {
					t.Errorf("state: %v", ev[0].Payload["state"])
				}
				if ev[0].Payload[PayloadKeySynthesized] != true {
					t.Errorf("not flagged synthesized")
				}
			},
		},
		{
			name: "empty list",
			target: Target{
				NativeID: "gm-e11.6", GitHubRepo: "Mike/gemba",
			},
			runnerOut: "[]",
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
			name: "missing github repo is silent",
			target: Target{
				NativeID: "gm-e11.6", GitHubRepo: "",
			},
			want: 0,
		},
		{
			name: "runner error",
			target: Target{
				NativeID: "gm-e11.6", GitHubRepo: "Mike/gemba",
			},
			runnerErr: errors.New("gh not installed"),
			wantErr:   true,
		},
		{
			name: "bad json",
			target: Target{
				NativeID: "gm-e11.6", GitHubRepo: "Mike/gemba",
			},
			runnerOut: "not json",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &GitHubPRCollector{
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
				t.Fatalf("got %d, want %d", len(ev), tt.want)
			}
			if tt.check != nil && len(ev) > 0 {
				tt.check(t, ev)
			}
		})
	}
}

func TestGitHubPRCollector_Source(t *testing.T) {
	if (&GitHubPRCollector{}).Source() != SourceGitHubPR {
		t.Fatal("source mismatch")
	}
}
