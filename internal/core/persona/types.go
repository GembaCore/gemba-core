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

	// Personality is the tonal flavor injected into this persona's
	// system prompt (gm-9rv §1). Optional — the zero value means
	// "no personality declared". Decorative only (invariant #27);
	// safe to swap without affecting correctness.
	Personality Personality `toml:"personality" json:"personality,omitzero"`

	// Perspective is the deterministic lens this persona always
	// applies (gm-9rv §2). Optional — the zero value means "no
	// perspective declared". Perspectives never gate state
	// transitions (invariant #28).
	Perspective Perspective `toml:"perspective" json:"perspective,omitzero"`

	// Purview declares the domain in which this persona has gate
	// authority and the phases that authority is active in
	// (gm-9rv §3). Optional — the zero value means "no purview
	// declared" (the persona is advisory-only everywhere). The
	// Manager-mutation-path binding lands with gm-3on; this bead
	// delivers the type + config surface only.
	Purview Purview `toml:"purview" json:"purview,omitzero"`

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

	// Output declares how the persona's response is shaped and who can
	// see past invocations. Conflating this with SystemPrompt makes
	// persona config noisy and unsafe — the prompt is about *what to
	// think*, validation is about *how the response is shaped*, and
	// sharing is about *who can see past invocations* (gm-lq1).
	//
	// Optional: the zero value means "use system defaults". Runtime
	// consumers (PromptEnvelope wiring, PersonaConsultRecord recording,
	// /insights/personas gating) land in follow-up beads.
	Output OutputPolicy `toml:"output" json:"output,omitzero"`

	// sourcePath is the filesystem path the persona was loaded from,
	// or "" when constructed programmatically (tests). Unexported so
	// it never leaks into the wire format; surfaced via [Registry.SourcePath].
	sourcePath string `toml:"-" json:"-"`
}

// Personality is the tonal flavor injected into a persona's system
// prompt as voice/tone qualifiers (gm-9rv). Personality is decorative
// only — invariant #27: two personas with identical Perspective +
// Purview but different Personalities produce equivalent
// correctness-affecting output. Safe to experiment with.
//
// All fields are optional; the zero value means "no personality
// declared" and the prompt layer renders nothing for it. The actual
// injection of Personality into the system prompt lands with gm-3on
// (Purview-as-gate binding); this bead delivers only the type + TOML
// surface + API surfacing.
type Personality struct {
	// ID is the canonical identifier for this personality flavor
	// (e.g. "laconic", "warm", "wry"). Free-form so workspaces can
	// author bespoke personalities; the loader does not enumerate.
	ID string `toml:"id" json:"id,omitempty"`

	// Description is the prompt-injected voice qualifier — appended
	// to the system prompt as `Voice: {description}` by the prompt
	// layer (gm-3on owns the actual injection).
	Description string `toml:"description" json:"description,omitempty"`

	// Examples are optional few-shot anchors illustrating the voice.
	// Currently advisory; future skills may splice them.
	Examples []string `toml:"examples,omitempty" json:"examples,omitempty"`
}

// IsZero reports whether the personality is the zero value. Used by
// JSON `omitzero` callers and the registry test fixtures.
func (p Personality) IsZero() bool {
	return p.ID == "" && p.Description == "" && len(p.Examples) == 0
}

// VolunteerMode controls when a persona's Perspective surfaces an
// inline comment without being explicitly consulted (gm-9rv §2.
// Perspective). Invariant #28: Perspectives never block — they only
// produce comments.
type VolunteerMode string

const (
	// VolunteerNever — only speaks when explicitly consulted.
	VolunteerNever VolunteerMode = "never"
	// VolunteerOnDemand — speaks when the operator asks
	// "any perspectives?" on a piece of work.
	VolunteerOnDemand VolunteerMode = "on_demand"
	// VolunteerOnTrigger — speaks when one of [Perspective.Triggers]
	// matches the active context (label, area, edge pattern, …).
	VolunteerOnTrigger VolunteerMode = "on_trigger"
	// VolunteerAlways — speaks every turn. Use sparingly.
	VolunteerAlways VolunteerMode = "always"
)

