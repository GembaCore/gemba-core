// Package core: see doc.go for the overview.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is the sentinel error WorkPlane implementations return
// when a lookup id does not exist in the backend. Adaptors MAY wrap it
// with context (errors.Wrap / fmt.Errorf with %w) so long as
// errors.Is(err, ErrNotFound) still holds.
var ErrNotFound = errors.New("core: not found")

// ErrUnsupported is the sentinel error WorkPlane implementations return
// when the caller requests a feature group the manifest opts out of
// (for example ReadBudgetRollup on a non-budget-enforced adaptor). The
// UI uses errors.Is to decide whether to hide the control versus
// surface a fatal error.
var ErrUnsupported = errors.New("core: unsupported by adaptor")

// Transport names the wire protocol an adaptor uses to talk to the core
// (gm-root DD-12). Exactly one of these three values is valid per adaptor
// in v1; multi-transport adaptors are explicitly out of scope.
type Transport string

const (
	// TransportAPI — HTTP + JSON request/response.
	TransportAPI Transport = "api"
	// TransportJSONL — newline-delimited JSON over stdio (in-process or
	// subprocess).
	TransportJSONL Transport = "jsonl"
	// TransportMCP — Model Context Protocol. Recommended-not-required.
	TransportMCP Transport = "mcp"
)

// validTransports is the authoritative set used by Valid / UnmarshalJSON.
var validTransports = map[Transport]struct{}{
	TransportAPI:   {},
	TransportJSONL: {},
	TransportMCP:   {},
}

// Valid reports whether t is one of the three canonical transports.
func (t Transport) Valid() bool {
	_, ok := validTransports[t]
	return ok
}

// String satisfies fmt.Stringer and always returns the lowercase token.
func (t Transport) String() string { return string(t) }

// UnmarshalJSON rejects unknown transports at decode time so that a bad
// manifest fails at the adaptor boundary, not later when we try to route.
func (t *Transport) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	candidate := Transport(raw)
	if !candidate.Valid() {
		return fmt.Errorf("core: unknown Transport %q", raw)
	}
	*t = candidate
	return nil
}

// FieldExtension declares an adaptor-native field that core does not know
// about. The UI will only render it inside
// `web/src/extensions/<adaptor-id>/` (gm-root DD-4).
//
// Name is the JSON key the adaptor emits on WorkItem.Custom. Type is a
// free-form hint for the UI renderer ("string", "number", "duration",
// "url", "markdown", ...); the UI is responsible for mapping unknown
// types to a safe default widget.
type FieldExtension struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// EdgeExtension declares an adaptor-native relationship kind that is not
// one of the three core edges (gm-root DD-9). The capability-negotiation
// UI renders the extension's Name when the adaptor's own extension widget
// is loaded, and falls back to "relates_to" semantics otherwise.
//
// Inverse is the name of the extension edge that the adaptor emits when
// the same logical link is walked backwards ("blocks" ↔ "blocked_by").
// Leave empty when the edge is symmetric or has no defined inverse.
type EdgeExtension struct {
	Name        string `json:"name"`
	Directed    bool   `json:"directed"`
	Inverse     string `json:"inverse,omitempty"`
	Description string `json:"description,omitempty"`
}

// RelationshipExtension declares an adaptor-native field on the
// Relationship record itself (not a new edge kind). Use this when the
// adaptor carries metadata on every edge — Jira link categories, beads
// edge confidence scores, LangGraph data-flow contracts — that the UI
// extension may want to render alongside the core edge.
type RelationshipExtension struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// StateMap is the adaptor's declared translation from its native status
// tokens to the five core StateCategory buckets. Every native status the
// adaptor can emit must appear as a key; missing keys surface as
// conformance failures and force the UI onto an unknown-state fallback.
//
// The map is declarative: core does not attempt to infer categories from
// status names. This keeps lane placement deterministic and keeps the
// UI free of adaptor-specific vocabulary (gm-root DD-4).
type StateMap map[string]StateCategory

