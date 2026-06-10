// Shader is the orthogonal data-plane → orchestrator format
// pre-processor (gm-root.4). Sits between core and the WorkPlane
// adaptor: encodes WorkItems on the way out so the underlying store
// (bd, dolt, jira, …) records them in the active orchestrator's
// idiomatic format; decodes them on the way back so the SPA always
// sees native types regardless of which orchestrator is wired.
//
// The shader is OPT-IN. Without one configured, gemba serve wraps
// the adaptor in NopShader and the wrapped WorkPlane behaves
// identically to the unwrapped one (every existing test still passes).
//
// Selection is config-driven, not baked into the adaptor:
//   bd adaptor + Gas Town shader   = "beads for gastown"
//   bd adaptor + LangGraph shader  = "beads for langraph"
//   jira adaptor + Gas Town shader = "jira for gastown"

package core

import "context"

// WriteOp tells the shader which adaptor entry-point initiated the
// encode call. The two values map 1:1 to the WorkPlane methods that
// take a WorkItem (or patch) on the wire.
type WriteOp string

const (
	WriteCreate WriteOp = "create"
	WriteUpdate WriteOp = "update"
)

// ShaderManifest is the declarative description every shader returns
// from Describe. Surfaced through /api/capabilities so the SPA can
// show "shader: gastown" in the workspace banner without poking the
// shader directly.
type ShaderManifest struct {
	// Name identifies the shader implementation ("nop", "gastown", …).
	Name string `json:"name"`
	// EncodedFields lists which WorkItem fields the shader rewrites
	// on write. Lets the UI hint to operators which values they're
	// editing in native vs encoded form. Common entries: "title",
	// "labels", "description". Empty for NopShader.
	EncodedFields []string `json:"encoded_fields,omitempty"`
}

// Shader is the encode/decode pair every orchestrator-flavour
// implements. Every method MUST be safe to call on the zero value
// and MUST NOT mutate the input WorkItem in place — return a new
// value if any field changes.
type Shader interface {
	// EncodeForWrite rewrites a native WorkItem into the form the
	// underlying adaptor should persist. op tells the shader whether
	// the call originated from CreateWorkItem or UpdateWorkItem; some
	// transforms (e.g. id-minting) only fire on Create.
	EncodeForWrite(ctx context.Context, op WriteOp, item WorkItem) (WorkItem, error)

	// DecodeFromRead is the inverse: turn an adaptor-stored WorkItem
	// back into the native shape the SPA expects. MUST be a pure
	// function of item — the SPA calls Get / List independently and
	// expects identical output regardless of order.
	DecodeFromRead(ctx context.Context, item WorkItem) (WorkItem, error)

	// Describe returns the shader's identity + advertised contract.
	// Idempotent and side-effect-free.
	Describe() ShaderManifest
}

// NopShader is the default when no orchestrator config is loaded.
// Identity transform on both directions; satisfies the Shader
// interface so callers never need a nil-check.
type NopShader struct{}

func (NopShader) EncodeForWrite(_ context.Context, _ WriteOp, item WorkItem) (WorkItem, error) {
	return item, nil
}

func (NopShader) DecodeFromRead(_ context.Context, item WorkItem) (WorkItem, error) {
	return item, nil
}

func (NopShader) Describe() ShaderManifest {
	return ShaderManifest{Name: "nop"}
}
