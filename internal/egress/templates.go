// Templates — named bundles of egress rules (gm-o9t8.4.3, wsB slice a).
//
// On workspace create, the lifecycle handler resolves a template by
// name and seeds the new workspace's egress allowlist by copying the
// bundle's allowed FQDNs into per-workspace Rules. A bundle also
// carries a default network stance (deny | allow) the runtime
// enforcer reads when no rule matches.
//
// Defined bundles:
//
//	github-only  — allow GitHub-family hosts; default deny.
//	pypi+npm     — github-only + PyPI + npm; default deny.
//	wide-open    — default allow; no per-host rules (egress is open).
//	default      — alias for github-only.
//
// Unknown / empty template names fall back to "default". Resolution is
// case-insensitive.

package egress

import "strings"

// NetworkDefault is the default network stance of a template — the
// terminal action the runtime enforcer falls through to when no rule
// in the workspace's effective set matches. The string values match
// Action so callers can interpret either form.
type NetworkDefault string

const (
	NetworkDeny  NetworkDefault = "deny"
	NetworkAllow NetworkDefault = "allow"
)

// Template is a named bundle of allowed FQDNs plus an optional
// network-default stance. Pure data — the lifecycle handler converts
// AllowedFQDNs to Rules at workspace-create time so the on-disk
// representation is consistent with operator-added rules.
type Template struct {
	Name           string
	AllowedFQDNs   []string
	NetworkDefault NetworkDefault
}

// TemplateProvider is the contract the server's workspace-create
// handler depends on. The default implementation is BuiltinTemplates;
// tests can substitute a stub.
type TemplateProvider interface {
	Lookup(name string) Template
}

// BuiltinTemplates is the zero-value-safe provider that serves the
// hard-coded bundles. Use NewBuiltinTemplates for the canonical
// instance.
type BuiltinTemplates struct{}

// NewBuiltinTemplates returns the canonical TemplateProvider.
func NewBuiltinTemplates() BuiltinTemplates { return BuiltinTemplates{} }

// DefaultTemplateName is the name resolved when callers omit a
// template or pass an unknown one.
const DefaultTemplateName = "default"

// Lookup returns the bundle named name. Unknown / empty names fall
// back to "default" (alias for "github-only").
func (BuiltinTemplates) Lookup(name string) Template {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "wide-open":
		return Template{
			Name:           "wide-open",
			AllowedFQDNs:   nil,
			NetworkDefault: NetworkAllow,
		}
	case "pypi+npm":
		return Template{
			Name: "pypi+npm",
			AllowedFQDNs: []string{
				"github.com",
				"api.github.com",
				"ghcr.io",
				"codeload.github.com",
				"objects.githubusercontent.com",
				"pypi.org",
				"files.pythonhosted.org",
				"registry.npmjs.org",
			},
			NetworkDefault: NetworkDeny,
		}
	case "github-only", DefaultTemplateName, "":
		return Template{
			Name: "github-only",
			AllowedFQDNs: []string{
				"github.com",
				"api.github.com",
				"ghcr.io",
				"codeload.github.com",
				"objects.githubusercontent.com",
			},
			NetworkDefault: NetworkDeny,
		}
	}
	// Unknown name — return default rather than empty so the
	// lifecycle handler always seeds a safe baseline.
	return BuiltinTemplates{}.Lookup(DefaultTemplateName)
}

// TemplateNames returns the set of well-known bundle names this
// provider answers for. Aliases (default → github-only) are listed as
// distinct entries so the SPA template-picker can present them
// verbatim.
func TemplateNames() []string {
	return []string{"github-only", "pypi+npm", "wide-open", DefaultTemplateName}
}