// Validate reports the first native status whose target bucket is not a
// valid StateCategory. Adaptors should run this inside their init path
// so a malformed map fails at startup rather than at first query.
func (m StateMap) Validate() error {
	for native, bucket := range m {
		if !bucket.Valid() {
			return fmt.Errorf(
				"core: StateMap[%q] = %q is not a valid StateCategory",
				native, bucket)
		}
	}
	return nil
}

// CapabilityManifest is the declarative description every WorkPlane
// adaptor returns from Describe. It tells core (and the UI) which
// transport the adaptor speaks, how to normalize its native statuses,
// what extensions it carries beyond the core primitives, and which
// optional feature groups it opts into.
//
// The manifest is the single source of truth the capability-negotiation
// UI consults before rendering adaptor-specific controls (gm-e11.4 /
// gm-root DD-15): controls for unsupported capabilities are hidden, not
// disabled, so the operator never sees a button they can't use.
type CapabilityManifest struct {
	AdaptorName    string `json:"adaptor_name"`
	AdaptorVersion string `json:"adaptor_version"`
	// ProtocolVersion is the core contract version the adaptor was built
	// against. Version negotiation (gm-e3.4) compares this to the core's
	// advertised core_version and fails startup on mismatch.
	ProtocolVersion string `json:"protocol_version"`

	// Transport is the wire protocol the adaptor speaks. Exactly one of
	// api|jsonl|mcp per adaptor in v1.
	Transport Transport `json:"transport"`

	// StateMap translates the adaptor's native statuses to the five core
	// StateCategory buckets. Required; an adaptor with no states is not a
	// valid WorkPlane.
	StateMap StateMap `json:"state_map"`

	// EdgeExtensions declares non-core relationship kinds (anything
	// beyond blocks / parent_child / relates_to).
	EdgeExtensions []EdgeExtension `json:"edge_extensions,omitempty"`

	// FieldExtensions declares non-core fields the adaptor emits on
	// WorkItem.Custom.
	FieldExtensions []FieldExtension `json:"field_extensions,omitempty"`

	// RelationshipExtensions declares per-edge metadata the adaptor
	// attaches to every Relationship record.
	RelationshipExtensions []RelationshipExtension `json:"relationship_extensions,omitempty"`

	// SprintNative — adaptor emits first-class Sprint records. When
	// false, core treats ListSprints as "may return empty" and hides the
	// sprint lane chrome in the UI.
	SprintNative bool `json:"sprint_native"`

	// TokenBudgetEnforced — adaptor carries a real TokenBudget with
	// three-tier enforcement (gm-root DD-14). When false, the UI may
	// still render a budget if one is configured, but the "stop" tier
	// has no runtime effect.
	TokenBudgetEnforced bool `json:"token_budget_enforced"`

	// EvidenceSynthesisRequired — adaptor expects the core to synthesize
	// Evidence records from transport-level artifacts (commits, test
	// runs) rather than receive them pre-built (gm-root DD-13). When
	// false, the adaptor provides its own Evidence in GetWorkItem.
	EvidenceSynthesisRequired bool `json:"evidence_synthesis_required"`

	// ReadOnly — adaptor cannot service mutations. CreateWorkItem,
	// UpdateWorkItem, and AttachEvidence all fail with KindReadOnly;
	// the UI MUST hide write controls rather than disable them. Used
	// by the --dolt-url direct SQL connector (gm-0fd); the bd CLI
	// adapter leaves this false because it mediates mutations through
	// bd's public API.
	ReadOnly bool `json:"read_only"`

	// DescriptionFormat declares the content type of WorkItem.Description
	// so the SPA can pick the correct renderer ("plain" → preformatted
	// text, "markdown" → markdown with GFM extensions). Adaptors that
	// don't set it fall through to "plain" on the SPA side; beads-backed
	// adaptors (bd CLI, dolt SQL) default to "markdown" since that's
	// what `bd` edits. Unknown values MUST be treated as "plain" by the
	// UI so a future format can ship without breaking older clients.
	DescriptionFormat string `json:"description_format,omitempty"`

	// --- Agentic-data-plane R1–R8 fields (gm-ekr / gmp-phc) ------------
	//
	// These nine fields let core decide whether an adaptor clears the
	// "agentic data plane" minimum bar (MinimumBar), and which optional
	// orchestrator capabilities the UI and Gas Town / Metaswarm can
	// assume are available. Below-bar adaptors still register — the
	// Host flips a flag they can inspect — but capability-sensitive
	// orchestrators refuse to bind them for high-autonomy work.
	//
	// The mapping to R1–R8 is in docs/design/ (domain.md §1.0 /
	// dataplane-requirements.md when it lands in the rig).

	// SchemaEnforcement declares whether the adaptor's store enforces
	// the core WorkItem schema natively (Dolt SQL, Postgres, typed API)
	// or the adaptor synthesizes it on top of an unstructured substrate
	// (flat Markdown, YAML frontmatter, free-form JSON). R1.
	SchemaEnforcement SchemaEnforcement `json:"schema_enforcement,omitempty"`

	// QueryLanguages enumerates the query surfaces the adaptor exposes
	// beyond the baseline ListWorkItems filter. Callers use this to
	// decide whether to push predicates down (e.g. sql-subset, jsonpath)
	// vs. post-filter in-process. R2.
	QueryLanguages []QueryLanguage `json:"query_languages,omitempty"`

	// DependencyGraphNative — the adaptor's store models the work-item
	// dependency graph as first-class edges (rather than free-form
	// labels or comment conventions). True for bd, Jira, GitHub
	// dependency graph. R3 edges.
	DependencyGraphNative bool `json:"dependency_graph_native,omitempty"`

	// ReadySetQuery — the adaptor exposes a native "ready set" query
	// (e.g. `bd ready`, a materialised view) so an orchestrator can ask
	// "what's unblocked right now?" without walking the graph
	// client-side. R3 native ready-set.
	ReadySetQuery bool `json:"ready_set_query,omitempty"`

	// VersioningTransport lists the versioned transport surfaces the
	// adaptor supports — for importing / exporting / diffing state
	// against another instance. Empty (or ["none"]) means non-versioned.
	// R4.
	VersioningTransport []VersioningTransport `json:"versioning_transport,omitempty"`

	// ConcurrencyModel describes how the adaptor resolves simultaneous
	// writes from N agents. "dolt-merge" and "git-merge" are
	// content-aware, three-way merges. "mvcc" is row-level MVCC
	// (Postgres style). "optimistic" is compare-and-swap without a
	// merge (last-writer-wins on conflict). R5.
	ConcurrencyModel ConcurrencyModel `json:"concurrency_model,omitempty"`

	// AgentSessionDecoupling — work-item state survives an agent
	// session's death. If an agent crashes mid-task, a second agent
	// can pick up exactly where the first left off. **MUST be true for
	// a conforming agentic-data-plane adaptor.** R6.
	AgentSessionDecoupling bool `json:"agent_session_decoupling,omitempty"`

	// AgentNativeAPI names the authoritative programmatic surface
	// agents speak to. "cli" is shell-friendly; "json-api" is the
	// adaptor's own HTTP surface; "mcp" is a first-class MCP server;
	// "rest-only" is below the bar — the adaptor has no agent-native
	// entry point and only a human web UI. R7.
	AgentNativeAPI AgentNativeAPI `json:"agent_native_api,omitempty"`

	// OrchestratorHooks lists the subscription / coordination hooks
	// an orchestrator can rely on. Each value is a guarantee — absent
	// means "no, the orchestrator has to simulate this client-side".
	// R8.
	OrchestratorHooks []OrchestratorHook `json:"orchestrator_hooks,omitempty"`
}

