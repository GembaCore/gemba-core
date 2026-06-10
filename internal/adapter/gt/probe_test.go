package gt

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

// fakeProbeShell is a hand-rolled probeRunner that returns canned
// output keyed on the joined argv. Mirrors fakeGT in sessions_test.go
// but at the probe layer (probeRunner has the same shape so the two
// could be unified, but keeping them separate keeps the probe path's
// failure modes obvious).
type fakeProbeShell struct {
	resp map[string]struct {
		out []byte
		err error
	}
}

func (f *fakeProbeShell) run(_ context.Context, args ...string) ([]byte, error) {
	key := joinArgs(args)
	r, ok := f.resp[key]
	if !ok {
		return nil, errors.New("fakeProbeShell: no canned response for " + key)
	}
	return r.out, r.err
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

// silentLogger is a *slog.Logger that discards everything; used so
// probe tests don't spam stdout.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProbeCapabilitiesHappyPath(t *testing.T) {
	shell := &fakeProbeShell{
		resp: map[string]struct {
			out []byte
			err error
		}{
			"--version":        {out: []byte("gt version 1.0.0\n")},
			"sling --help":     {out: []byte("sling help text — has --json flag for output\n")},
			"scheduler --help": {out: []byte("scheduler help\n")},
			"handoff --help":   {out: []byte("handoff help\n")},
		},
	}
	caps, err := probeCapabilities(context.Background(), shell.run, silentLogger())
	if err != nil {
		t.Fatalf("probeCapabilities: %v", err)
	}
	if caps.Version != "1.0.0" {
		t.Errorf("Version=%q, want 1.0.0", caps.Version)
	}
	if caps.VersionBelowFloor {
		t.Errorf("VersionBelowFloor=true; 1.0.0 should match floor")
	}
	if !caps.SupportsSlingJSON {
		t.Errorf("SupportsSlingJSON=false; help text contains --json")
	}
	if !caps.HasScheduler {
		t.Errorf("HasScheduler=false; help shelled OK")
	}
	if !caps.HasHandoff {
		t.Errorf("HasHandoff=false; help shelled OK")
	}
}

func TestProbeCapabilitiesBelowFloor(t *testing.T) {
	shell := &fakeProbeShell{
		resp: map[string]struct {
			out []byte
			err error
		}{
			"--version":        {out: []byte("gt version 0.9.5\n")},
			"sling --help":     {out: []byte("(no json)\n")},
			"scheduler --help": {out: []byte("scheduler help\n")},
			"handoff --help":   {out: []byte("handoff help\n")},
		},
	}
	caps, err := probeCapabilities(context.Background(), shell.run, silentLogger())
	if err != nil {
		t.Fatalf("probeCapabilities: %v", err)
	}
	if !caps.VersionBelowFloor {
		t.Errorf("VersionBelowFloor=false for 0.9.5 (floor 1.0.0)")
	}
	if caps.SupportsSlingJSON {
		t.Errorf("SupportsSlingJSON=true; help text lacks --json")
	}
}

func TestProbeCapabilitiesMissingSubcommand(t *testing.T) {
	shell := &fakeProbeShell{
		resp: map[string]struct {
			out []byte
			err error
		}{
			"--version":        {out: []byte("gt version 1.0.0\n")},
			"sling --help":     {out: []byte("--json\n")},
			"scheduler --help": {err: errors.New("unknown command")},
			"handoff --help":   {err: errors.New("unknown command")},
		},
	}
	caps, err := probeCapabilities(context.Background(), shell.run, silentLogger())
	if err != nil {
		t.Fatalf("probeCapabilities: %v", err)
	}
	if caps.HasScheduler {
		t.Errorf("HasScheduler=true; subcommand probe failed")
	}
	if caps.HasHandoff {
		t.Errorf("HasHandoff=true; subcommand probe failed")
	}
	if !caps.SupportsSlingJSON {
		t.Errorf("SupportsSlingJSON=false; help advertises --json")
	}
}

func TestProbeCapabilitiesVersionFatal(t *testing.T) {
	shell := &fakeProbeShell{
		resp: map[string]struct {
			out []byte
			err error
		}{
			"--version": {err: errors.New("gt: command not found")},
		},
	}
	_, err := probeCapabilities(context.Background(), shell.run, silentLogger())
	if err == nil {
		t.Fatal("expected error when --version fails")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected *core.AdaptorError, got %T", err)
	}
	if ae.Kind != core.KindAdaptorDegraded {
		t.Errorf("Kind=%q, want adaptor_degraded", ae.Kind)
	}
}

func TestProbeCapabilitiesUnparseableVersion(t *testing.T) {
	shell := &fakeProbeShell{
		resp: map[string]struct {
			out []byte
			err error
		}{
			"--version":        {out: []byte("gt - some weird output\n")},
			"sling --help":     {out: []byte("--json\n")},
			"scheduler --help": {out: []byte("ok\n")},
			"handoff --help":   {out: []byte("ok\n")},
		},
	}
	caps, err := probeCapabilities(context.Background(), shell.run, silentLogger())
	if err != nil {
		t.Fatalf("probeCapabilities: %v", err)
	}
	if caps.Version != "" {
		t.Errorf("Version=%q, want empty when unparseable", caps.Version)
	}
	if caps.VersionBelowFloor {
		t.Errorf("VersionBelowFloor=true on unparseable version; want false (we only flag known-low)")
	}
}

func TestParseGtVersion(t *testing.T) {
	cases := map[string]string{
		"gt version 1.0.0":   "1.0.0",
		"gt version 1.2.3\n": "1.2.3",
		"gt v0.9.5":          "0.9.5",
		"1.0.0":              "1.0.0",
		"":                   "",
		"no version here":    "",
	}
	for in, want := range cases {
		got := parseGtVersion(in)
		if got != want {
			t.Errorf("parseGtVersion(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "0.9.9", 1},
		{"0.9.9", "1.0.0", -1},
		{"1.2.0", "1.1.9", 1},
		{"1.0.1", "1.0.0", 1},
		{"", "1.0.0", -1},
		{"1.0.0", "", 1},
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Errorf("compareSemver(%q,%q)=%d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDescribeSurfacesProbeMetadata(t *testing.T) {
	o := &OrchestrationPlane{
		caps: gtCapabilities{
			Version:           "1.0.0",
			VersionRaw:        "gt version 1.0.0",
			VersionBelowFloor: false,
			SupportsSlingJSON: true,
			HasScheduler:      true,
			HasHandoff:        true,
		},
	}
	m := o.Describe()
	if m.Extension == nil {
		t.Fatal("Describe().Extension is nil; gm-e7.12 requires probe metadata")
	}
	if got := m.Extension["gt_version"]; got != "1.0.0" {
		t.Errorf("Extension[gt_version]=%v, want 1.0.0", got)
	}
	if got := m.Extension["gt_version_floor"]; got != minimumGtVersion {
		t.Errorf("Extension[gt_version_floor]=%v, want %q", got, minimumGtVersion)
	}
	if got := m.Extension["has_handoff"]; got != true {
		t.Errorf("Extension[has_handoff]=%v, want true", got)
	}
}
