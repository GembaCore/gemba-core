// Gas Town transport: api declaration (gm-e7.6).
//
// The Gas Town adaptor speaks the API transport (HTTP + JSON
// request/response per gm-root DD-12). v1 the "API" channel is
// the `gt` CLI invoked with `--json` — every gtProbeRunner call
// is a one-shot JSON request/response cycle:
//
//   gt rig list --json     → []rig{...}
//   gt polecat list --json → []polecat{...}
//   gt mail inbox --json   → []mail{...}
//   gt session ...
//
// The API transport contract is identical to a remote HTTP+JSON
// service — the adaptor just happens to reach the local `gt`
// process via exec rather than HTTP. A future remote shim (e.g.
// `gemba adaptor register --transport api --target https://...`)
// can replace the exec runner with a fetcher that POSTs the
// equivalent payloads to a Gas Town daemon and the adaptor body
// stays unchanged.
//
// gm-e7.6 ships:
//   - The transport declaration on the manifest (api).
//   - This file documenting the contract.
//   - docs/adaptors/gastown.md operator-facing docs.
//
// Conformance traffic (groups A–F, gm-e7.7) flows through the
// same JSON request/response surface. No MCP bridge required —
// MCP is recommended-not-required per DD-15.

package gt

import (
	"encoding/json"
	"fmt"
)

// transportAPIBuildInfo is the per-process self-describing
// metadata the adaptor exposes when asked. Helpful for the
// /api/adaptors snapshot to show "gt adaptor — transport: api,
// channel: exec(gt --json), version: ..." without callers
// re-inferring it from the manifest.
type transportAPIBuildInfo struct {
	Channel string `json:"channel"`
	Notes   string `json:"notes"`
}

// transportAPIInfo returns a small JSON blob describing the
// concrete API channel this build uses. Stable string keys so
// the SPA can render a one-liner without per-adaptor casing.
func transportAPIInfo() transportAPIBuildInfo {
	return transportAPIBuildInfo{
		Channel: "exec(gt --json)",
		Notes: "API transport over local `gt` CLI invocations with --json. " +
			"Each method is a one-shot JSON request/response cycle. A future " +
			"remote shim can swap the exec runner for an HTTP fetcher without " +
			"changing the adaptor body.",
	}
}

// MarshalTransportInfo encodes the info as JSON. Convenience for
// callers that want to embed the blob in a wider response. Returns
// the encoded bytes; panics on encode failure (the struct is
// trivially JSON-encodable, so an error here is a programming
// bug worth surfacing loudly).
func MarshalTransportInfo() []byte {
	b, err := json.Marshal(transportAPIInfo())
	if err != nil {
		panic(fmt.Sprintf("gt: encode transport info: %v", err))
	}
	return b
}