// Known DescriptionFormat values. Keep in lockstep with the SPA's
// renderer registry (web/src/components/board/descriptionRenderers.tsx).
const (
	DescriptionFormatPlain    = "plain"
	DescriptionFormatMarkdown = "markdown"
)

// SchemaEnforcement classifies how the adaptor's store enforces
// the core WorkItem schema. R1.
type SchemaEnforcement string

const (
	// SchemaNative — the substrate enforces the schema (SQL columns,
	// typed API, required fields). A malformed write fails at write
	// time, not on read.
	SchemaNative SchemaEnforcement = "native"
	// SchemaSynthesized — the substrate is unstructured (flat file,
	// free-form JSON) and the adaptor reconstructs the schema on read
	// with best-effort projection.
	SchemaSynthesized SchemaEnforcement = "synthesized"
)

// SchemaEnforcements is the authoritative set — keep in lockstep with
// any SPA enum that gates on these tokens.
var SchemaEnforcements = []SchemaEnforcement{SchemaNative, SchemaSynthesized}

// QueryLanguage names an optional query surface beyond the baseline
// WorkItemFilter. R2.
type QueryLanguage string

const (
	// QueryFilterOnly — only the structured WorkItemFilter surface.
	// Every conforming adaptor supports this implicitly; declaring it
	// means "no other query language is exposed".
	QueryFilterOnly QueryLanguage = "filter-only"
	// QueryJSONPath — JSONPath expressions against WorkItem shape.
	QueryJSONPath QueryLanguage = "jsonpath"
	// QuerySQLSubset — the adaptor's native SQL surface (e.g. Dolt,
	// Postgres). Typically read-only from the agent side.
	QuerySQLSubset QueryLanguage = "sql-subset"
	// QueryGraphQL — a GraphQL surface over the work items.
	QueryGraphQL QueryLanguage = "graphql"
)

