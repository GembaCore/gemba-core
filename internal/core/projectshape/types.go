// Package projectshape introduces named, shared understandings of the
// implementation pattern a workspace follows — the "architectural
// archetype" the project lives under (gm-4uso).
//
// A Shape primes personas (PM, coaches, managers) with the right
// vocabulary and the right default recommendations. A coach helping
// with a 2-tier-with-SPA shape says different things than one helping
// a mobile-with-backend shape.
//
// The persona dispatcher injects the active workspace's Shape via the
// prompt envelope's project-values layer (gm-3d6). Shapes are
// orthogonal to the gm-9rv persona axes (Personality / Perspective /
// Purview); this package owns the project-values layer injection
// only, not Phase or Purview interactions.
//
// This bead (gm-4uso) ships the seed registry of five shapes plus the
// `.gemba/project_shape.toml` loader. CLI affordances (`gemba shape
// list`, `gemba shape update`) and operator-authored shape files
// under `.gemba/shapes/` are deliberately deferred to follow-up beads
// — the foundation here is the structured Shape type and the
// dispatcher injection.
package projectshape

import "strings"

// Shape is one architectural archetype. It captures the tiers a
// project actually has, the runtime path a typical request follows,
// the tooling and deployment surface, and a short prose blurb the
// persona reads as framing.
//
// The fields are deliberately small. A persona doesn't need to know
// every detail of the build — it needs to know enough to say "you
// have no edge tier; don't recommend Cloudflare Workers patterns" or
// "the backend is in Go; suggest go test idioms, not jest".
//
// A zero-value Shape is valid: Render returns the empty string so a
// dispatcher with no shape configured emits no project-values
// contribution from this layer.
type Shape struct {
	// Name is the canonical, stable identifier for this shape.
	// Personas reason about a stable identifier (e.g. "two-tier-spa")
	// across consults — DO NOT translate the name for display.
	Name string

	// Description is a one-paragraph blurb used at the top of the
	// rendered block. Speaks in the second person to the persona
	// ("Your project is …") so the framing reads naturally inline
	// with the rest of the system prompt.
	Description string

	// Tiers names the architectural tiers the project has, in the
	// order a request flows through them (e.g. "spa frontend",
	// "http api backend", "postgres"). Personas read this to scope
	// advice; "your project has no edge tier" follows from this list.
	Tiers []string

	// RequestPath is the canonical path a typical user request
	// follows through the tiers. Phrased as a chain
	// ("browser → https → api → postgres"). Concrete; one example
	// is more useful than abstract topology fields.
	RequestPath string

	// Tooling is the set of build/test/deploy tools the operator
	// reaches for daily (e.g. "vite", "go test", "goreleaser",
	// "systemd"). Keeps coach advice in the operator's vocabulary.
	Tooling []string

	// DeploymentSurface describes where the project runs in
	// production (e.g. "single VPS via systemd", "vercel + edge
	// functions", "iOS/Android stores + backend cluster"). Drives
	// recommendations about release cadence, rollback strategy, and
	// observability defaults.
	DeploymentSurface string
}

// IsZero reports whether s carries no shape information. The
// dispatcher uses this to skip the project-values injection when no
// shape is configured.
func (s Shape) IsZero() bool {
	return s.Name == "" &&
		s.Description == "" &&
		len(s.Tiers) == 0 &&
		s.RequestPath == "" &&
		len(s.Tooling) == 0 &&
		s.DeploymentSurface == ""
}

// Render returns the human-readable, prompt-ready block the
// dispatcher injects into the project-values layer. Format is
// deliberately compact — bulleted facts rather than prose — so the
// model anchors on the structured signals without inflating the
// prompt budget.
//
// A zero-value Shape returns the empty string. The dispatcher
// interprets that as "no shape configured" and omits the project-
// values contribution entirely (gm-4uso's no-shape fallback).
func (s Shape) Render() string {
	if s.IsZero() {
		return ""
	}

	var b strings.Builder
	b.WriteString("Project shape: ")
	if s.Name != "" {
		b.WriteString(s.Name)
	} else {
		b.WriteString("(unnamed)")
	}
	if d := strings.TrimSpace(s.Description); d != "" {
		b.WriteString("\n")
		b.WriteString(d)
	}
	if len(s.Tiers) > 0 {
		b.WriteString("\nTiers: ")
		b.WriteString(strings.Join(s.Tiers, ", "))
	}
	if rp := strings.TrimSpace(s.RequestPath); rp != "" {
		b.WriteString("\nRequest path: ")
		b.WriteString(rp)
	}
	if len(s.Tooling) > 0 {
		b.WriteString("\nTooling: ")
		b.WriteString(strings.Join(s.Tooling, ", "))
	}
	if ds := strings.TrimSpace(s.DeploymentSurface); ds != "" {
		b.WriteString("\nDeployment: ")
		b.WriteString(ds)
	}
	return b.String()
}
