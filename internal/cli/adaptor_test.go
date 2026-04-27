package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

func runRegister(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newAdaptorRegisterCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestAdaptorRegister_RejectsUnknownTransport(t *testing.T) {
	_, err := runRegister(t, "--transport", "grpc", "--target", "localhost:9000")
	if err == nil || !strings.Contains(err.Error(), "unknown --transport") {
		t.Fatalf("want unknown-transport error, got %v", err)
	}
}

func TestAdaptorRegister_RejectsUnknownPlane(t *testing.T) {
	_, err := runRegister(t,
		"--transport", "api", "--target", "http://x", "--plane", "nope")
	if err == nil || !strings.Contains(err.Error(), "unknown --plane") {
		t.Fatalf("want unknown-plane error, got %v", err)
	}
}

func TestAdaptorRegister_RequiresTransportAndTarget(t *testing.T) {
	_, err := runRegister(t, "--transport", "api")
	if err == nil {
		t.Fatal("want error when --target is missing")
	}
}

func TestAdaptorRegister_HumanOutputIncludesCoreVersion(t *testing.T) {
	out, err := runRegister(t, "--transport", "jsonl", "--target", "./noop")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if !strings.Contains(out, core.ProtocolVersion) {
		t.Errorf("output must surface core_version for operator diagnosis:\n%s", out)
	}
	if !strings.Contains(out, "not-yet-implemented") {
		t.Errorf("output must mark status not-yet-implemented until wire client lands:\n%s", out)
	}
}

func TestAdaptorRegister_JSONOutput(t *testing.T) {
	out, err := runRegister(t,
		"--transport", "mcp", "--target", "stdio://x", "--json")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got["transport"] != "mcp" || got["core_version"] != core.ProtocolVersion {
		t.Errorf("unexpected payload: %+v", got)
	}
}

func TestAdaptorRegister_AllThreeTransportsAccepted(t *testing.T) {
	for _, tp := range []string{"api", "jsonl", "mcp"} {
		out, err := runRegister(t, "--transport", tp, "--target", "x")
		if err != nil {
			t.Errorf("transport %s: unexpected error: %v", tp, err)
			continue
		}
		if !strings.Contains(out, "transport:    "+tp) &&
			!strings.Contains(out, "\"transport\": \""+tp+"\"") {
			t.Errorf("transport %s: output missing transport line: %s", tp, out)
		}
	}
}
