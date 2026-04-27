package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func runSessionStatus_helper(t *testing.T, body string, asJSON bool) string {
	t.Helper()
	cmd := newSessionStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(body))
	cmd.SetContext(context.Background())
	args := []string{}
	if asJSON {
		args = append(args, "--json")
	}
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
	return out.String()
}

const fixedNowRFC3339 = "2026-04-26T19:00:00Z"

func sessionStatusBody(t *testing.T, body string) string {
	t.Helper()
	return strings.ReplaceAll(body, "{NOW}", fixedNowRFC3339)
}

// ── Happy path ──────────────────────────────────────────────────

func TestSessionStatus_HealthySessionRendersLargeRunway(t *testing.T) {
	body := sessionStatusBody(t, `{
		"sessions": [{
			"session": {
				"id": "sess-healthy",
				"agent_id": "alice",
				"status": "working",
				"started_at": "2026-04-26T18:30:00Z"
			},
			"profile": {
				"session_id": "sess-healthy",
				"context_pct": 0.2
			}
		}],
		"now": "{NOW}"
	}`)
	out := runSessionStatus_helper(t, body, false)
	if !strings.Contains(out, "sess-healthy") {
		t.Errorf("missing session id: %q", out)
	}
	if !strings.Contains(out, "large") {
		t.Errorf("expected large runway in output: %q", out)
	}
}

func TestSessionStatus_StressedSessionRendersSmallRunway(t *testing.T) {
	body := sessionStatusBody(t, `{
		"sessions": [{
			"session": {
				"id": "sess-stressed",
				"agent_id": "bob",
				"status": "working",
				"started_at": "2026-04-26T15:00:00Z"
			},
			"profile": {
				"session_id": "sess-stressed",
				"context_pct": 0.92
			}
		}],
		"now": "{NOW}"
	}`)
	out := runSessionStatus_helper(t, body, false)
	if !strings.Contains(out, "small") {
		t.Errorf("expected small runway: %q", out)
	}
	if !strings.Contains(out, "(RECYCLE)") {
		t.Errorf("expected recycle tag for high pressure: %q", out)
	}
}

func TestSessionStatus_JSONEnvelopeStable(t *testing.T) {
	body := sessionStatusBody(t, `{
		"sessions": [{
			"session": {
				"id": "sess-1",
				"agent_id": "alice",
				"status": "working",
				"started_at": "2026-04-26T18:30:00Z"
			},
			"profile": {
				"session_id": "sess-1",
				"context_pct": 0.4
			}
		}],
		"now": "{NOW}"
	}`)
	out := runSessionStatus_helper(t, body, true)
	var env SessionStatusOut
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if len(env.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(env.Rows))
	}
	r := env.Rows[0]
	if r.SessionID != "sess-1" || r.AgentID != "alice" {
		t.Errorf("identity columns: %+v", r)
	}
	if r.Runway.Bucket == "" {
		t.Errorf("runway bucket missing: %+v", r.Runway)
	}
	if r.Runway.Drivers.Headroom <= 0 {
		t.Errorf("headroom should be populated: %+v", r.Runway.Drivers)
	}
}

func TestSessionStatus_CalibrationShrinksRunway(t *testing.T) {
	body := sessionStatusBody(t, `{
		"sessions": [
			{
				"session": {"id": "on-track", "agent_id": "a",  "status": "working", "started_at": "2026-04-26T18:30:00Z"},
				"profile": {"session_id": "on-track", "context_pct": 0.2}
			},
			{
				"session": {"id": "overrun",  "agent_id": "b",  "status": "working", "started_at": "2026-04-26T18:30:00Z"},
				"profile": {"session_id": "overrun",  "context_pct": 0.2},
				"calibration": 0.5
			}
		],
		"now": "{NOW}"
	}`)
	out := runSessionStatus_helper(t, body, true)
	var env SessionStatusOut
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Rows) != 2 {
		t.Fatalf("rows = %d", len(env.Rows))
	}
	if env.Rows[1].Runway.Score >= env.Rows[0].Runway.Score {
		t.Errorf("0.5 calibration should shrink runway score: %v vs %v",
			env.Rows[1].Runway.Score, env.Rows[0].Runway.Score)
	}
}

// ── Edge cases ──────────────────────────────────────────────────

func TestSessionStatus_EmptySessionsHints(t *testing.T) {
	out := runSessionStatus_helper(t, `{"sessions":[]}`, false)
	if !strings.Contains(out, "no sessions in input") {
		t.Errorf("expected empty hint; got %q", out)
	}
}

func TestSessionStatus_RejectsBadNow(t *testing.T) {
	cmd := newSessionStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(`{"sessions":[],"now":"not-a-timestamp"}`))
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error on malformed now")
	}
}

func TestSessionStatus_RejectsUnknownFields(t *testing.T) {
	cmd := newSessionStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(`{"sessions":[],"unknown":42}`))
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error on unknown field")
	}
}

func TestSessionStatus_NilProfileStillRendersRunway(t *testing.T) {
	// Session present, profile absent — runway should still
	// produce a (small) bucket from time-on-task / etc. Don't
	// crash on nil.
	body := `{
		"sessions": [{
			"session": {"id": "naked", "agent_id": "x", "status": "working", "started_at": "2026-04-26T18:30:00Z"}
		}]
	}`
	out := runSessionStatus_helper(t, body, true)
	var env SessionStatusOut
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Rows) != 1 {
		t.Fatalf("rows = %d", len(env.Rows))
	}
	if env.Rows[0].Runway.Bucket == "" {
		t.Error("runway bucket should populate even with nil profile")
	}
}

// Compile-time pin: catch a future flag rename.
func TestSessionStatus_FlagSurface(t *testing.T) {
	cmd := newSessionStatusCmd()
	for _, f := range []string{"json", "file"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("missing --%s flag", f)
		}
	}
}

// touch time package to keep import for future deterministic tests.
var _ = time.Now
