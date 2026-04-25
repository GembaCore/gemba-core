package persona

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Variety classifies the persona's authority over workspace state.
// Per gm-2yg, every persona is either a Coach (advice-only — surfaces
// SuggestedActions the operator must apply) or a Manager (may directly
// mutate within its declared scope).
type Variety string

const (
	// VarietyCoach — the persona produces advice and SuggestedActions
	// only; the operator (or another authorized actor) applies them.
	// This is the default when a TOML file omits `variety`.
	VarietyCoach Variety = "coach"
	// VarietyManager — the persona may directly mutate state within
	// its declared mutation_scope. Every mutation is recorded as a
	// PersonaConsultRecord with a diff attached.
	VarietyManager Variety = "manager"
)

// Validate checks that v is one of the known varieties. The empty
// string is rejected — callers should normalize via Persona.normalize
// before validation.
func (v Variety) Validate() error {
	switch v {
	case VarietyCoach, VarietyManager:
		return nil
	default:
		return fmt.Errorf("persona: unknown variety %q", v)
	}
}

// ScopeKind names the breadth of a persona's reach across the
// repositories of its workspace (gm-k2jn / gm-26n4). Required on every
// persona file — the loader rejects a missing scope so a misconfigured
// Manager cannot silently span repos it has no business touching.
type ScopeKind string

const (
	// ScopeProject — the persona consults across all repositories of
	// the workspace. cwd at spawn = the project root (read-only by
	// default; Manager mutation_scope.paths bind to this root). The PM
	// is the canonical project-scope persona.
	ScopeProject ScopeKind = "project"

	// ScopeRepository — the persona is pinned to one repository named
	// in [PersonaScope.RepositoryID]. cwd at spawn = that repo's
	// primary worktree. The SPA surfaces the persona only in views
	// that already have repository context (bead detail, repo-scoped
	// Plan view, file diff).
	ScopeRepository ScopeKind = "repository"

	// ScopeAny — the caller picks the repository per consult. The SPA
	// renders these in a generic picker with a required repo selector
	// before invocation; never surfaces them context-free.
	ScopeAny ScopeKind = "any"
)

// PersonaScope declares where in the workspace a persona reaches. It
// is a required block on every persona TOML — the load path fails on
// omission so a misconfigured persona cannot silently span repos it
// has no business touching. Named PersonaScope (not just Scope) to
// avoid collision with [MutationScope] on the same struct.
type PersonaScope struct {
	// Kind is "project", "repository", or "any". Required.
	Kind ScopeKind `toml:"kind" json:"kind"`

	// RepositoryID is the repo the persona is pinned to. Required iff
	// Kind == ScopeRepository; forbidden otherwise.
	RepositoryID core.RepositoryID `toml:"repository,omitempty" json:"repository,omitempty"`

	// AdditionalReadPaths extends the default read surface for every
	// consult invoking this persona (gm-v8vr). Glob-style patterns;
	// add to the bead-level [core.WorkItem.AdditionalReadPaths]
	// rather than replacing it — the resolver concatenates and
	// de-duplicates. Use sparingly: a "Tests Coach" that legitimately
	// needs to grep across all workspaces; a "Security Auditor" that
	// must read `~/.ssh/known_hosts`. The cwd-constraint design
	// (gm-v8vr) prefers per-bead overrides over per-persona ones —
	// most needs are situational, not categorical.
	AdditionalReadPaths []string `toml:"additional_read_paths,omitempty" json:"additional_read_paths,omitempty"`
}

// IsZero reports whether the scope is the zero value. Used by tests
// that construct a Persona programmatically; production loads always
// populate Kind because Validate rejects empty.
func (s PersonaScope) IsZero() bool {
	return s.Kind == "" && s.RepositoryID == ""
}

