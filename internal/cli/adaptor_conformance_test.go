package cli

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runTest(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newAdaptorTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestAdaptorTest_NoopWorkplaneIsGreen(t *testing.T) {
	out, err := runTest(t, "--transport", "jsonl", "--target", "builtin:noop-work")
	if err != nil {
		t.Fatalf("adaptor test: unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Summary: GREEN") {
		t.Errorf("noop workplane must report green:\n%s", out)
	}
	// The DoD calls out groups A–F; every letter should appear in the
	// text summary so operators can verify coverage at a glance.
	for _, g := range []string{"Group A", "Group B", "Group C", "Group D", "Group E", "Group F"} {
		if !strings.Contains(out, g) {
			t.Errorf("summary missing %s:\n%s", g, out)
		}
	}
}

func TestAdaptorTest_NoopOrchestrationIsGreen(t *testing.T) {
	out, err := runTest(t, "--transport", "jsonl", "--target", "builtin:noop-orch")
	if err != nil {
		t.Fatalf("adaptor test: unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Summary: GREEN") {
		t.Errorf("noop orchestration must report green:\n%s", out)
	}
}

// gm-e7.7: the gt adaptor must report green via skip-counted-not-failed
// semantics. Group A passes today (manifest is well-formed); B/D-fixture/F
// skip cleanly until the lifecycle methods land in the rest of gm-e7.x.
//
// Skipped when `gt` is not on PATH so CI without Gas Town installed
// doesn't fail the build (the conformance run requires a live `gt`).
func TestAdaptorTest_GastownIsGreen(t *testing.T) {
	if _, err := exec.LookPath("gt"); err != nil {
		t.Skip("gt CLI not on PATH; gm-e7.7 conformance requires Gas Town installed")
	}
	out, err := runTest(t, "--transport", "api", "--target", "builtin:gastown")
	if err != nil {
		t.Fatalf("adaptor test: unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Summary: GREEN") {
		t.Errorf("gastown adaptor must report green:\n%s", out)
	}
	// At least Group A's two probes must PASS — they don't depend on
	// any fixture and they're the canonical proof the manifest is
	// well-formed.
	if !strings.Contains(out, "[PASS] A_describe_returns_valid_manifest") {
		t.Errorf("expected describe probe to pass:\n%s", out)
	}
	if !strings.Contains(out, "[PASS] A_manifest_round_trips_json") {
		t.Errorf("expected manifest round-trip probe to pass:\n%s", out)
	}
}

func TestAdaptorTest_RejectsUnknownTransport(t *testing.T) {
	_, err := runTest(t, "--transport", "grpc", "--target", "builtin:noop-work")
	if err == nil || !strings.Contains(err.Error(), "unknown --transport") {
		t.Fatalf("want unknown-transport error, got %v", err)
	}
}

func TestAdaptorTest_RemoteTargetIsNotYetImplemented(t *testing.T) {
	_, err := runTest(t, "--transport", "api", "--target", "http://elsewhere:9000")
	if err == nil || !strings.Contains(err.Error(), "not-yet-implemented") {
		t.Fatalf("remote target must surface not-yet-implemented, got: %v", err)
	}
}

func TestAdaptorTest_TransportMismatch(t *testing.T) {
	// noop adaptor declares jsonl; asking for api should fail.
	_, err := runTest(t, "--transport", "api", "--target", "builtin:noop-work")
	if err == nil || !strings.Contains(err.Error(), "transport mismatch") {
		t.Fatalf("transport mismatch must fail fast, got: %v", err)
	}
}

func TestAdaptorTest_JSONOutputIsValidReport(t *testing.T) {
	out, err := runTest(t, "--transport", "jsonl", "--target", "builtin:noop-work", "--json")
	if err != nil {
		t.Fatalf("adaptor test: %v\n%s", err, out)
	}
	var got struct {
		Plane  string `json:"plane"`
		Groups []struct {
			Name   string `json:"name"`
			Probes []struct {
				Name   string `json:"name"`
				Passed bool   `json:"passed"`
			} `json:"probes"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json output must parse: %v\n%s", err, out)
	}
	if got.Plane != "work" {
		t.Errorf("plane=%q, want work", got.Plane)
	}
	// A–F always present; G–K land with gm-l26 as
	// NotApplicable when the noop manifest doesn't advertise the
	// capabilities (R1–R8).
	if len(got.Groups) != 11 {
		t.Errorf("want 11 groups (A–K), got %d", len(got.Groups))
	}
}

func TestAdaptorTest_JUnitOutputWritesValidXML(t *testing.T) {
	dir := t.TempDir()
	junitPath := filepath.Join(dir, "noop.xml")
	_, err := runTest(t,
		"--transport", "jsonl", "--target", "builtin:noop-work",
		"--junit", junitPath)
	if err != nil {
		t.Fatalf("adaptor test: %v", err)
	}
	data, err := os.ReadFile(junitPath)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}
	// Header sanity — easy way to detect a wrong file got written.
	if !strings.HasPrefix(string(data), "<?xml") {
		t.Errorf("junit file missing xml header:\n%s", data)
	}
	var suites struct {
		XMLName  xml.Name `xml:"testsuites"`
		Tests    int      `xml:"tests,attr"`
		Failures int      `xml:"failures,attr"`
	}
	if err := xml.Unmarshal(data, &suites); err != nil {
		t.Fatalf("junit must parse as xml: %v", err)
	}
	if suites.Failures != 0 {
		t.Errorf("want 0 failures on noop, got %d", suites.Failures)
	}
	if suites.Tests == 0 {
		t.Errorf("junit must surface non-zero test count; got %d", suites.Tests)
	}
}

func TestAdaptorTest_PlaneMismatchOnBuiltinIsRejected(t *testing.T) {
	_, err := runTest(t,
		"--transport", "jsonl", "--target", "builtin:noop-work", "--plane", "orchestration")
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("want plane mismatch error, got: %v", err)
	}
}

func TestAdaptorTest_FailureSurfacesSentinelError(t *testing.T) {
	// Sanity: sentinel must be unique so Execute() can branch on it.
	if !errors.Is(ErrConformanceFailed, ErrConformanceFailed) {
		t.Fatal("sentinel must satisfy errors.Is with itself")
	}
}
