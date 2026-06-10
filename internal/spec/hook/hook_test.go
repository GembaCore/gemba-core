package hook

import (
	"bytes"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
)

func strict() constitution.Constitution {
	t := true
	return constitution.Constitution{SpecStrictNoTasksMD: &t}
}

func permissive() constitution.Constitution { return constitution.Constitution{} }

func TestWriteRejectsTasksMD(t *testing.T) {
	in := []byte(`{"tool_name":"Write","tool_input":{"file_path":"specs/001/tasks.md"}}`)
	d := EvaluateWriteEdit(in, strict())
	if !d.Block {
		t.Fatal("expected block")
	}
	if !strings.Contains(d.Reason, "deprecated") {
		t.Fatal("missing message")
	}
	if !strings.Contains(d.Reason, "gm-v0sp.21") {
		t.Fatal("missing analyzer seam reference")
	}
	if d.Payload == "" {
		t.Fatal("payload should be preserved for analyzer hand-off")
	}
}

func TestWriteAllowsOtherFiles(t *testing.T) {
	in := []byte(`{"tool_name":"Write","tool_input":{"file_path":"specs/001/spec.md"}}`)
	if EvaluateWriteEdit(in, strict()).Block {
		t.Fatal("spec.md should be allowed")
	}
}

func TestWriteAllowedWhenPermissive(t *testing.T) {
	in := []byte(`{"tool_name":"Write","tool_input":{"file_path":"tasks.md"}}`)
	if EvaluateWriteEdit(in, permissive()).Block {
		t.Fatal("permissive mode must allow")
	}
}

func TestBashRedirectBlocked(t *testing.T) {
	cases := []string{
		`{"tool_name":"Bash","tool_input":{"command":"echo hi > tasks.md"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"echo hi >> todo.md"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"sed -i 's/x/y/' tasks.md"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"cat >> tasks.md"}}`,
	}
	for _, c := range cases {
		d := EvaluateBash([]byte(c), strict())
		if !d.Block {
			t.Fatalf("expected block for %s", c)
		}
	}
}

func TestBashUnrelatedAllowed(t *testing.T) {
	in := []byte(`{"tool_name":"Bash","tool_input":{"command":"ls -la"}}`)
	if EvaluateBash(in, strict()).Block {
		t.Fatal("unrelated command must pass")
	}
}

func TestRun_ExitCodes(t *testing.T) {
	var serr bytes.Buffer
	raw := []byte(`{"tool_name":"Write","tool_input":{"file_path":"tasks.md"}}`)
	code := Run("write", bytes.NewReader(raw), &serr, strict())
	if code != ExitBlock {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(serr.String(), "tasks.md is deprecated") {
		t.Fatalf("stderr missing message: %s", serr.String())
	}
}