// ResolveWorkingDir returns the absolute filesystem path the
// dispatcher should spawn a Claude Code session in for this scope
// (gm-k2jn / gm-26n4 / gm-twp2). The resolver implements the cwd
// policy locked on 2026-04-25:
//
//   - ScopeProject    → workspaceDir (the directory containing .gemba/).
//     The persona is told via preamble that it has no source repo;
//     promptctx providers carry the cross-repo state.
//   - ScopeRepository → repos.Get(s.RepositoryID).Path. Spawning fails
//     if the repo is not registered.
//   - ScopeAny        → callerOverride (must be non-nil + registered).
//     The HTTP layer requires a `repo` query parameter on consults
//     to a scope=any persona; this method is where that parameter
//     binds.
//
// Returns an error rather than panicking on missing/invalid input
// so the dispatcher can surface a 400 to the caller cleanly.
func (s PersonaScope) ResolveWorkingDir(workspaceDir string, repos *core.RepositoryRegistry, callerOverride core.RepositoryID) (string, error) {
	if strings.TrimSpace(workspaceDir) == "" {
		return "", fmt.Errorf("persona/scope: workspaceDir must not be empty")
	}
	switch s.Kind {
	case ScopeProject:
		return workspaceDir, nil
	case ScopeRepository:
		if repos == nil {
			return "", fmt.Errorf("persona/scope: repository registry required for scope=repository")
		}
		repo, ok := repos.Get(s.RepositoryID)
		if !ok {
			return "", fmt.Errorf("persona/scope: repository %q not registered", s.RepositoryID)
		}
		return repo.Path, nil
	case ScopeAny:
		if strings.TrimSpace(string(callerOverride)) == "" {
			return "", fmt.Errorf("persona/scope: scope=any requires a repository override from the caller")
		}
		if repos == nil {
			return "", fmt.Errorf("persona/scope: repository registry required for scope=any")
		}
		repo, ok := repos.Get(callerOverride)
		if !ok {
			return "", fmt.Errorf("persona/scope: repository override %q not registered", callerOverride)
		}
		return repo.Path, nil
	default:
		return "", fmt.Errorf("persona/scope: unknown kind %q", s.Kind)
	}
}

// Validate checks that the scope is well-formed:
//   - Kind required and ∈ {project, repository, any}
//   - RepositoryID required iff Kind == repository
//   - RepositoryID forbidden when Kind != repository (catches typos)
func (s PersonaScope) Validate() error {
	switch s.Kind {
	case "":
		return fmt.Errorf("persona: scope.kind is required (project | repository | any)")
	case ScopeProject, ScopeAny:
		if s.RepositoryID != "" {
			return fmt.Errorf("persona: scope.repository must be empty when scope.kind=%q", s.Kind)
		}
		return nil
	case ScopeRepository:
		if strings.TrimSpace(string(s.RepositoryID)) == "" {
			return fmt.Errorf("persona: scope.repository required when scope.kind=%q", s.Kind)
		}
		return nil
	default:
		return fmt.Errorf("persona: unknown scope.kind %q (want project | repository | any)", s.Kind)
	}
}

// Persona is the parsed form of a `.gemba/personas/<id>.toml` file.
// Field tags drive both TOML and JSON encoding; the struct is
// deliberately flat-with-substructs so user-authored files stay
// readable.
type Persona struct {
	// ID is the kebab-case identifier; it MUST match the filename
	// stem (e.g. `project-manager.toml` → `project-manager`). The
	// loader enforces this so registry lookups and audit-log writes
	// stay anchored to a single name per persona.
	ID string `toml:"id" json:"id"`

	// Name is the short display label ("PM", "Docs"). Not required to
	// be unique; the UI uses it for chips and the audit log.
	Name string `toml:"name" json:"name"`

	// Role is the human-readable role string ("Project Manager"). The
	// system_prompt template substitutes `{{role}}` from this field.
	Role string `toml:"role" json:"role"`

	// Variety is Coach or Manager. Empty defaults to Coach via
	// normalize so a minimal TOML file is still valid.
	Variety Variety `toml:"variety" json:"variety"`

	// Scope declares the persona's reach across workspace repositories
	// (gm-k2jn). REQUIRED on every persona file — the loader rejects
	// omission. The dispatcher reads Scope.Kind to pick the working
	// directory at spawn time; the SPA reads it to decide where to
	// surface the persona contextually vs globally.
	Scope PersonaScope `toml:"scope" json:"scope"`

	// Description is one-paragraph free-form copy shown in the
	// persona picker. No length cap; the UI clips for display.
	Description string `toml:"description" json:"description"`

	// Icon is an emoji or single grapheme used as the visual marker
	// in the UI. Optional.
	Icon string `toml:"icon" json:"icon,omitempty"`

	// Skills lists the Skill IDs this persona ships with. The Skill
	// registry is the source of truth for what the IDs resolve to;
	// the loader does NOT validate that every entry is registered
	// (registration order varies; the dispatcher checks at call time).
	Skills []string `toml:"skills" json:"skills,omitempty"`

	// MutationAuthority lists the named authority kinds this persona
	// holds (e.g. "documentation_edit"). Meaningful only when Variety
	// is Manager; ignored for Coaches.
	MutationAuthority []string `toml:"mutation_authority" json:"mutation_authority,omitempty"`

	// MutationScope bounds the file paths a Manager-variety persona
	// may touch. Outside scope, even a Manager behaves like a Coach.
	MutationScope MutationScope `toml:"mutation_scope" json:"mutation_scope,omitzero"`

	// Model is the LLM provider/model this persona consults. Required;
	// the loader rejects a Persona whose Model.Vendor or Model.Model
	// is empty so we never silently fall through to a default.
	Model ModelConfig `toml:"model" json:"model"`

	// BudgetPolicy bounds per-invocation and per-day spend. The
	// dispatcher enforces these caps before the provider call fires;
	// they're configuration, not advisory.
	BudgetPolicy BudgetPolicy `toml:"budget_policy" json:"budget_policy"`

	// ContextProviders declares which promptctx providers this persona
	// reads from and (optionally) maintains. The loader does not
	// resolve these — the prompt-composition layer does, since
	// provider availability varies by workspace.
	ContextProviders ContextProvidersConfig `toml:"context_providers" json:"context_providers,omitzero"`

	// Pairings maps named hooks (e.g. "pm_add_to_backlog") to a
	// skill ID this persona contributes when the hook fires. Used by
	// the orchestrator to weave personas together; opaque to the
	// loader.
	Pairings map[string]string `toml:"pairings" json:"pairings,omitempty"`

	// SystemPrompt is the persona's role-specific system prompt. May
	// contain `{{template}}` placeholders that the dispatcher
	// substitutes at consult time (workspace_name, role, etc.).
	SystemPrompt string `toml:"system_prompt" json:"system_prompt"`

	// sourcePath is the filesystem path the persona was loaded from,
	// or "" when constructed programmatically (tests). Unexported so
	// it never leaks into the wire format; surfaced via [Registry.SourcePath].
	sourcePath string `toml:"-" json:"-"`
}

