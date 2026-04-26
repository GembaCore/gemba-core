// Package shader provides a decorator that wraps a core.WorkPlane in
// a core.Shader, encoding writes / decoding reads transparently.
//
// The decorator is the only place the rest of the system needs to
// know about shaders: gemba serve calls shader.Wrap(adaptor, sh)
// during plane registration; from then on every handler talks to
// the wrapped WorkPlane and shader transforms are invisible.
//
// Update encoding needs the WorkItem's current Kind to compute the
// title prefix, but a WorkItemPatch doesn't carry Kind. The decorator
// therefore performs a Get before each title-touching Update so the
// shader has the context it needs. Updates that don't touch the title
// skip the extra round-trip.

package shader

import (
	"context"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Wrap returns a WorkPlane whose reads run through Shader.DecodeFromRead
// and whose writes run through Shader.EncodeForWrite. Passing a nil
// shader is equivalent to passing core.NopShader{} — callers don't
// need to special-case the no-shader path.
//
// Optional capabilities (currently core.WorkItemNotifier — gm-jqwf,
// follow-up to gm-e4.3.2) are forwarded conditionally: if the inner
// adaptor implements the optional interface, the returned wrapper
// also implements it via direct delegation. Adaptors that don't
// implement the optional surface get the bare decorator, so the
// server-side type assertion still 409s correctly against opt-out
// backends.
func Wrap(inner core.WorkPlane, sh core.Shader) core.WorkPlane {
	if sh == nil {
		sh = core.NopShader{}
	}
	d := &decorator{inner: inner, shader: sh}
	if n, ok := inner.(core.WorkItemNotifier); ok {
		return &notifyingDecorator{decorator: d, notifier: n}
	}
	return d
}

// notifyingDecorator is the WorkPlane wrapper used when the inner
// adaptor implements core.WorkItemNotifier. NotifyExternal delegates
// to the inner so the inner's emitter (the same one Subscribe pulls
// from) is the one that publishes — preserving the in-process /
// out-of-process indistinguishability the contract promises.
type notifyingDecorator struct {
	*decorator
	notifier core.WorkItemNotifier
}

func (n *notifyingDecorator) NotifyExternal(ctx context.Context, id core.WorkItemID, source string) (core.WorkItem, string, error) {
	return n.notifier.NotifyExternal(ctx, id, source)
}

// Compile-time guarantees that the wrapper satisfies both surfaces.
var (
	_ core.WorkPlane        = (*notifyingDecorator)(nil)
	_ core.WorkItemNotifier = (*notifyingDecorator)(nil)
)

type decorator struct {
	inner  core.WorkPlane
	shader core.Shader
}

// Static guarantee at compile time that decorator satisfies the
// WorkPlane interface — surfaces method-shape drift before runtime.
var _ core.WorkPlane = (*decorator)(nil)

func (d *decorator) Describe(ctx context.Context) (core.CapabilityManifest, error) {
	return d.inner.Describe(ctx)
}

func (d *decorator) ListWorkItems(ctx context.Context, f core.WorkItemFilter) ([]core.WorkItem, error) {
	items, err := d.inner.ListWorkItems(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]core.WorkItem, 0, len(items))
	for _, it := range items {
		decoded, err := d.shader.DecodeFromRead(ctx, it)
		if err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}
	return out, nil
}

func (d *decorator) GetWorkItem(ctx context.Context, id core.WorkItemID) (core.WorkItem, error) {
	item, err := d.inner.GetWorkItem(ctx, id)
	if err != nil {
		return core.WorkItem{}, err
	}
	return d.shader.DecodeFromRead(ctx, item)
}

func (d *decorator) CreateWorkItem(ctx context.Context, wi core.WorkItem) (core.WorkItem, error) {
	encoded, err := d.shader.EncodeForWrite(ctx, core.WriteCreate, wi)
	if err != nil {
		return core.WorkItem{}, err
	}
	out, err := d.inner.CreateWorkItem(ctx, encoded)
	if err != nil {
		return core.WorkItem{}, err
	}
	return d.shader.DecodeFromRead(ctx, out)
}

func (d *decorator) UpdateWorkItem(ctx context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
	// When the patch touches Title, the shader needs the WorkItem's
	// Kind to compute the kind-prefix. Patches don't carry Kind, so
	// fetch the current item and feed the synthesized
	// {current.Kind, patch.Title} pair to the encoder. If the Get
	// fails (e.g. id not found, adaptor degraded), the inner
	// UpdateWorkItem call below will surface the same error with the
	// same envelope — keep it simple and let the patch pass through
	// to that natural error path.
	if patch.Title != nil {
		current, getErr := d.inner.GetWorkItem(ctx, id)
		if getErr == nil {
			synth := current
			synth.Title = *patch.Title
			encoded, err := d.shader.EncodeForWrite(ctx, core.WriteUpdate, synth)
			if err != nil {
				return core.WorkItem{}, err
			}
			t := encoded.Title
			patch.Title = &t
		}
	}

	out, err := d.inner.UpdateWorkItem(ctx, id, patch)
	if err != nil {
		return core.WorkItem{}, err
	}
	return d.shader.DecodeFromRead(ctx, out)
}

// AttachEvidence, ListSprints, ReadBudgetRollup are passthrough today
// — the shader contract scopes itself to WorkItem field rewrites.
// Evidence + sprints + rollups don't carry orchestrator-flavoured
// titles or labels, so encoding them would just be ceremony.

func (d *decorator) AttachEvidence(ctx context.Context, id core.WorkItemID, ev core.Evidence) error {
	return d.inner.AttachEvidence(ctx, id, ev)
}

func (d *decorator) ListSprints(ctx context.Context) ([]core.Sprint, error) {
	return d.inner.ListSprints(ctx)
}

func (d *decorator) ReadBudgetRollup(ctx context.Context, sprintID string) (core.BudgetRollup, error) {
	return d.inner.ReadBudgetRollup(ctx, sprintID)
}

func (d *decorator) Subscribe(ctx context.Context, f core.WorkPlaneSubscribeFilter) (<-chan core.WorkPlaneEvent, error) {
	return d.inner.Subscribe(ctx, f)
}