// Validate checks that v is one of the known volunteer modes. The
// empty string is treated as "unset" by callers (it normalizes to
// VolunteerNever in [Persona.normalize]) and is rejected here so a
// typoed mode doesn't silently fall through to "never".
func (v VolunteerMode) Validate() error {
	switch v {
	case VolunteerNever, VolunteerOnDemand, VolunteerOnTrigger, VolunteerAlways:
		return nil
	default:
		return fmt.Errorf("persona: unknown volunteer_mode %q (want never | on_demand | on_trigger | always)", string(v))
	}
}

// Perspective is the deterministic lens a persona always applies when
// consulted (gm-9rv §2). Every persona has at most one Perspective;
// the zero value means "no perspective declared".
//
// Perspectives never gate state transitions. A persona that wants
// blocking authority needs a Purview with PurviewBlocking authority
// in an active phase; without it, opinions are advisory-only
// (invariant #28).
type Perspective struct {
	// Statement is the lens, in human-readable form. E.g.
	// "design integrity, module boundaries, type-system cleanness,
	// coupling cost".
	Statement string `toml:"statement" json:"statement,omitempty"`

	// Triggers are label-or-signal patterns that should surface a
	// volunteered comment (used when VolunteerMode == on_trigger).
	// Free-form — the trigger matcher (gm-perspective-volunteer) is
	// the source of truth for the pattern grammar.
	Triggers []string `toml:"triggers,omitempty" json:"triggers,omitempty"`

	// VolunteerMode controls when this persona speaks unbidden. The
	// zero value normalizes to VolunteerNever via [Persona.normalize].
	VolunteerMode VolunteerMode `toml:"volunteer_mode" json:"volunteer_mode,omitempty"`

	// CostTier names the model tier the dispatcher should route a
	// volunteered comment through. Free-form so cost tiers can shift
	// without a schema bump; the design doc names "haiku" (default
	// for volunteered) | "sonnet" | "opus" (explicit consult).
	CostTier string `toml:"cost_tier,omitempty" json:"cost_tier,omitempty"`
}

// IsZero reports whether the perspective is the zero value.
func (p Perspective) IsZero() bool {
	return p.Statement == "" && len(p.Triggers) == 0 && p.VolunteerMode == "" && p.CostTier == ""
}

// Phase names a project-level mode that determines which Purviews are
// active (gm-9rv §3). The Phase primitive proper lands with gm-jt9 —
// this bead defines the type so [Purview.ActivePhases] has a typed
// slot but does NOT enumerate or validate phase values. Treat the
// type as opaque-string until gm-jt9 ratifies the canonical enum.
type Phase string

// PurviewAuthority is the gate strength a Purview carries while its
// phase is active (gm-9rv §3).
type PurviewAuthority string

const (
	// PurviewAdvisory — always a Coach; strong opinions, never
	// blocks. Documentarian-style.
	PurviewAdvisory PurviewAuthority = "advisory"
	// PurviewStrong — can block with persona consensus + user
	// override. Coaches cap here.
	PurviewStrong PurviewAuthority = "strong"
	// PurviewBlocking — hard-block in active phases; override
	// requires nonce + justification. Manager-only.
	PurviewBlocking PurviewAuthority = "blocking"
)

// Validate checks that a is one of the known authority tiers. The
// empty string is treated as "unset" by callers and rejected here.
func (a PurviewAuthority) Validate() error {
	switch a {
	case PurviewAdvisory, PurviewStrong, PurviewBlocking:
		return nil
	default:
		return fmt.Errorf("persona: unknown blocking_authority %q (want advisory | strong | blocking)", string(a))
	}
}

