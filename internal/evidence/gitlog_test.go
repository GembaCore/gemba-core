package evidence

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

func TestGitLogCollector_Collect(t *testing.T) {
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
			name: "two matching commits parsed",
			target: Target{
				ID:       "gemba/gemba/gm-e11.6",
				NativeID: "gm-e11.6",
				RepoPath: "/tmp/repo",
			},
			runnerOut: strings.Join([]string{
				"abc1234deadbeef\t2026-04-27T10:00:00Z\tfeat(evidence): library (gm-e11.6)",
				"def5678abadcafe\t2026-04-27T11:30:00Z\tfix(evidence): nil-safe target gm-e11.6",
			}, "\n"),
			want: 2,
			check: func(t *testing.T, ev []core.Evidence) {
				if ev[0].Kind != core.EvidenceCommit {
					t.Errorf("kind: got %s want commit", ev[0].Kind)
				}
				if ev[0].Ref != "abc1234deadbeef" {
					t.Errorf("ref: got %q", ev[0].Ref)
				}
				if ev[0].Source != "evidence:git_log" {
					t.Errorf("source: got %q", ev[0].Source)
				}
				if ev[0].Payload[PayloadKeySynthesized] != true {
					t.Errorf("payload not flagged synthesized: %#v", ev[0].Payload)
				}
				if ev[0].Payload[PayloadKeySource] != "git_log" {
					t.Errorf("synth_source: got %v", ev[0].Payload[PayloadKeySource])
				}
				if ev[0].CapturedAt.IsZero() {
					t.Errorf("CapturedAt unparsed")
				}
			},
		},
		{
			name: "empty output returns nil",
			target: Target{
				ID: "x/y/z", NativeID: "z", RepoPath: "/tmp/repo",
			},
			runnerOut: "",
			want:      0,
		},
		{
			name: "no native id is silent",
			target: Target{
				ID: "x/y/z", NativeID: "", RepoPath: "/tmp/repo",
			},
			want: 0,
		},
		{
			name: "no repo path is silent",
			target: Target{
				ID: "x/y/z", NativeID: "z", RepoPath: "",
			},
			want: 0,
		},
		{
			name: "runner error propagates",
			target: Target{
				ID: "x/y/z", NativeID: "z", RepoPath: "/tmp/repo",
			},
			runnerErr: errors.New("git missing"),
			wantErr:   true,
		},
		{
			name: "malformed line skipped",
			target: Target{
				ID: "x/y/z", NativeID: "z", RepoPath: "/tmp/repo",
			},
			runnerOut: "no-tabs-here\nabc\t2026-04-27T10:00:00Z\tok",
			want:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &GitLogCollector{
				Runner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if tt.runnerErr != nil {
						return nil, tt.runnerErr
					}
					if name != "git" {
						t.Errorf("expected git, got %s", name)
					}
					// First arg should be -C <repoPath> when RepoPath set.
					if tt.target.RepoPath != "" {
						if len(args) < 2 || args[0] != "-C" || args[1] != tt.target.RepoPath {
							t.Errorf("expected -C %s prefix, got %v", tt.target.RepoPath, args)
						}
					}
					return []byte(tt.runnerOut), nil
				},
			}
			ev, err := c.Collect(context.Background(), tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if len(ev) != tt.want {
				t.Fatalf("got %d evidence, want %d (ev=%+v)", len(ev), tt.want, ev)
			}
			if tt.check != nil && len(ev) > 0 {
				tt.check(t, ev)
			}
		})
	}
}

func TestGitLogCollector_Source(t *testing.T) {
	if (&GitLogCollector{}).Source() != SourceGitLog {
		t.Fatal("source mismatch")
	}
}
