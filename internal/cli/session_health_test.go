package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner"
)

func runSessionHealthCmd(t *testing.T, in SessionHealthInput, args []string) string {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	cmd := newSessionHealthCmd()
	cmd.SetIn(bytes.NewReader(body))
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(context.Background())
	if args != nil {
		cmd.SetArgs(args)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, stdout.String())
	}
	return stdout.String()
}

func TestSessionHealthCmd_HeaderRowOnEmptyInput(t *testing.T) {
	body := runSessionHealthCmd(t, SessionHealthInput{}, nil)
	if !strings.Contains(body, "pressure") {
		t.Errorf("expected header line: %s", body)
	}
}

func TestSessionHealthCmd_TimeOnTaskFromSessionAlone(t *testing.T) {
	// No profile → ContextPressure / Drift = 0; TimeOnTask still
	// computable from Session.StartedAt + injected Now.
	in := SessionHealthInput{
		Now: "2026-04-26T13:30:00Z",
		Sessions: []SessionHealthEntry{{
			Session: &core.Session{
				ID:        "sess-1",
				AgentID:   "mike",
				StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC),
			},
		}},
	}
	body := runSessionHealthCmd(t, in, []string{"--json"})
	var env SessionHealthOut
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, body)
	}
	if len(env.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(env.Rows))
	}
	r := env.Rows[0]
	if r.SessionID != "sess-1" {
		t.Errorf("session_id = %q", r.SessionID)
	}
	if r.AgentID != "mike" {
		t.Errorf("agent_id = %q", r.AgentID)
	}
	if r.TimeOnTaskSeconds != 1800 {
		t.Errorf("time_on_task_seconds = %v, want 1800", r.TimeOnTaskSeconds)
	}
	if r.PressureLevel != "ok" {
		t.Errorf("pressure_level = %q, want ok", r.PressureLevel)
	}
}

func TestSessionHealthCmd_PressureThresholds(t *testing.T) {
	cases := []struct {
		name  string
		pct   float64
		level string
	}{
		{"low pressure → ok", 0.30, "ok"},
		{"warn band", 0.65, "warn"},
		{"recycle band", 0.85, "recycle"},
		{"recycle band high", 0.95, "recycle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := SessionHealthInput{
				Now: "2026-04-26T13:00:00Z",
				Sessions: []SessionHealthEntry{{
					Session: &core.Session{ID: "sess-1", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)},
					Profile: &planner.SessionProfile{ContextPct: tc.pct},
				}},
			}
			body := runSessionHealthCmd(t, in, []string{"--json"})
			var env SessionHealthOut
			if err := json.Unmarshal([]byte(body), &env); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if env.Rows[0].PressureLevel != tc.level {
				t.Errorf("pressure %v → %q, want %q", tc.pct, env.Rows[0].PressureLevel, tc.level)
			}
		})
	}
}

func TestSessionHealthCmd_RecycleTagRendersInTextOutput(t *testing.T) {
	in := SessionHealthInput{
		Now: "2026-04-26T13:00:00Z",
		Sessions: []SessionHealthEntry{{
			Session: &core.Session{ID: "sess-stress", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)},
			Profile: &planner.SessionProfile{ContextPct: 0.95},
		}},
	}
	body := runSessionHealthCmd(t, in, nil)
	if !strings.Contains(body, "(RECYCLE)") {
		t.Errorf("expected (RECYCLE) tag in text output: %s", body)
	}
}

func TestSessionHealthCmd_StableOrderAcrossSessionIDs(t *testing.T) {
	in := SessionHealthInput{
		Now: "2026-04-26T13:00:00Z",
		Sessions: []SessionHealthEntry{
			{Session: &core.Session{ID: "sess-c", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)}},
			{Session: &core.Session{ID: "sess-a", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)}},
			{Session: &core.Session{ID: "sess-b", StartedAt: time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)}},
		},
	}
	body := runSessionHealthCmd(t, in, []string{"--json"})
	var env SessionHealthOut
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantOrder := []string{"sess-a", "sess-b", "sess-c"}
	for i, want := range wantOrder {
		if env.Rows[i].SessionID != want {
			t.Errorf("row %d: %q, want %q", i, env.Rows[i].SessionID, want)
		}
	}
}

func TestSessionHealthCmd_InvalidNowReturnsError(t *testing.T) {
	body, err := json.Marshal(SessionHealthInput{Now: "not-a-time"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cmd := newSessionHealthCmd()
	cmd.SetIn(bytes.NewReader(body))
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error on bad 'now', got: %s", stdout.String())
	}
}
