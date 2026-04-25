package persona

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
