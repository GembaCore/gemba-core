package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner"
)

func runOpCtxCmd(t *testing.T, args []string, stdin string) string {
	t.Helper()
	cmd := newOperationalContextCmd()
	if stdin != "" {
		cmd.SetIn(strings.NewReader(stdin))
	}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, stdout.String())
	}
	return stdout.String()
}

func runOpCtxCmdExpectError(t *testing.T, args []string, stdin string) error {
	t.Helper()
	cmd := newOperationalContextCmd()
	if stdin != "" {
		cmd.SetIn(strings.NewReader(stdin))
	}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestOpCtxCmd_FileAndBaseURLMutex(t *testing.T) {
	if err := runOpCtxCmdExpectError(t, []string{"--file", "x.json", "--base-url", "http://x"}, ""); err == nil {
		t.Fatal("expected error when both --file and --base-url given")
	}
}

func TestOpCtxCmd_OfflineAllSessionsRendered(t *testing.T) {
	in := OperationalContextInput{
		Now: "2026-04-26T13:30:00Z",
		Sessions: []*core.Session{
			{ID: "sess-a", AgentID: "alice", AssignmentID: "asg-a", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)},
			{ID: "sess-b", AgentID: "bob", AssignmentID: "asg-b", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)},
		},
		Agents: []*core.AgentRef{
			{ID: "alice", Name: "alice", Kind: "agent", Role: "crew"},
		},
	}
	body, _ := json.Marshal(in)
	out := runOpCtxCmd(t, nil, string(body))
	if !strings.Contains(out, "session: sess-a") || !strings.Contains(out, "session: sess-b") {
		t.Errorf("expected both sessions rendered:\n%s", out)
	}
	if !strings.Contains(out, "agent:         alice") {
		t.Errorf("expected agent line for sess-a:\n%s", out)
	}
}

func TestOpCtxCmd_OfflineSpecificSession(t *testing.T) {
	in := OperationalContextInput{
		Now: "2026-04-26T13:30:00Z",
		Sessions: []*core.Session{
			{ID: "sess-a", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)},
			{ID: "sess-b", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)},
		},
	}
	body, _ := json.Marshal(in)
	out := runOpCtxCmd(t, []string{"--session-id", "sess-a"}, string(body))
	if !strings.Contains(out, "sess-a") {
		t.Errorf("expected sess-a in output:\n%s", out)
	}
	if strings.Contains(out, "session: sess-b") {
		t.Errorf("filtered session-id MUST NOT include other sessions:\n%s", out)
	}
}

func TestOpCtxCmd_OfflineEmitsHealthFromProfile(t *testing.T) {
	in := OperationalContextInput{
		Now: "2026-04-26T13:30:00Z",
		Sessions: []*core.Session{
			{ID: "sess-a", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)},
		},
		Profiles: map[string]*planner.SessionProfile{
			"sess-a": {
				SessionID:  "sess-a",
				ContextPct: 0.42,
				Concepts:   map[planner.ConceptTag]float64{"auth": 0.9, "spa-routing": 0.3},
			},
		},
	}
	body, _ := json.Marshal(in)
	out := runOpCtxCmd(t, nil, string(body))
	if !strings.Contains(out, "pressure:      0.42") {
		t.Errorf("expected pressure line: %s", out)
	}
	if !strings.Contains(out, "auth=0.90") {
		t.Errorf("expected top concepts line: %s", out)
	}
}

func TestOpCtxCmd_OfflineJSONEnvelopeStable(t *testing.T) {
	in := OperationalContextInput{
		Now: "2026-04-26T13:30:00Z",
		Sessions: []*core.Session{
			{ID: "sess-a", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)},
		},
	}
	body, _ := json.Marshal(in)
	out := runOpCtxCmd(t, []string{"--json"}, string(body))
	var env struct {
		Contexts []*planner.OperationalContext `json:"contexts"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if len(env.Contexts) != 1 || env.Contexts[0].Session.ID != "sess-a" {
		t.Errorf("unexpected contexts: %+v", env.Contexts)
	}
}

func TestOpCtxCmd_BaseURLNoSessionIDErrors(t *testing.T) {
	if err := runOpCtxCmdExpectError(t, []string{"--base-url", "http://example"}, ""); err == nil {
		t.Fatal("expected --session-id required error")
	}
}

func TestOpCtxCmd_BaseURLFetchesAndRenders(t *testing.T) {
	now := time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/operational-context" {
			http.NotFound(w, req)
			return
		}
		if req.URL.Query().Get("session_id") != "sess-x" {
			http.Error(w, "wrong id", http.StatusBadRequest)
			return
		}
		ctx := planner.OperationalContext{
			Session:   &core.Session{ID: "sess-x", AgentID: "carol", StartedAt: now},
			Workspace: &core.Workspace{Repository: "gemba", Branch: "main", Kind: core.WorkspaceWorktree},
			Health:    &planner.SessionHealth{ContextPressure: 0.5},
		}
		_ = json.NewEncoder(w).Encode(&ctx)
	}))
	defer srv.Close()

	out := runOpCtxCmd(t, []string{"--base-url", srv.URL, "--session-id", "sess-x"}, "")
	if !strings.Contains(out, "session: sess-x") {
		t.Errorf("expected session line: %s", out)
	}
	if !strings.Contains(out, "gemba/main") {
		t.Errorf("expected workspace line: %s", out)
	}
}

func TestOpCtxCmd_BaseURLPropagatesNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "{\"error\":\"session_not_found\",\"message\":\"nope\"}", http.StatusNotFound)
	}))
	defer srv.Close()

	cmd := newOperationalContextCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--base-url", srv.URL, "--session-id", "sess-missing"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error from non-200 response")
	}
}

// Compile-time check the cobra command type matches what root.go wires.
var _ *cobra.Command = newOperationalContextCmd()
