// gm-o9t8.4.3 — server-side egress template adapter.
//
// The workspace lifecycle handler (workspaces_lifecycle.go) consumes
// templates via EgressTemplateProvider so tests can substitute a
// stub provider without dragging the package defaults in. The
// production wiring uses defaultEgressTemplateProvider, a thin
// adapter over internal/egress.BuiltinTemplates.

package server

import (
	"context"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/egress"
)

// EgressTemplateProvider is the contract workspace-create uses to
// resolve a named template + materialize it as a slice of egress.Rule
// rows keyed to the new workspace.
type EgressTemplateProvider interface {
	// Lookup returns the named bundle. An empty / unknown name
	// resolves to the provider's "default" bundle. Implementations
	// MUST never return an empty Name — callers persist the resolved
	// name in audit so an unknown template's fallback is observable.
	Lookup(name string) egress.Template
	// SeedRules converts a bundle into per-workspace Rules ready for
	// egress.Store.Put. The slice is priority-ordered (highest
	// first) so the runtime enforcer walks bundle hosts before the
	// hard-coded baseline allow-list.
	SeedRules(wsid string, tpl egress.Template, now time.Time) []egress.Rule
}

// AttachEgressTemplates wires p as the active EgressTemplateProvider.
// Passing nil restores the package default (BuiltinTemplates).
func (r *Router) AttachEgressTemplates(p EgressTemplateProvider) {
	r.egressTemplates = p
}

// effectiveEgressTemplates returns the provider in use — the attached
// override or the package default. Always non-nil.
func (r *Router) effectiveEgressTemplates() EgressTemplateProvider {
	if r.egressTemplates != nil {
		return r.egressTemplates
	}
	return defaultEgressTemplateProvider{}
}

// SeedWorkspaceEgress materializes the template named name into
// store. Returns the resolved template (after the empty/unknown
// fallback) so the caller can audit it. A nil store is a no-op —
// the lifecycle handler may run in tests without a Dolt pool.
func (r *Router) SeedWorkspaceEgress(ctx context.Context, store egress.Store, wsid, name string) (egress.Template, error) {
	p := r.effectiveEgressTemplates()
	tpl := p.Lookup(name)
	if store == nil {
		return tpl, nil
	}
	rules := p.SeedRules(wsid, tpl, time.Now().UTC())
	for _, rule := range rules {
		if err := store.Put(ctx, rule); err != nil {
			return tpl, err
		}
	}
	return tpl, nil
}

// defaultEgressTemplateProvider is the production adapter over the
// hard-coded BuiltinTemplates bundle set.
type defaultEgressTemplateProvider struct{}

// Lookup delegates to egress.BuiltinTemplates.
func (defaultEgressTemplateProvider) Lookup(name string) egress.Template {
	return egress.NewBuiltinTemplates().Lookup(name)
}

// SeedRules materializes the bundle as per-workspace Rules. Rules
// carry positive Priority (1000 + index, descending) so they outrank
// the negative-priority defaults in egress.Defaults.
func (defaultEgressTemplateProvider) SeedRules(wsid string, tpl egress.Template, now time.Time) []egress.Rule {
	if wsid == "" || len(tpl.AllowedFQDNs) == 0 {
		return nil
	}
	out := make([]egress.Rule, 0, len(tpl.AllowedFQDNs))
	for i, host := range tpl.AllowedFQDNs {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		out = append(out, egress.Rule{
			ID:          "tpl-" + tpl.Name + "-" + safeRuleSuffix(host),
			WorkspaceID: wsid,
			Action:      egress.ActionAllow,
			Proto:       egress.ProtoTCP,
			HostPattern: host,
			PortStart:   443,
			PortEnd:     443,
			Priority:    1000 - i,
			CreatedAt:   now,
			CreatedBy:   "egress-template:" + tpl.Name,
		})
	}
	return out
}

// safeRuleSuffix turns a host pattern into a stable, store-safe id
// fragment (lowercase, dots → dashes).
func safeRuleSuffix(host string) string {
	r := strings.ToLower(host)
	r = strings.ReplaceAll(r, ".", "-")
	r = strings.ReplaceAll(r, "*", "x")
	return r
}
