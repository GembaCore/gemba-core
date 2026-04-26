package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/planner"
)

func runDispatch(t *testing.T, args []string) string {
	t.Helper()
	cmd := newDispatchCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v\n%s", args, err, stdout.String())
	}
	return stdout.String()
}

// mountTempPolicyFile writes the given policy at <tmp>/dispatch.json
// and returns the path so the caller can pass --file=<path>.
func mountTempPolicyFile(t *testing.T, p planner.DispatchPolicy) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dispatch.json")
	if err := planner.SavePolicyFile(path, p); err != nil {
		t.Fatalf("save: %v", err)
	}
	return path
}

func TestDispatchStatus_PrintsPolicyFromFile(t *testing.T) {
	path := mountTempPolicyFile(t, planner.DispatchPolicy{
		Enabled:               true,
		MinIntervalPerSession: 90 * time.Second,
		MaxConcurrent:         2,
	})
	out := runDispatch(t, []string{"--file", path, "status"})
	if !strings.Contains(out, "enabled:                  true") {
		t.Errorf("expected enabled=true in output:\n%s", out)
	}
	if !strings.Contains(out, "1m30s") {
		t.Errorf("expected min_interval display:\n%s", out)
	}
}

func TestDispatchStatus_JSONEnvelope(t *testing.T) {
	path := mountTempPolicyFile(t, planner.DispatchPolicy{Enabled: true, MaxConcurrent: 3})
	out := runDispatch(t, []string{"--file", path, "status", "--json"})
	var env struct {
		Path   string                 `json:"path"`
		Policy planner.DispatchPolicy `json:"policy"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if env.Policy.MaxConcurrent != 3 {
		t.Errorf("max_concurrent = %d", env.Policy.MaxConcurrent)
	}
	if env.Path != path {
		t.Errorf("path = %q, want %q", env.Path, path)
	}
}

func TestDispatchOff_FlipsKillSwitch(t *testing.T) {
	path := mountTempPolicyFile(t, planner.DispatchPolicy{Enabled: true})
	runDispatch(t, []string{"--file", path, "off"})
	pol, err := planner.LoadPolicyFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if pol.Enabled {
		t.Errorf("kill switch did not flip; enabled=%v", pol.Enabled)
	}
}

func TestDispatchOn_TogglesAfterOff(t *testing.T) {
	path := mountTempPolicyFile(t, planner.DispatchPolicy{Enabled: false})
	runDispatch(t, []string{"--file", path, "on"})
	pol, _ := planner.LoadPolicyFile(path)
	if !pol.Enabled {
		t.Errorf("on did not enable; enabled=%v", pol.Enabled)
	}
}

func TestDispatchSet_TunesMinIntervalAndMaxConcurrent(t *testing.T) {
	path := mountTempPolicyFile(t, planner.DispatchPolicy{Enabled: true})
	runDispatch(t, []string{
		"--file", path, "set",
		"min-interval=30s",
		"max-concurrent=8",
	})
	pol, _ := planner.LoadPolicyFile(path)
	if pol.MinIntervalPerSession != 30*time.Second {
		t.Errorf("min-interval = %s", pol.MinIntervalPerSession)
	}
	if pol.MaxConcurrent != 8 {
		t.Errorf("max-concurrent = %d", pol.MaxConcurrent)
	}
}

func TestDispatchSet_BadValueWarnsButPersistsTheRest(t *testing.T) {
	path := mountTempPolicyFile(t, planner.DispatchPolicy{Enabled: true})
	cmd := newDispatchCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--file", path, "set", "min-interval=not-a-duration", "max-concurrent=4"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stdout.String(), "warning:") {
		t.Errorf("expected warning for bad duration:\n%s", stdout.String())
	}
	pol, _ := planner.LoadPolicyFile(path)
	if pol.MaxConcurrent != 4 {
		t.Errorf("good kv was not applied; max=%d", pol.MaxConcurrent)
	}
}

func TestDispatchStatus_MissingFileShowsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	out := runDispatch(t, []string{"--file", path, "status"})
	if !strings.Contains(out, "enabled:                  false") {
		t.Errorf("missing file should report enabled=false:\n%s", out)
	}
	if !strings.Contains(out, "default") {
		t.Errorf("missing file should report default min interval:\n%s", out)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("status MUST not write the file")
	}
}

// Compile-time check that newDispatchCmd returns *cobra.Command.
var _ *cobra.Command = newDispatchCmd()
