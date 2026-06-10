package gt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

// gm-e7.6: the manifest must declare TransportAPI. Conformance
// suite (gm-e7.7) keys off this; if it ever drifts back to
// TransportJSONL, the gt-adaptor-over-HTTP shim story breaks.
func TestManifestDeclaresTransportAPI(t *testing.T) {
	m := (&OrchestrationPlane{}).Describe()
	if m.Transport != core.TransportAPI {
		t.Errorf("Transport = %q, want %q", m.Transport, core.TransportAPI)
	}
}

func TestTransportInfoIsValidJSON(t *testing.T) {
	b := MarshalTransportInfo()
	var got transportAPIBuildInfo
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Channel == "" {
		t.Error("Channel must not be empty")
	}
	if !strings.Contains(got.Channel, "gt") {
		t.Errorf("Channel = %q; expected to mention 'gt'", got.Channel)
	}
}