// QueryLanguages is the authoritative set.
var QueryLanguages = []QueryLanguage{
	QueryFilterOnly, QueryJSONPath, QuerySQLSubset, QueryGraphQL,
}

// VersioningTransport names a versioned-import/export surface. R4.
type VersioningTransport string

const (
	// VersioningNone — the adaptor has no cross-instance versioning
	// transport. Writes are authoritative; history is whatever the
	// substrate retains.
	VersioningNone VersioningTransport = "none"
	// VersioningGit — the store is (or is backed by) a git repo.
	VersioningGit VersioningTransport = "git"
	// VersioningDolt — Dolt SQL with branches / merges.
	VersioningDolt VersioningTransport = "dolt"
	// VersioningJSONL — newline-delimited JSON export/import, the
	// common lowest-form-factor versioning transport.
	VersioningJSONL VersioningTransport = "jsonl"
	// VersioningNativeSQLiteExport — a native SQLite file the
	// adaptor produces for transport / backup.
	VersioningNativeSQLiteExport VersioningTransport = "native-sqlite-export"
)

// VersioningTransports is the authoritative set.
var VersioningTransports = []VersioningTransport{
	VersioningNone, VersioningGit, VersioningDolt, VersioningJSONL,
	VersioningNativeSQLiteExport,
}

// ConcurrencyModel names how the adaptor resolves simultaneous
// writes from N agents. R5.
type ConcurrencyModel string

const (
	// ConcurrencyOptimistic — compare-and-swap; conflict → fail.
	ConcurrencyOptimistic ConcurrencyModel = "optimistic"
	// ConcurrencyMVCC — row-level multi-version concurrency (Postgres
	// style).
	ConcurrencyMVCC ConcurrencyModel = "mvcc"
	// ConcurrencyGitMerge — three-way merge resolution against a git
	// working tree.
	ConcurrencyGitMerge ConcurrencyModel = "git-merge"
	// ConcurrencyDoltMerge — three-way merge resolution against Dolt.
	// Content-aware: row-level merges survive across simultaneous
	// table writes.
	ConcurrencyDoltMerge ConcurrencyModel = "dolt-merge"
)

// ConcurrencyModels is the authoritative set.
var ConcurrencyModels = []ConcurrencyModel{
	ConcurrencyOptimistic, ConcurrencyMVCC, ConcurrencyGitMerge, ConcurrencyDoltMerge,
}

// AgentNativeAPI names the authoritative programmatic surface agents
// speak to. R7.
type AgentNativeAPI string

const (
	// AgentAPICLI — a CLI binary that agents shell out to.
	AgentAPICLI AgentNativeAPI = "cli"
	// AgentAPIJSONAPI — the adaptor's own HTTP/JSON surface.
	AgentAPIJSONAPI AgentNativeAPI = "json-api"
	// AgentAPIMCP — a first-class MCP server agents connect to.
	AgentAPIMCP AgentNativeAPI = "mcp"
	// AgentAPIRESTOnly — only a REST surface intended for the
	// adaptor's human web UI. No agent-native affordances; agents
	// must scrape or script around it. **Below the minimum bar.**
	AgentAPIRESTOnly AgentNativeAPI = "rest-only"
)