// Purview is the domain in which a persona has gate authority
// (gm-9rv §3). Purviews are phase-conditional (invariant #29):
// dormant outside their active phases, binding (up to blocking)
// during them. The zero value means "no purview declared" — the
// persona has no gate authority anywhere.
//
// The Manager-mutation-path binding (Purview-as-gate) lands with
// gm-3on; this bead delivers only the type + TOML round-trip + API
// surface. The Project Phase primitive that drives ActivePhases is
// owned by gm-jt9.
type Purview struct {
	// Domain names the gated area, free-form so workspaces can
	// declare custom domains. The design doc names: design | testing
	// | auth | release | copy | docs | code | …
	Domain string `toml:"domain" json:"domain,omitempty"`

	// ActivePhases lists the project phases in which this Purview
	// is binding. Outside these phases the Purview is dormant —
	// equivalent to no Purview at all. Free-form Phase strings
	// pending gm-jt9's enum ratification.
	ActivePhases []Phase `toml:"active_phases,omitempty" json:"active_phases,omitempty"`

	// BlockingAuthority is the gate strength while ActivePhases
	// includes the project's current phase. The zero value
	// normalizes to PurviewAdvisory via [Persona.normalize].
	BlockingAuthority PurviewAuthority `toml:"blocking_authority" json:"blocking_authority,omitempty"`
}