// MutationScope bounds where a Manager-variety persona may mutate.
// Today only Paths is supported; future axes (label patterns, bead
// types, table names) will land here additively.
type MutationScope struct {
	// Paths is a list of glob patterns (doublestar-style). A path is
	// in scope if at least one pattern matches.
	Paths []string `toml:"paths" json:"paths,omitempty"`
}

// IsZero reports whether the scope is empty. Used by `omitzero` JSON
// tags to keep round-tripped persona files clean.
func (m MutationScope) IsZero() bool { return len(m.Paths) == 0 }

// ModelConfig identifies the provider+model the persona calls. Vendor
// is the registry key the provider router uses; Model is whatever
// string that vendor accepts.
type ModelConfig struct {
	Vendor      string  `toml:"vendor" json:"vendor"`
	Model       string  `toml:"model" json:"model"`
	MaxTokens   int     `toml:"max_tokens" json:"max_tokens,omitempty"`
	Temperature float64 `toml:"temperature" json:"temperature,omitempty"`
}

// BudgetPolicy is the per-persona cost envelope. CountsAgainstSprint
// determines whether invocations of this persona consume the active
// sprint's token budget; the dispatcher reads it on every consult.
type BudgetPolicy struct {
	CountsAgainstSprint     bool    `toml:"counts_against_sprint" json:"counts_against_sprint"`
	MaxPerInvocationDollars float64 `toml:"max_per_invocation_dollars" json:"max_per_invocation_dollars,omitempty"`
	MaxPerDayDollars        float64 `toml:"max_per_day_dollars" json:"max_per_day_dollars,omitempty"`
}

// ContextProvidersConfig links a persona to the promptctx providers
// it uses. Maintains is the set the persona refreshes (Manager-only);
// Reads is the set whose Result is spliced into the prompt's context
// layer at consult time.
type ContextProvidersConfig struct {
	Maintains []string `toml:"maintains" json:"maintains,omitempty"`
	Reads     []string `toml:"reads" json:"reads,omitempty"`
}

// IsZero reports whether the config is empty.
func (c ContextProvidersConfig) IsZero() bool {
	return len(c.Maintains) == 0 && len(c.Reads) == 0
}

// normalize fills in defaults that make a sparse TOML file valid.
// Currently: Variety defaults to Coach. Called by the loader after
// decode and before Validate; tests and external callers should call
// it explicitly when constructing a Persona in code.
func (p *Persona) normalize() {
	if p.Variety == "" {
		p.Variety = VarietyCoach
	}
}