// AgentNativeAPIs is the authoritative set.
var AgentNativeAPIs = []AgentNativeAPI{
	AgentAPICLI, AgentAPIJSONAPI, AgentAPIMCP, AgentAPIRESTOnly,
}

// OrchestratorHook is one coordination guarantee an adaptor declares.
// Each value is a promise — absent means "orchestrator has to simulate
// this client-side". R8.
type OrchestratorHook string

const (
	// HookReadySetSubscribe — the adaptor streams ready-set deltas
	// (new items becoming ready, items leaving the ready set) as
	// they happen, without polling.
	HookReadySetSubscribe OrchestratorHook = "ready-set-subscribe"
	// HookClaimAtomic — claim operations are atomic: two agents
	// racing to claim the same item produce exactly one winner.
	HookClaimAtomic OrchestratorHook = "claim-atomic"
	// HookEscalationIngest — the adaptor accepts structured
	// escalations as first-class records (not free-form comments).
	HookEscalationIngest OrchestratorHook = "escalation-ingest"
	// HookWorkCompleteAck — a work-complete write gets a
	// round-tripped ack so the orchestrator can distinguish
	// "write accepted" from "write in flight".
	HookWorkCompleteAck OrchestratorHook = "work-complete-ack"
	// HookPoolBulkDispatch — the adaptor accepts bulk dispatch of
	// N work items to a pool of agents in one round-trip (vs.
	// per-item RPCs).
	HookPoolBulkDispatch OrchestratorHook = "pool-bulk-dispatch"
)

// OrchestratorHooks is the authoritative set.
var OrchestratorHooks = []OrchestratorHook{
	HookReadySetSubscribe, HookClaimAtomic, HookEscalationIngest,
	HookWorkCompleteAck, HookPoolBulkDispatch,
}

// Validate applies structural checks that every manifest must satisfy.
// Adaptors should call this in the startup path so doctor reports a
// clean failure before the transport is bound.
func (m CapabilityManifest) Validate() error {
	if m.AdaptorName == "" {
		return fmt.Errorf("core: CapabilityManifest.AdaptorName is required")
	}
	if m.ProtocolVersion == "" {
		return fmt.Errorf("core: CapabilityManifest.ProtocolVersion is required")
	}
	if !m.Transport.Valid() {
		return fmt.Errorf("core: CapabilityManifest.Transport %q is not valid", m.Transport)
	}
	if len(m.StateMap) == 0 {
		return fmt.Errorf("core: CapabilityManifest.StateMap must not be empty")
	}
	if err := m.StateMap.Validate(); err != nil {
		return err
	}
	// Enum fields (gm-ekr). Empty is permitted — older adaptors that
	// haven't declared R1–R8 yet register in reduced-capability mode
	// (see MinimumBar). When present, the value must be one of the
	// known tokens so a downstream consumer doesn't silently mishandle
	// an unknown.
	if m.SchemaEnforcement != "" && !isKnownSchemaEnforcement(m.SchemaEnforcement) {
		return fmt.Errorf("core: CapabilityManifest.SchemaEnforcement %q not recognised", m.SchemaEnforcement)
	}
	for _, ql := range m.QueryLanguages {
		if !isKnownQueryLanguage(ql) {
			return fmt.Errorf("core: CapabilityManifest.QueryLanguages contains unknown %q", ql)
		}
	}
	for _, vt := range m.VersioningTransport {
		if !isKnownVersioningTransport(vt) {
			return fmt.Errorf("core: CapabilityManifest.VersioningTransport contains unknown %q", vt)
		}
	}
	if m.ConcurrencyModel != "" && !isKnownConcurrencyModel(m.ConcurrencyModel) {
		return fmt.Errorf("core: CapabilityManifest.ConcurrencyModel %q not recognised", m.ConcurrencyModel)
	}
	if m.AgentNativeAPI != "" && !isKnownAgentNativeAPI(m.AgentNativeAPI) {
		return fmt.Errorf("core: CapabilityManifest.AgentNativeAPI %q not recognised", m.AgentNativeAPI)
	}
	for _, h := range m.OrchestratorHooks {
		if !isKnownOrchestratorHook(h) {
			return fmt.Errorf("core: CapabilityManifest.OrchestratorHooks contains unknown %q", h)
		}
	}
	return nil
}