// IsZero reports whether the purview is the zero value.
func (p Purview) IsZero() bool {
	return p.Domain == "" && len(p.ActivePhases) == 0 && p.BlockingAuthority == ""
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

// OutputValidation names how a persona's response is structurally
// validated before it is recorded or surfaced (gm-lq1). Empty means
// "use system default" — the loader treats zero as valid.
type OutputValidation string

const (
	// OutputValidationJSONLSchema — each line is a JSON object
	// validated against the Skill's schema_ref.
	OutputValidationJSONLSchema OutputValidation = "jsonl_schema"
	// OutputValidationMarkdownStructural — markdown with a structural
	// shape (headings, lists) the consumer parses.
	OutputValidationMarkdownStructural OutputValidation = "markdown_structural"
	// OutputValidationJSONObject — a single JSON object validated
	// against the Skill's schema_ref.
	OutputValidationJSONObject OutputValidation = "json_object"
	// OutputValidationFreeForm — opaque string; no structural
	// validation applied.
	OutputValidationFreeForm OutputValidation = "free_form"
)

// Validate checks v is one of the known validations or empty (which
// means "use system default").
func (v OutputValidation) Validate() error {
	switch v {
	case "", OutputValidationJSONLSchema, OutputValidationMarkdownStructural,
		OutputValidationJSONObject, OutputValidationFreeForm:
		return nil
	default:
		return fmt.Errorf("persona: unknown output.validation %q (want jsonl_schema | markdown_structural | json_object | free_form)", v)
	}
}

// OutputSharing names who can see past invocations of this persona
// via the /insights/personas surface (gm-lq1). Empty means "use
// system default" — the loader treats zero as valid.
type OutputSharing string

const (
	// OutputSharingAuditOnly — visible only to the audit log; not
	// surfaced in /insights/personas.
	OutputSharingAuditOnly OutputSharing = "audit_only"
	// OutputSharingPublic — visible to anyone with workspace access.
	OutputSharingPublic OutputSharing = "public"
	// OutputSharingTeam — visible to members of the persona's team.
	OutputSharingTeam OutputSharing = "team"
	// OutputSharingPrivate — visible only to the invoking operator.
	OutputSharingPrivate OutputSharing = "private"
)

// Validate checks s is one of the known sharing modes or empty.
func (s OutputSharing) Validate() error {
	switch s {
	case "", OutputSharingAuditOnly, OutputSharingPublic,
		OutputSharingTeam, OutputSharingPrivate:
		return nil
	default:
		return fmt.Errorf("persona: unknown output.sharing %q (want audit_only | public | team | private)", s)
	}
}

// OutputPolicy is the per-persona output validation + sharing config
// (gm-lq1). All fields are optional; the zero value means "use system
// defaults" for every dimension. Runtime consumers (PromptEnvelope,
// PersonaConsultRecord, /insights/personas) land in follow-up beads.
type OutputPolicy struct {
	// Validation declares the structural shape the response must take.
	// Empty defers to the system default for the persona's skills.
	Validation OutputValidation `toml:"validation" json:"validation,omitempty"`

	// SchemaRef points at the JSON Schema (or schema-like asset) the
	// validator consults. Free-form path/URL — the validator
	// dereferences at consult time.
	SchemaRef string `toml:"schema_ref" json:"schema_ref,omitempty"`

	// Sharing declares who can see past invocations of this persona.
	// Empty defers to the system default.
	Sharing OutputSharing `toml:"sharing" json:"sharing,omitempty"`

	// RetentionDays is the number of days past consults are retained
	// before purging. Zero means "use system default"; must be >= 0.
	RetentionDays int `toml:"retention_days" json:"retention_days,omitempty"`

	// RedactBeforeSharing names redactor categories applied to the
	// response before it surfaces beyond the audit log (e.g.
	// "api_keys", "internal_urls"). Opaque to the loader; the
	// /insights/personas layer dereferences these.
	RedactBeforeSharing []string `toml:"redact_before_sharing" json:"redact_before_sharing,omitempty"`
}

// IsZero reports whether the policy is empty. Used by `omitzero` JSON
// tags to keep round-tripped persona files clean.
func (o OutputPolicy) IsZero() bool {
	return o.Validation == "" &&
		o.SchemaRef == "" &&
		o.Sharing == "" &&
		o.RetentionDays == 0 &&
		len(o.RedactBeforeSharing) == 0
}

// Validate checks the output policy is well-formed. Empty enum values
// and zero RetentionDays are accepted — they mean "use system
// default". Negative RetentionDays is rejected.
func (o OutputPolicy) Validate() error {
	if err := o.Validation.Validate(); err != nil {
		return err
	}
	if err := o.Sharing.Validate(); err != nil {
		return err
	}
	if o.RetentionDays < 0 {
		return fmt.Errorf("persona: output.retention_days must be >= 0, got %d", o.RetentionDays)
	}
	return nil
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
//
// PPPP defaults (gm-9rv): a Perspective without a volunteer_mode
// defaults to "never" (the persona only speaks when consulted), and
// a Purview without a blocking_authority defaults to "advisory" (the
// safest cap for an under-specified purview). These defaults only
// apply when the operator declared the block at all — a fully
// omitted [perspective] / [purview] block stays the zero value.
func (p *Persona) normalize() {
	if p.Variety == "" {
		p.Variety = VarietyCoach
	}
	if !p.Perspective.IsZero() && p.Perspective.VolunteerMode == "" {
		p.Perspective.VolunteerMode = VolunteerNever
	}
	if !p.Purview.IsZero() && p.Purview.BlockingAuthority == "" {
		p.Purview.BlockingAuthority = PurviewAdvisory
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
	// PPPP enum validation (gm-9rv): only checked when the block was
	// declared at all — a fully zero-value Perspective / Purview is a
	// valid "no-op" for a persona that opts out of the new axes.
	if !p.Perspective.IsZero() {
		if err := p.Perspective.VolunteerMode.Validate(); err != nil {
			return fmt.Errorf("persona %q: perspective: %w", p.ID, err)
		}
	}
	if !p.Purview.IsZero() {
		if err := p.Purview.BlockingAuthority.Validate(); err != nil {
			return fmt.Errorf("persona %q: purview: %w", p.ID, err)
		}
	}
	if err := p.Output.Validate(); err != nil {
		return fmt.Errorf("persona %q: %w", p.ID, err)
	}
	return nil
}

// SkillRequest is the inbound shape for a persona consult. Input is
// the per-skill request body, validated by the Skill at dispatch
// time. The dispatcher composes this from the HTTP `/api/v1/consult`
// payload.
type SkillRequest struct {
	SkillID     string           `json:"skill_id"`
	Workspace   string           `json:"workspace"`
	Input       json.RawMessage  `json:"input"`
	Guidance    string           `json:"guidance,omitempty"`
	Constraints SkillConstraints `json:"constraints,omitzero"`
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
	ID         string          `json:"id"`
	PersonaID  string          `json:"persona_id"`
	SkillID    string          `json:"skill_id"`
	Workspace  string          `json:"workspace"`
	StartedAt  time.Time       `json:"started_at"`
	EndedAt    time.Time       `json:"ended_at"`
	Request    json.RawMessage `json:"request"`
	Response   json.RawMessage `json:"response,omitempty"`
	Tokens     TokenUsage      `json:"tokens"`
	Dollars    float64         `json:"dollars"`
	Model      string          `json:"model"`
	LatencyMs  int             `json:"latency_ms"`
	AppliedIdx []int           `json:"applied_idx,omitempty"`
	Error      string          `json:"error,omitempty"`
}