// Validate checks the structural invariants the loader and registry
// rely on. It does NOT validate the system_prompt content or skill
// availability — both are the dispatcher's job.
func (p Persona) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("persona: id must not be empty")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("persona %q: name must not be empty", p.ID)
	}
	if strings.TrimSpace(p.Role) == "" {
		return fmt.Errorf("persona %q: role must not be empty", p.ID)
	}
	if err := p.Variety.Validate(); err != nil {
		return fmt.Errorf("persona %q: %w", p.ID, err)
	}
	if err := p.Scope.Validate(); err != nil {
		return fmt.Errorf("persona %q: %w", p.ID, err)
	}
	if p.Variety == VarietyManager && p.Scope.Kind == ScopeAny {
		// A Manager mutates state. Without a repo (or project) anchor
		// for [MutationScope.Paths], the mutation cannot be bounded
		// safely. Coaches with scope=any are fine — they propose only.
		return fmt.Errorf("persona %q: manager variety must not declare scope.kind=any (mutation must bind to project or named repository)", p.ID)
	}
	if strings.TrimSpace(p.Model.Vendor) == "" {
		return fmt.Errorf("persona %q: model.vendor must not be empty", p.ID)
	}
	if strings.TrimSpace(p.Model.Model) == "" {
		return fmt.Errorf("persona %q: model.model must not be empty", p.ID)
	}
	if strings.TrimSpace(p.SystemPrompt) == "" {
		return fmt.Errorf("persona %q: system_prompt must not be empty", p.ID)
	}
	if p.Variety == VarietyCoach && len(p.MutationAuthority) > 0 {
		return fmt.Errorf("persona %q: coach variety must not declare mutation_authority", p.ID)
	}
	if p.Variety == VarietyManager && len(p.MutationAuthority) == 0 {
		return fmt.Errorf("persona %q: manager variety must declare at least one mutation_authority", p.ID)
	}
	return nil
}

// SkillRequest is the inbound shape for a persona consult. Input is
// the per-skill request body, validated by the Skill at dispatch
// time. The dispatcher composes this from the HTTP `/api/v1/consult`
// payload.
type SkillRequest struct {
	SkillID     string             `json:"skill_id"`
	Workspace   string             `json:"workspace"`
	Input       json.RawMessage    `json:"input"`
	Guidance    string             `json:"guidance,omitempty"`
	Constraints SkillConstraints   `json:"constraints,omitzero"`
}

// SkillConstraints carries caller-supplied limits the dispatcher
// honors before the provider call fires. Zero values mean "no
// constraint"; the persona's BudgetPolicy still applies on top.
type SkillConstraints struct {
	MaxDollars     float64 `json:"max_dollars,omitempty"`
	MaxLatencySecs int     `json:"max_latency_seconds,omitempty"`
	RequiredModel  string  `json:"required_model,omitempty"`
	MinConfidence  float64 `json:"min_confidence,omitempty"`
}

// IsZero reports whether the constraints are unset, so JSON encoding
// can omit the empty struct.
func (c SkillConstraints) IsZero() bool { return c == SkillConstraints{} }

// SkillResponse is the outbound shape from a consult. Lines is the
// JSONL stream — already validated against the Skill's output schema.
// The dispatcher is responsible for streaming; this struct is the
// fully-buffered form used by the audit log and non-streaming
// consumers.
type SkillResponse struct {
	Lines     []json.RawMessage `json:"lines"`
	Tokens    TokenUsage        `json:"tokens"`
	Dollars   float64           `json:"dollars"`
	LatencyMs int               `json:"latency_ms"`
	Model     string            `json:"model"`
}

// TokenUsage is the input/output token split returned by the model
// provider. Costs are derived in the dispatcher, not here, so a
// vendor swap doesn't require updating every call site.
type TokenUsage struct {
	In  int `json:"in"`
	Out int `json:"out"`
}

// PersonaConsultRecord is one row in the append-only consult audit
// log. Every consult — whether the operator applied a SuggestedAction
// or not — produces exactly one record.
type PersonaConsultRecord struct {
	ID          string          `json:"id"`
	PersonaID   string          `json:"persona_id"`
	SkillID     string          `json:"skill_id"`
	Workspace   string          `json:"workspace"`
	StartedAt   time.Time       `json:"started_at"`
	EndedAt     time.Time       `json:"ended_at"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response,omitempty"`
	Tokens      TokenUsage      `json:"tokens"`
	Dollars     float64         `json:"dollars"`
	Model       string          `json:"model"`
	LatencyMs   int             `json:"latency_ms"`
	AppliedIdx  []int           `json:"applied_idx,omitempty"`
	Error       string          `json:"error,omitempty"`
}