// MinimumBar reports whether the manifest clears the agentic
// data-plane minimum bar (gm-ekr, domain.md §1.0). Returns false plus
// a list of human-readable reasons when any required criterion is
// missing; callers (registration, orchestrator bind) use the bool to
// decide between full-capability and reduced-capability mode.
//
// The current bar (kept tight on purpose — below-bar adaptors still
// register, so this is a classification, not an admission gate):
//
//   - agent_session_decoupling MUST be true. A store that can't
//     survive an agent session's death isn't an agentic data plane;
//     it's a task list with a task runner glued on.
//   - agent_native_api MUST NOT be "rest-only". An adaptor with only
//     a human web UI can't be safely driven by a fleet of agents.
//
// Other R-fields are advisory: they narrow what capabilities the
// orchestrator can rely on, but they don't disqualify the adaptor.
func (m CapabilityManifest) MinimumBar() (ok bool, reasons []string) {
	if !m.AgentSessionDecoupling {
		reasons = append(reasons, "agent_session_decoupling must be true (R6)")
	}
	if m.AgentNativeAPI == AgentAPIRESTOnly {
		reasons = append(reasons, "agent_native_api \"rest-only\" is below bar (R7)")
	}
	return len(reasons) == 0, reasons
}

// --- enum membership checks -----------------------------------------

func isKnownSchemaEnforcement(v SchemaEnforcement) bool {
	for _, k := range SchemaEnforcements {
		if k == v {
			return true
		}
	}
	return false
}
func isKnownQueryLanguage(v QueryLanguage) bool {
	for _, k := range QueryLanguages {
		if k == v {
			return true
		}
	}
	return false
}
func isKnownVersioningTransport(v VersioningTransport) bool {
	for _, k := range VersioningTransports {
		if k == v {
			return true
		}
	}
	return false
}
func isKnownConcurrencyModel(v ConcurrencyModel) bool {
	for _, k := range ConcurrencyModels {
		if k == v {
			return true
		}
	}
	return false
}
func isKnownAgentNativeAPI(v AgentNativeAPI) bool {
	for _, k := range AgentNativeAPIs {
		if k == v {
			return true
		}
	}
	return false
}
func isKnownOrchestratorHook(v OrchestratorHook) bool {
	for _, k := range OrchestratorHooks {
		if k == v {
			return true
		}
	}
	return false
}

// WorkItemFilter narrows a ListWorkItems query. Zero values mean "no
// filter on that field"; the intersection of non-zero fields applies.
// Adaptors are expected to push as many filters down to their native
// store as possible and only fall back to in-process filtering for
// predicates their backend can't express.
type WorkItemFilter struct {
	IDs           []WorkItemID    `json:"ids,omitempty"`
	Kinds         []string        `json:"kinds,omitempty"`
	Statuses      []string        `json:"statuses,omitempty"`
	StateCategory []StateCategory `json:"state_category,omitempty"`
	AssigneeID    *AgentID        `json:"assignee_id,omitempty"`
	SprintID      *string         `json:"sprint_id,omitempty"`
	Labels        []string        `json:"labels,omitempty"`
	UpdatedSince  *time.Time      `json:"updated_since,omitempty"`
	// Limit caps the returned set. 0 means "adaptor default".
	Limit int `json:"limit,omitempty"`
}

