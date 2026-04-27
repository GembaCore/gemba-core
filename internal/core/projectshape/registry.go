package projectshape

import (
	"errors"
	"fmt"
	"sort"
)

// ErrUnknownShape is returned by [Lookup] when the requested name
// has no registered shape. Wrapped errors carry the offending name
// in their text so the operator sees what they typed.
var ErrUnknownShape = errors.New("projectshape: unknown shape")

// SeedShapes returns the 5 architectural-archetype seed shapes
// gm-4uso ships. The returned slice is fresh on every call so a
// caller can mutate it without polluting the registry.
//
// The five seeds (in this order) are:
//
//  1. gemba — this workspace's shape: two-tier Go server + Vite SPA
//     on a single VPS (matches the bead's first seed verbatim).
//  2. two-tier-spa — generalisation of (1): SPA + backend API.
//  3. two-tier-ssr — server-rendered (Next/Remix/Rails/Django) plus
//     progressive-enhancement JS.
//  4. three-tier-with-edge — SPA → edge functions → origin db/services.
//  5. mobile-with-backend — native/RN/Flutter mobile + backend API +
//     push channel; mobile has its own release cycle.
//
// Names match the bead's seed enumeration verbatim so personas can
// reason about a stable identifier across consults.
func SeedShapes() []Shape {
	return []Shape{
		{
			Name:              "gemba",
			Description:       "Two-tier Go server plus a Vite SPA, deployed via goreleaser on a single VPS under systemd. Postgres for state. No edge functions, no third-tier cache. Beads database lives alongside the app.",
			Tiers:             []string{"vite spa", "go http api", "postgres"},
			RequestPath:       "browser → https → go api → postgres",
			Tooling:           []string{"vite", "go test", "go vet", "goreleaser", "systemd"},
			DeploymentSurface: "single VPS via systemd; goreleaser builds; manual rollback by re-deploying prior tag",
		},
		{
			Name:              "two-tier-spa",
			Description:       "Browser-side SPA plus a backend HTTP API. Single auth boundary; backend owns persistence; no edge tier. Generalises the gemba shape — examples include most internal tools.",
			Tiers:             []string{"spa frontend", "http api backend", "database"},
			RequestPath:       "browser → https → api → database",
			Tooling:           []string{"vite or next-csr or vue or svelte", "any backend language", "typical RDBMS"},
			DeploymentSurface: "container or VPS; one auth boundary at the api edge",
		},
		{
			Name:              "two-tier-ssr",
			Description:       "Server-rendered (Next.js, Remix, Rails, Django, Laravel, Phoenix) returning HTML, with progressive-enhancement JS on top. No SPA bundler complexity. Examples: marketing sites, content-heavy apps.",
			Tiers:             []string{"ssr web server", "database"},
			RequestPath:       "browser → https → ssr server (renders html) → database",
			Tooling:           []string{"framework cli (next/remix/rails/django)", "framework test runner"},
			DeploymentSurface: "container or VPS; long-lived server processes; framework-driven asset pipeline",
		},
		{
			Name:              "three-tier-with-edge",
			Description:       "SPA fronted by edge functions (Cloudflare Workers, Vercel Functions, Lambda@Edge) that talk to origin services and a database. Edge handles latency-sensitive routes; origin holds state. Examples: SaaS products with global users.",
			Tiers:             []string{"spa frontend", "edge functions", "origin api or services", "database"},
			RequestPath:       "browser → cdn → edge function → origin api → database",
			Tooling:           []string{"vite or next", "wrangler or vercel cli", "infrastructure-as-code for origin"},
			DeploymentSurface: "edge platform (cloudflare/vercel/aws) plus origin cluster; multi-tier rollouts; staged cache invalidation",
		},
		{
			Name:              "mobile-with-backend",
			Description:       "Native or RN/Flutter mobile app, a backend HTTP API, and a push channel. Mobile has its own release cycle (App Store / Play Store) decoupled from backend deploys. Often a third tier for auth/identity. Examples: consumer mobile products.",
			Tiers:             []string{"mobile client", "http api backend", "push channel", "auth/identity provider"},
			RequestPath:       "mobile app → https → api → database; push: backend → push provider → device",
			Tooling:           []string{"xcode or android studio or expo or flutter", "fastlane", "backend test runner", "push sdk"},
			DeploymentSurface: "store-mediated mobile releases (decoupled cadence); backend on container/VPS/serverless; auth/identity via dedicated provider",
		},
	}
}

// Registry is the in-memory lookup table for named shapes. Built
// once at startup (typically via [DefaultRegistry]) and treated as
// read-only thereafter. Methods are safe for concurrent reads;
// Register is not concurrent-safe, mirroring the persona registry
// pattern.
type Registry struct {
	shapes map[string]Shape
}

// NewRegistry returns an empty Registry. Most callers want
// [DefaultRegistry] instead — it ships pre-populated with the 5
// seed shapes.
func NewRegistry() *Registry {
	return &Registry{shapes: make(map[string]Shape)}
}

// DefaultRegistry returns a Registry pre-populated with the 5 seed
// shapes from [SeedShapes]. The returned registry is independent of
// any other registry the caller already holds — mutations do not
// leak across calls.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	for _, s := range SeedShapes() {
		// Programmatic registration: the seeds are vetted at compile
		// time, so a duplicate here is a bug-grade failure (only
		// possible if SeedShapes ships two entries with the same
		// name). Panic so a regression surfaces in the unit tests
		// rather than silently dropping a seed.
		if err := r.Register(s); err != nil {
			panic(fmt.Sprintf("projectshape: seed registry: %v", err))
		}
	}
	return r
}

// Register adds s to the registry. Returns an error when s.Name is
// empty or another shape is already registered under that name.
func (r *Registry) Register(s Shape) error {
	if s.Name == "" {
		return errors.New("projectshape: Register: shape name is empty")
	}
	if _, exists := r.shapes[s.Name]; exists {
		return fmt.Errorf("projectshape: Register: name %q already registered", s.Name)
	}
	r.shapes[s.Name] = s
	return nil
}

// Lookup returns the shape registered under name. The bool is
// false when no shape is registered under that name. Callers that
// want a typed error (e.g. config validation) should call
// [Registry.Resolve] instead.
func (r *Registry) Lookup(name string) (Shape, bool) {
	s, ok := r.shapes[name]
	return s, ok
}

// Resolve returns the shape registered under name, or wraps
// [ErrUnknownShape] when the name is not registered. Used by the
// config loader to surface a clean operator-facing error on a
// misspelled `active` value.
func (r *Registry) Resolve(name string) (Shape, error) {
	s, ok := r.shapes[name]
	if !ok {
		return Shape{}, fmt.Errorf("%w: %q", ErrUnknownShape, name)
	}
	return s, nil
}

// Names returns the registered shape names in ascending order. The
// `gemba shape list` CLI surface (deferred follow-up bead) consumes
// this to render its menu.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.shapes))
	for name := range r.shapes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// LookupOrGeneric returns the shape registered under name, or a
// zero-value Shape when name is empty or unknown. The dispatcher
// uses this on the no-shape-configured fallback path: a zero-value
// Shape's Render() returns the empty string, so the project-values
// layer simply contributes nothing — exactly what gm-4uso requires
// for a workspace that hasn't pinned a shape yet.
func (r *Registry) LookupOrGeneric(name string) Shape {
	if name == "" {
		return Shape{}
	}
	s, ok := r.shapes[name]
	if !ok {
		return Shape{}
	}
	return s
}
