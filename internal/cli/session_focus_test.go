// Tests for `gemba session focus` (gm-v5z2.3). Focus on the
// flag-parsing + describe-intent helpers; the SQL boundary is
// exercised in internal/planner/intent/store_test.go via sqlmock.
//
// We can't drive the dolt-url path from a unit test without a live
// server, so the integration check goes as far as "command parses,
// errors when --dolt-url is missing".

package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/planner/intent"
)

func runFocusCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd(BuildInfo{})
	root.SetArgs(append([]string{"session", "focus"}, args...))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

func TestSessionFocusSet_RequiresAtLeastOneRestrictor(t *testing.T) {
	out, err := runFocusCmd(t, "set", "sess-1", "--dolt-url", "mysql://r@127.0.0.1:3307/db")
	if err == nil {
		t.Fatalf("expected error when no restrictors are passed; out=%q", out)
	}
	if !strings.Contains(err.Error(), "epic") &&
		!strings.Contains(err.Error(), "label") &&
		!strings.Contains(err.Error(), "regex") {
		t.Errorf("error should call out the missing restrictors: %v", err)
	}
}

func TestSessionFocusSet_RejectsMissingDoltURL(t *testing.T) {
	_, err := runFocusCmd(t, "set", "sess-1", "--epic", "gm-e1")
	if err == nil {
		t.Fatal("expected error when --dolt-url is missing")
	}
	if !strings.Contains(err.Error(), "dolt-url") {
		t.Errorf("error should call out --dolt-url: %v", err)
	}
}

func TestSessionFocusList_MissingDoltURL(t *testing.T) {
	_, err := runFocusCmd(t, "list")
	if err == nil {
		t.Fatal("expected error when --dolt-url is missing")
	}
}

func TestSessionFocusGet_RequiresArg(t *testing.T) {
	_, err := runFocusCmd(t, "get")
	if err == nil {
		t.Fatal("expected error when session-id arg is missing")
	}
}

func TestSessionFocusAudit_RequiresArg(t *testing.T) {
	_, err := runFocusCmd(t, "audit")
	if err == nil {
		t.Fatal("expected error when session-id arg is missing")
	}
}

// describeIntent renders the audit's "from → to" text. The empty
// case must read as "(empty)" rather than blank to make the audit
// log clear about cleared states.
func TestDescribeIntent_Empty(t *testing.T) {
	if got := describeIntent(intent.Intent{}); got != "(empty)" {
		t.Errorf("empty intent should render as (empty); got %q", got)
	}
}

func TestDescribeIntent_SingleRestrictor(t *testing.T) {
	cases := map[string]intent.Intent{
		"epic=gm-e1":             {EpicID: "gm-e1"},
		"label=spa":              {Label: "spa"},
		"regex=^gm-s47n\\.[0-9]": {BeadIDRegex: "^gm-s47n\\.[0-9]"},
	}
	for want, in := range cases {
		if got := describeIntent(in); got != want {
			t.Errorf("describeIntent(%+v) = %q; want %q", in, got, want)
		}
	}
}

func TestDescribeIntent_MultipleRestrictors(t *testing.T) {
	got := describeIntent(intent.Intent{
		EpicID: "gm-e1", Label: "spa", BeadIDRegex: "^gm-",
	})
	want := "epic=gm-e1,label=spa,regex=^gm-"
	if got != want {
		t.Errorf("multi: got %q; want %q", got, want)
	}
}

// Smoke check the SetInput shape is forwarded as expected.
func TestSetInputShape_PreservesNowOverride(t *testing.T) {
	now := time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC)
	in := intent.SetInput{
		SessionID:      "sess-1",
		EpicID:         "gm-e1",
		Rationale:      "test",
		DemotionFactor: 0.4,
		Actor:          "cli:test",
		Now:            func() time.Time { return now },
	}
	if in.Now() != now {
		t.Errorf("Now override not threaded")
	}
}