// WorkItemPatch carries an update intended for an existing WorkItem.
// Every field is optional; nil pointers and empty slices mean "do not
// touch". Adaptors translate the patch to their backend's public API
// (gm-root DD-9) — core never writes private storage.
//
// Status and StateCategory move together at the adaptor boundary: the
// adaptor may accept either (its own native token or the normalized
// bucket) and must reject the case where both are set and inconsistent
// with its StateMap.
type WorkItemPatch struct {
	Title         *string           `json:"title,omitempty"`
	Description   *string           `json:"description,omitempty"`
	Status        *string           `json:"status,omitempty"`
	StateCategory *StateCategory    `json:"state_category,omitempty"`
	Priority      *int              `json:"priority,omitempty"`
	Owner         *AgentRef         `json:"owner,omitempty"`
	Assignee      *AgentRef         `json:"assignee,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	DoD           *DefinitionOfDone `json:"dod,omitempty"`
	SprintID      *string           `json:"sprint_id,omitempty"`
	Custom        map[string]any    `json:"custom,omitempty"`
}

// BudgetRollup is the aggregated token consumption for a sprint,
// suitable for the UI's budget dashboard (gm-e12.3 and friends).
//
// Used/Limit/Tier are convenience projections of the underlying
// TokenBudget at read time; ByWorkItem lets the UI break the total
// down without a second query.
type BudgetRollup struct {
	SprintID   string               `json:"sprint_id"`
	Budget     TokenBudget          `json:"budget"`
	Tier       BudgetTier           `json:"tier"`
	ByWorkItem map[WorkItemID]int64 `json:"by_work_item,omitempty"`
	CapturedAt time.Time            `json:"captured_at"`
}

// WorkPlane is the adaptor-agnostic surface every work-tracker
// implementation must satisfy. One WorkPlane is bound per gemba process
// (gm-root DD-1) and paired with one OrchestrationPlane (see
// orchestration.go, gm-e3.3).
//
// Methods are divided into three groups:
//
//  1. Describe — declarative capability advertisement. Called at
//     startup by doctor and on every reconnect by the UI so it always
//     renders against a fresh manifest.
//  2. Work-item queries and mutations — the main CRUD surface.
//     Mutations MUST be implemented by calling the backend's public
//     CLI or API; direct writes to private storage are forbidden (gm-
//     root DD-9).
//  3. Sprint + budget — optional feature group. Adaptors that set
//     SprintNative=false may return empty slices and zero rollups; the
//     UI hides sprint chrome when the manifest says so.
//
// Contexts carry the request deadline and tracing span (gm-e3.6);
// implementations should propagate them verbatim to their transport
// and return a wrapped error on cancellation.
type WorkPlane interface {
	// Describe returns the adaptor's declared capabilities. MUST be
	// idempotent and side-effect-free; called repeatedly by the UI and
	// the doctor command.
	Describe(ctx context.Context) (CapabilityManifest, error)

	// ListWorkItems returns the work items matching filter, sorted by
	// the adaptor's natural order (typically UpdatedAt desc). Adaptors
	// should respect filter.Limit; 0 means "adaptor default".
	ListWorkItems(ctx context.Context, filter WorkItemFilter) ([]WorkItem, error)

	// GetWorkItem returns a single work item by ID, with relationships
	// and evidence populated when the adaptor supports them.
	// Returns ErrNotFound (or a wrapper thereof) when id is unknown.
	GetWorkItem(ctx context.Context, id WorkItemID) (WorkItem, error)

	// CreateWorkItem creates a new work item through the backend's
	// public API and returns the materialized record (including the
	// backend-assigned id and timestamps). Implementations MUST NOT
	// write private storage directly (gm-root DD-9).
	CreateWorkItem(ctx context.Context, wi WorkItem) (WorkItem, error)

	// UpdateWorkItem applies patch to the work item with the given id
	// and returns the resulting record. Adaptors reject patches that
	// violate their own invariants (e.g. illegal status transitions)
	// with a structured error the UI can surface verbatim.
	UpdateWorkItem(ctx context.Context, id WorkItemID, patch WorkItemPatch) (WorkItem, error)

	// AttachEvidence appends an evidence record to the work item. When
	// the manifest sets EvidenceSynthesisRequired=true the core may
	// call this from its own synthesis pipeline; otherwise the adaptor
	// is expected to manage its own evidence and this method may
	// return an ErrUnsupported.
	AttachEvidence(ctx context.Context, id WorkItemID, ev Evidence) error

	// ListSprints returns the sprints currently declared by the
	// backend. Adaptors with SprintNative=false MAY return an empty
	// slice without error.
	ListSprints(ctx context.Context) ([]Sprint, error)

	// ReadBudgetRollup returns the token consumption aggregated against
	// the named sprint. Adaptors with SprintNative=false or
	// TokenBudgetEnforced=false SHOULD return ErrUnsupported so the UI
	// can hide the widget.
	ReadBudgetRollup(ctx context.Context, sprintID string) (BudgetRollup, error)

	// Subscribe streams WorkPlaneEvents matching f. The adaptor closes
	// the returned channel when ctx is cancelled or the underlying
	// transport disconnects (gm-e4.3.1).
	//
	// Adaptors that cannot emit events (noop, read-only dolt-direct,
	// archival formats) return nil + ErrUnsupported. The server-side
	// pump treats ErrUnsupported as "no events from this plane" and
	// drops silently — callers MUST NOT treat it as a hard failure.
	//
	// Multiple concurrent subscribers are supported; the adaptor fans
	// out each emitted event to all active subscribers. Slow
	// subscribers drop events rather than block the fan-out (the hub
	// handles SSE-client-level backpressure upstream).
	Subscribe(ctx context.Context, f WorkPlaneSubscribeFilter) (<-chan WorkPlaneEvent, error)
}

// WorkPlaneSubscribeFilter narrows a Subscribe stream. Zero values
// mean "no filter on that field"; multiple non-zero fields combine
// with AND. gm-e4.3.1.
type WorkPlaneSubscribeFilter struct {
	// Kinds filters to a subset of WorkPlaneEvent kinds
	// (workitem_created / workitem_updated / workitem_closed /
	// workitem_evidence_attached). Empty means all kinds.
	Kinds []string
	// WorkItemID narrows to events about a single work item. Empty
	// means all items.
	WorkItemID WorkItemID
	// Since filters out events emitted before this timestamp — useful
	// when a client reconnects and wants to catch up from its last
	// known event time.
	Since *time.Time
}

// WorkPlaneEvent is the streamed envelope every adaptor mutation
// surfaces through. Mirrors OrchestrationEvent's shape so the event
// hub's canonicalisation layer (internal/events/translate.go) treats
// both planes identically. gm-e4.3.1.
//
// Canonical Kind values:
//
//	"workitem_created"           — CreateWorkItem completed
//	"workitem_updated"           — UpdateWorkItem completed
//	"workitem_closed"            — state_category transitioned to
//	                               completed or canceled
//	"workitem_evidence_attached" — AttachEvidence completed
//
// Adaptors MAY emit additional kinds; the canonicaliser drops unknown
// kinds under the "workplane." namespace so the SPA's kind-handler
// registry can ignore them without error.
type WorkPlaneEvent struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	At         time.Time      `json:"at"`
	WorkItemID WorkItemID     `json:"work_item_id,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

// Canonical WorkPlaneEvent.Kind values. Adaptors SHOULD use these
// tokens; any other value is passed through verbatim by the events
// canonicaliser.
const (
	WorkItemEventCreated          = "workitem_created"
	WorkItemEventUpdated          = "workitem_updated"
	WorkItemEventClosed           = "workitem_closed"
	WorkItemEventEvidenceAttached = "workitem_evidence_attached"
)

// WorkItemNotifier is the optional interface a WorkPlane adaptor
// implements when it can publish a [WorkPlaneEvent] for a mutation
// that landed via an out-of-process writer (gm-e4.3.2). The HTTP
// handler at POST /api/workitems/notify type-asserts the bound
// adaptor to this interface; adaptors that don't implement it (the
// dolt read-only adaptor, the noop adaptor) cause the endpoint to
// return 409 capability_denied.
//
// Implementations re-read the WorkItem through the same path the
// in-process UpdateWorkItem uses (no trust of caller-supplied
// state), derive the kind from the persisted state, and Publish
// through the same emitter so /events SSE subscribers receive an
// event indistinguishable from in-process mutations.
type WorkItemNotifier interface {
	// NotifyExternal re-reads the WorkItem identified by id and
	// publishes a workitem.* event. Returns the re-read WorkItem
	// and the emitted kind ([WorkItemEventUpdated] or
	// [WorkItemEventClosed]). source is an optional hint
	// ("bd-git-hook", "ops-runbook") echoed onto the event payload.
	NotifyExternal(ctx context.Context, id WorkItemID, source string) (WorkItem, string, error)
}
