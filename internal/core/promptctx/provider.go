// Package promptctx: see doc.go for the overview.
package promptctx

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ProviderID is the stable kebab/snake-case identifier of a context
// provider. Persona TOML configs reference providers by this ID; the
// set below is the v1 seed catalog (gm-eiw). Custom providers MAY be
// registered under additional IDs — the registry accepts any string
// that does not collide with an existing entry.
type ProviderID string

// Seed provider IDs (gm-eiw). Each corresponds to one slice of
// workspace context that a Persona may opt into.
const (
	// ProviderProjectSummary — the high-level "what is this project"
	// narrative, Documentarian-maintained.
	ProviderProjectSummary ProviderID = "project_summary"
	// ProviderProjectState — current epic status, in-flight beads,
	// sprint posture.
	ProviderProjectState ProviderID = "project_state"
	// ProviderDecisionsLog — accumulating log of ratified design
	// decisions with citations.
	ProviderDecisionsLog ProviderID = "decisions_log"
	// ProviderIssuesLog — recent bugs, escalations, failures.
	ProviderIssuesLog ProviderID = "issues_log"
	// ProviderBeadDatabase — per-repo bead snapshot. A project may
	// span multiple repos each with its own bead DB; the Config's
	// Options carry repo scope.
	ProviderBeadDatabase ProviderID = "bead_database"
	// ProviderRecentCommits — git log tail for the relevant repos.
	ProviderRecentCommits ProviderID = "recent_commits"
	// ProviderCIStatus — latest CI run results.
	ProviderCIStatus ProviderID = "ci_status"
	// ProviderActiveEscalations — open EscalationRequest records.
	ProviderActiveEscalations ProviderID = "active_escalations"
	// ProviderSprintContext — sprint budget posture + burn thresholds.
	ProviderSprintContext ProviderID = "sprint_context"
)

// SeedProviderIDs is the v1 catalog, in the order personas SHOULD
// enumerate them in their context section when the order matters
// (project_summary first, sprint_context last — a rough narrative arc).
//
// Consumers MUST treat this list as advisory, not exhaustive — custom
// provider IDs are first-class and registry-driven.
var SeedProviderIDs = []ProviderID{
	ProviderProjectSummary,
	ProviderProjectState,
	ProviderDecisionsLog,
	ProviderIssuesLog,
	ProviderBeadDatabase,
	ProviderRecentCommits,
	ProviderCIStatus,
	ProviderActiveEscalations,
	ProviderSprintContext,
}

// Config is the per-persona configuration for a single context
// provider. It is the parsed form of a [context.<provider_id>] table in
// a .gemba/personas/<id>.toml file.
//
// Options is the provider-specific knobs (max_tokens, last_n, scope,
// …). Providers validate their own Options map; the registry does not.
type Config struct {
	ID      ProviderID     `json:"id"`
	Options map[string]any `json:"options,omitempty"`
}

// Validate checks that the config carries a non-empty ID. Provider-
// specific option validation happens inside each Provider.Refresh.
func (c Config) Validate() error {
	if c.ID == "" {
		return errors.New("promptctx: Config.ID must not be empty")
	}
	return nil
}

// Result is the materialized output of a Provider.Refresh call.
//
// Content is the prose that the PromptEnvelope splices into the final
// prompt. Payload is optional structured data for providers that want
// to offer both a narrative form and machine-readable fields (callers
// MAY persist Payload in the audit log for post-hoc inspection).
//
// FetchedAt + TTL + Stale together surface freshness: the audit log
// writes FetchedAt on every invocation; Stale is set by Cache.Invoke
// when the cached result's age has exceeded its TTL but a fresh fetch
// failed (so a degraded-but-usable value is returned).
type Result struct {
	ProviderID    ProviderID     `json:"provider_id"`
	Content       string         `json:"content"`
	Payload       map[string]any `json:"payload,omitempty"`
	FetchedAt     time.Time      `json:"fetched_at"`
	TTL           time.Duration  `json:"ttl"`
	Stale         bool           `json:"stale,omitempty"`
	TokenEstimate int            `json:"token_estimate,omitempty"`
}

// Provider is the interface every context provider implements. Refresh
// is expected to be idempotent from the caller's perspective — every
// call MAY hit the provider's backing source, so callers usually go
// through Cache.Invoke instead of calling Refresh directly.
//
// Implementations MUST:
//
//   - Return a Result whose ProviderID equals p.ID().
//   - Stamp FetchedAt on every successful Refresh.
//   - Populate TTL from their own defaults; the registry does not
//     override it.
//   - Validate their Options map inline and return a descriptive error
//     when it is malformed.
type Provider interface {
	ID() ProviderID
	Refresh(ctx context.Context, opts map[string]any) (*Result, error)
	TTL() time.Duration
}

// Registry is a lookup table of providers keyed by ProviderID. It is
// safe for concurrent reads AFTER all registrations complete; callers
// who register at runtime MUST guard with their own lock. The typical
// pattern is to register everything at startup and then treat the
// registry as immutable.
type Registry struct {
	providers map[ProviderID]Provider
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[ProviderID]Provider)}
}

// Register adds p to the registry. Returns an error when the ID is
// empty or when another provider is already registered under p.ID().
// Re-registering the same concrete provider (pointer equality) is a
// no-op; re-registering a different provider under the same ID fails,
// since silent overwrites are a frequent foot-gun when two seed
// packages both register a "decisions_log" provider.
func (r *Registry) Register(p Provider) error {
	if p == nil {
		return errors.New("promptctx: Register: provider is nil")
	}
	id := p.ID()
	if id == "" {
		return errors.New("promptctx: Register: provider ID is empty")
	}
	if existing, ok := r.providers[id]; ok {
		if existing == p {
			return nil
		}
		return fmt.Errorf("promptctx: Register: ID %q already registered", id)
	}
	r.providers[id] = p
	return nil
}

// MustRegister is Register with an error panic. Use only at init-time.
func (r *Registry) MustRegister(p Provider) {
	if err := r.Register(p); err != nil {
		panic(err)
	}
}

// Get returns the provider for id. The bool is false when id is not
// registered.
func (r *Registry) Get(id ProviderID) (Provider, bool) {
	p, ok := r.providers[id]
	return p, ok
}

// List returns the registered provider IDs in deterministic seed order:
// seed IDs that ARE registered come first (in SeedProviderIDs order),
// followed by any custom IDs in ascending string order. Callers use
// this to enumerate available providers for UI/config surfaces.
func (r *Registry) List() []ProviderID {
	out := make([]ProviderID, 0, len(r.providers))
	seen := make(map[ProviderID]struct{}, len(r.providers))
	for _, id := range SeedProviderIDs {
		if _, ok := r.providers[id]; ok {
			out = append(out, id)
			seen[id] = struct{}{}
		}
	}
	custom := make([]ProviderID, 0)
	for id := range r.providers {
		if _, ok := seen[id]; ok {
			continue
		}
		custom = append(custom, id)
	}
	sortProviderIDs(custom)
	out = append(out, custom...)
	return out
}

// sortProviderIDs sorts in ascending string order without pulling in
// sort.Slice on every List() call. The slices are bounded by the
// registration count, which is small.
func sortProviderIDs(ids []ProviderID) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j-1] > ids[j]; j-- {
			ids[j-1], ids[j] = ids[j], ids[j-1]
		}
	}
}
