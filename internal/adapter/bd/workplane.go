package bd

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// Runner is the shape of the `bd` shell the adaptor uses. Tests
// substitute an in-memory stub to exercise CRUD round-trips without
// spawning processes; production wiring shells to the installed `bd`
// binary. Args mirror what the caller would type after `bd ` — flags
// and positional args in CLI order.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// Config configures a production-mode WorkPlane. Tests use
// NewWorkPlaneWithRunner instead and bypass this struct.
type Config struct {
	// BeadsDir is the working directory the bd subprocess inherits. When
	// empty, bd runs in the gemba server's cwd. Mirrors the
	// `--beads-dir` flag on `gemba serve`.
	BeadsDir string

	// DescriptionFormat overrides the CapabilityManifest's declared
	// description content type. Empty string → defaults to
	// core.DescriptionFormatMarkdown, which is what `bd` edits day-to-day.
	// Operators override this only if their beads rig stores a different
	// format in Description (rare).
	DescriptionFormat string

	// Repositories, when non-nil, enables prefix-based repository
	// routing (gm-d2ts). On every WorkItem read, if a bead carries no
	// explicit `repo:*` label, the adaptor splits its bd id at the
	// first hyphen and looks up the prefix in this registry. A match
	// populates PrimaryRepositoryID + RepositoryIDs from the
	// resolved [core.Repository.ID]. Nil keeps legacy behavior
	// (operators get repo association only via explicit labels).
	Repositories *core.RepositoryRegistry
}

// WorkPlane is the Beads-backed core.WorkPlane implementation
// (gm-e6.1). It wraps `bd list / show / create / update` behind the
// adaptor-agnostic interface by shelling to the CLI with --json and
// mapping the returned Bead records onto core.WorkItem.
//
// The prefix field is the "<workspace>/<repo>" chunk prepended to
// every bd id when emitting a WorkItemID (DD-6). Default is
// "gemba/gemba" so the adaptor composes with the local gemba rig out
// of the box; callers running multiple Beads workspaces on one gemba
// instance override it at construction.
type WorkPlane struct {
	run               Runner
	prefix            string
	descriptionFormat string
	emitter           *core.WorkPlaneEmitter
	repos             *core.RepositoryRegistry // gm-d2ts; nil = no prefix routing
	// beadsDir is the workspace root the bd subprocess runs in. Empty
	// for tests built via NewWorkPlaneWithRunner. The fsnotify watcher
	// (gm-e6.6) attaches to <beadsDir>/.beads when this is set.
	beadsDir    string
	watcherOnce sync.Once
	watcherStop func()
}

// NewWorkPlane returns a WorkPlane shelling to the `bd` binary on PATH.
// Returns a tagged AdaptorDegraded error if `bd` is not installed so
// callers can branch on the typed envelope without probing PATH
// themselves. cfg.BeadsDir, when non-empty, becomes the working
// directory of every spawned bd process so a single gemba server can
// point at any beads workspace on the host.
func NewWorkPlane(cfg Config) (*WorkPlane, error) {
	path, err := exec.LookPath("bd")
	if err != nil {
		return nil, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"beads: bd CLI not on PATH")
	}
	beadsDir := cfg.BeadsDir
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		if beadsDir != "" {
			cmd.Dir = beadsDir
		}
		return cmd.Output()
	}
	format := cfg.DescriptionFormat
	if format == "" {
		format = core.DescriptionFormatMarkdown
	}
	return &WorkPlane{
		run:               runner,
		prefix:            defaultPrefix,
		descriptionFormat: format,
		emitter:           core.NewWorkPlaneEmitter(),
		repos:             cfg.Repositories,
		beadsDir:          beadsDir,
	}, nil
}

// NewWorkPlaneWithRunner is the constructor tests use to inject a stub
// Runner. Production callers use NewWorkPlane. A zero prefix is
// replaced with the default so tests don't need to re-specify it.
func NewWorkPlaneWithRunner(run Runner, prefix string) *WorkPlane {
	if prefix == "" {
		prefix = defaultPrefix
	}
	return &WorkPlane{
		run:               run,
		prefix:            prefix,
		descriptionFormat: core.DescriptionFormatMarkdown,
		emitter:           core.NewWorkPlaneEmitter(),
	}
}

const (
	// defaultPrefix is the <workspace>/<repo> fragment the adaptor
	// prepends to bare bd ids. Matches the id format the conformance
	// harness uses ("gemba/gemba/gm-..."), so the default composes with
	// the in-repo gemba rig without extra configuration.
	defaultPrefix = "gemba/gemba"

	adaptorName    = "beads"
	adaptorVersion = "0.1.0"
)

var _ core.WorkPlane = (*WorkPlane)(nil)

// Describe returns the Beads capability manifest. SprintNative,
// TokenBudgetEnforced, and EvidenceSynthesisRequired are all false for
// now: sprint + budget land as a separate epic, and Evidence synthesis
// is deferred to gm-e6.5. Group E's conformance probe asserts the
// gated methods (AttachEvidence, ListSprints, ReadBudgetRollup) fail
// fast with capability_denied when the manifest opts out.
func (w *WorkPlane) Describe(context.Context) (core.CapabilityManifest, error) {
	m := beadsManifest
	m.DescriptionFormat = w.descriptionFormat
	return m, nil
}

var beadsManifest = core.CapabilityManifest{
	AdaptorName:     adaptorName,
	AdaptorVersion:  adaptorVersion,
	ProtocolVersion: core.ProtocolVersion,
	// The bd adaptor is in-process Go reachable through gemba's HTTP+JSON
	// surface, so the api transport host is its registration target
	// (DD-12).
	Transport: core.TransportAPI,
	StateMap:  beadsStateMap,
	// Beads carries an issue_type field (task|feature|bug|decision|
	// epic|chore|molecule|…) that has no core counterpart — expose it
	// as a Custom field so the SPA's beads extension can render the
	// chip without needing a dedicated core field. gm-e6.2/6.3/6.4
	// add more as the edge/evidence/DoD mappings land.
	FieldExtensions: []core.FieldExtension{
		{Name: "beads:issue_type", Type: "string",
			Description: "Beads issue_type (task|feature|bug|decision|epic|chore|molecule|event)"},
		{Name: "beads:notes", Type: "markdown",
			Description: "Free-text notes field bd writes independently of description"},
		{Name: "beads:parent", Type: "string",
			Description: "Parent bead id — populated for hierarchical children"},
	},
	// EdgeExtensions declares the four Beads-native edge types that don't
	// map onto core's three kinds (blocks / parent_child / relates_to).
	// The SPA renders these only when the beads/ extension bundle is
	// loaded; other adaptors see an advisory "relates_to" fallback. The
	// canonical table lives in edges.go so the mapping code and the
	// manifest declaration can't drift (gm-e6.2 / DD-9).
	EdgeExtensions:            beadsEdgeExtensions,
	SprintNative:              false,
	TokenBudgetEnforced:       false,
	EvidenceSynthesisRequired: false,

	// --- R1–R8 projection (gm-zdx) ------------------------------------
	//
	// bd is backed by a Dolt SQL store with first-class edges, a native
	// ready-set query (`bd ready`), and an in-process CLI that agents
	// drive directly. This mapping is audited in the conformance suite
	// and any drift surfaces as a failing Group A probe.

	// R1 — Dolt enforces columns + types at write time.
	SchemaEnforcement: core.SchemaNative,
	// R2 — agents use the WorkItemFilter surface; operators can also
	// drop into Dolt SQL for ad-hoc analytics.
	QueryLanguages: []core.QueryLanguage{core.QueryFilterOnly, core.QuerySQLSubset},
	// R3 — dep graph is native (bd's edge table), ready-set is a
	// first-class query.
	DependencyGraphNative: true,
	ReadySetQuery:         true,
	// R4 — bd data is a git repo of Dolt rows; jsonl export is the
	// interchange form every other adaptor reads.
	VersioningTransport: []core.VersioningTransport{
		core.VersioningGit, core.VersioningDolt, core.VersioningJSONL,
	},
	// R5 — Dolt three-way merges resolve concurrent writers
	// row-by-row. Above optimistic and above plain git-merge.
	ConcurrencyModel: core.ConcurrencyDoltMerge,
	// R6 — bd's state lives in Dolt, not a per-agent session. Kill the
	// agent, the next one picks up from the same rows.
	AgentSessionDecoupling: true,
	// R7 — the bd CLI is the agent-native entry point. Gemba's
	// json-api layer projects it over HTTP for SPA consumers.
	AgentNativeAPI: core.AgentAPICLI,
	// R8 — bd supplies ready-set subscribe, atomic claim, escalation
	// ingest, and work-complete ack. pool-bulk-dispatch isn't wired
	// yet — file as a gap under gm-e6 if the audit flags it.
	OrchestratorHooks: []core.OrchestratorHook{
		core.HookReadySetSubscribe,
		core.HookClaimAtomic,
		core.HookEscalationIngest,
		core.HookWorkCompleteAck,
	},
}

// ListWorkItems runs `bd list --json` with filter-derived flags and
// maps each row to a WorkItem. The bd CLI does NOT expose every
// core.WorkItemFilter field as a flag yet; what can't be pushed down
// (UpdatedSince, multi-value status intersections) is filtered in
// process after the CLI call.
func (w *WorkPlane) ListWorkItems(ctx context.Context, f core.WorkItemFilter) ([]core.WorkItem, error) {
	args := []string{"list", "--json"}
	if f.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(f.Limit))
	}
	// bd list accepts a single --status; when the caller supplied more
	// than one, we omit the flag and filter client-side. Same for
	// labels: --label accepts comma-separated values.
	if len(f.Statuses) == 1 {
		args = append(args, "--status", f.Statuses[0])
	}
	// Gather all bd --label filters: caller-supplied Filter.Labels plus
	// the milestone convention (type:milestone) when Kinds asks only for
	// milestones. Multi-kind requests still post-filter client-side
	// because bd has no native "kind set" flag we can push down without
	// narrowing the result set incorrectly.
	labels := append([]string(nil), f.Labels...)
	if len(f.Kinds) == 1 && f.Kinds[0] == core.KindMilestone {
		labels = append(labels, milestoneLabel)
	}
	if len(labels) > 0 {
		args = append(args, "--label", strings.Join(labels, ","))
	}
	if f.AssigneeID != nil && *f.AssigneeID != "" {
		args = append(args, "--assignee", string(*f.AssigneeID))
	}

	out, err := w.run(ctx, args...)
	if err != nil {
		return nil, wrapBdError(err, "bd list --json")
	}
	var beads []Bead
	if err := json.Unmarshal(out, &beads); err != nil {
		return nil, core.WrapAdaptorError(core.KindProcessFailed, err,
			"beads: parse bd list --json")
	}
	items := make([]core.WorkItem, 0, len(beads))
	for i := range beads {
		wi := beads[i].toWorkItem(w.prefix, w.repos)
		if !matchesFilter(wi, f) {
			continue
		}
		items = append(items, wi)
	}
	return items, nil
}

// matchesFilter applies the filter predicates bd couldn't push down to
// its native query (multi-status intersections, state_category sets,
// sprint_id, ids). Zero-value fields still behave as "no filter".
func matchesFilter(wi core.WorkItem, f core.WorkItemFilter) bool {
	if len(f.IDs) > 0 {
		hit := false
		for _, id := range f.IDs {
			if id == wi.ID {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(f.Kinds) > 0 {
		hit := false
		for _, k := range f.Kinds {
			if k == wi.Kind {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(f.Statuses) > 1 {
		hit := false
		for _, s := range f.Statuses {
			if s == wi.Status {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(f.StateCategory) > 0 {
		hit := false
		for _, c := range f.StateCategory {
			if c == wi.StateCategory {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if f.UpdatedSince != nil && wi.UpdatedAt.Before(*f.UpdatedSince) {
		return false
	}
	return true
}

// GetWorkItem resolves a single bead via `bd show <id> --json`. bd's
// show output is an array (it accepts multi-id batch lookups); we
// expect exactly one row and surface session_not_found otherwise so
// errors.Is(err, core.ErrNotFound) keeps working for callers.
func (w *WorkPlane) GetWorkItem(ctx context.Context, id core.WorkItemID) (core.WorkItem, error) {
	native := nativeID(w.prefix, id)
	out, err := w.run(ctx, "show", native, "--json")
	if err != nil {
		// bd exits non-zero with a "not found" stderr when the id is
		// missing. We surface that as session_not_found so the legacy
		// ErrNotFound sentinel + Group F's tagged-error assertion both
		// match. Other non-zero exits remain adaptor_degraded (the Dolt
		// daemon is the most common failure path here).
		if isNotFoundError(err) {
			return core.WorkItem{}, core.WrapAdaptorError(core.KindSessionNotFound, err,
				"beads: bead %q not found", native)
		}
		return core.WorkItem{}, wrapBdError(err, "bd show "+native+" --json")
	}
	var beads []Bead
	if err := json.Unmarshal(out, &beads); err != nil {
		return core.WorkItem{}, core.WrapAdaptorError(core.KindProcessFailed, err,
			"beads: parse bd show --json")
	}
	if len(beads) == 0 {
		return core.WorkItem{}, core.NewAdaptorError(core.KindSessionNotFound,
			"beads: bead %q not found", native)
	}
	return beads[0].toWorkItem(w.prefix, w.repos), nil
}

// CreateWorkItem creates a new bead via `bd create --json`. Caller-
// supplied ID is ignored — Beads assigns ids from its prefix pool —
// and the materialized record (with backend id + timestamps) is read
// back so the returned WorkItem reflects exactly what bd stored.
//
// Rule: mutations MUST go through the backend's public CLI (DD-9).
// Writing .beads/*.db directly is a conformance failure.
func (w *WorkPlane) CreateWorkItem(ctx context.Context, wi core.WorkItem) (core.WorkItem, error) {
	args := []string{"create", wi.Title, "--json"}
	// gm-root.3.4: KindMilestone is Gemba-native. bd has no milestone
	// issue type, so we encode it as `-t epic -l type:milestone`
	// (see docs/design/milestone-convention.md). Every other kind
	// passes through unchanged as the bd native type.
	kindArg := wi.Kind
	addMilestoneLabel := false
	if kindArg == core.KindMilestone {
		kindArg = "epic"
		addMilestoneLabel = true
	}
	if kindArg != "" {
		args = append(args, "--type", kindArg)
	}
	if wi.Priority != nil {
		args = append(args, "--priority", strconv.Itoa(*wi.Priority))
	}
	if wi.Description != "" {
		args = append(args, "--description", wi.Description)
	}
	// Agent-federation labels (gm-e6.3) ride with --labels so a single
	// create call persists the full AgentRef. Caller-supplied labels win
	// for non-agent keys; agent:role/parent prefixes are rewritten from
	// wi.Assignee so the two views cannot drift at insert time.
	labels := stripAgentLabels(wi.Labels)
	if wi.Assignee != nil {
		labels = append(labels, agentLabels(wi.Assignee)...)
	}
	if addMilestoneLabel && !hasLabel(labels, milestoneLabel) {
		// Append, not prepend — caller-supplied labels keep their order
		// and the milestone marker is stable at a known index.
		labels = append(labels, milestoneLabel)
	}
	if len(labels) > 0 {
		args = append(args, "--labels", strings.Join(labels, ","))
	}
	if wi.Assignee != nil && wi.Assignee.ID != "" {
		args = append(args, "--assignee", string(wi.Assignee.ID))
	}
	// Parent is carried as a Relationship{Kind: parent_child, From: parent, To: ""}.
	// `To` is empty on create because the new bead's id isn't known yet.
	// bd's CLI accepts the raw id (with or without prefix); nativeID
	// strips the rig-prefix so a caller passing "gemba/gm-a3" ends up
	// invoking `--parent gm-a3`, matching how bd names beads internally.
	if parent := parentOnCreate(wi.Relationships); parent != "" {
		args = append(args, "--parent", nativeID(w.prefix, parent))
	}

	out, err := w.run(ctx, args...)
	if err != nil {
		return core.WorkItem{}, wrapBdError(err, "bd create --json")
	}
	bead, err := decodeCreateOutput(out)
	if err != nil {
		return core.WorkItem{}, err
	}
	wiOut := bead.toWorkItem(w.prefix, w.repos)
	w.emit(core.WorkItemEventCreated, wiOut)
	return wiOut, nil
}

// decodeCreateOutput accepts both shapes bd's create command can emit
// with --json: a single-object `{...}` and a one-element array
// `[{...}]`. Tolerating both keeps the adaptor compatible with Beads
// versions that differ on this detail without forcing a minimum
// version.
func decodeCreateOutput(out []byte) (*Bead, error) {
	trimmed := strings.TrimSpace(string(out))
	if strings.HasPrefix(trimmed, "[") {
		var beads []Bead
		if err := json.Unmarshal(out, &beads); err != nil {
			return nil, core.WrapAdaptorError(core.KindProcessFailed, err,
				"beads: parse bd create --json (array)")
		}
		if len(beads) == 0 {
			return nil, core.NewAdaptorError(core.KindProcessFailed,
				"beads: bd create --json returned empty array")
		}
		return &beads[0], nil
	}
	var bead Bead
	if err := json.Unmarshal(out, &bead); err != nil {
		return nil, core.WrapAdaptorError(core.KindProcessFailed, err,
			"beads: parse bd create --json")
	}
	return &bead, nil
}

// UpdateWorkItem applies a WorkItemPatch via `bd update <id> --json`.
// Non-nil patch fields translate to the matching bd flag; nil / empty
// values are ignored so callers can patch a single field without
// touching the rest (DD-9 mutation model).
//
// Labels have specific semantics: a non-nil slice replaces the full
// label set (matches bd's --set-labels), so callers who want additive
// updates must build the new slice themselves. This mirrors the
// core.WorkItemPatch contract ("empty slice means do not touch"; a
// populated slice is the new set).
func (w *WorkPlane) UpdateWorkItem(
	ctx context.Context, id core.WorkItemID, patch core.WorkItemPatch,
) (core.WorkItem, error) {
	native := nativeID(w.prefix, id)
	args := []string{"update", native, "--json"}
	if patch.Title != nil {
		args = append(args, "--title", *patch.Title)
	}
	if patch.Description != nil {
		args = append(args, "--description", *patch.Description)
	}
	if patch.Status != nil {
		args = append(args, "--status", *patch.Status)
	} else if patch.StateCategory != nil {
		// Drag-to-restage and other core-level flows express the target
		// as a StateCategory (the SPA never invents bd-native tokens).
		// Translate via the canonical inverse of beadsStateMap: pick the
		// representative bd status for the target bucket. Categories
		// without a bd-native equivalent are a validation error — the
		// caller must send an explicit --status instead.
		bdStatus, ok := bdStatusForCategory(*patch.StateCategory)
		if !ok {
			return core.WorkItem{}, core.NewAdaptorError(core.KindValidation,
				"beads: state_category %q has no bd-native status; send an explicit status field instead",
				*patch.StateCategory)
		}
		args = append(args, "--status", bdStatus)
	}
	if patch.Priority != nil {
		args = append(args, "--priority", strconv.Itoa(*patch.Priority))
	}
	if patch.Assignee != nil && patch.Assignee.ID != "" {
		args = append(args, "--assignee", string(patch.Assignee.ID))
	}
	// Agent-federation labels on the patched assignee (gm-e6.3). If the
	// caller also supplied Labels we merge into that --set-labels value
	// so a single bd update writes the full federated view atomically —
	// stale agent:* labels in patch.Labels are stripped so the new
	// AgentRef is authoritative. If only Assignee is patched we fall
	// through to --add-label (additive) rather than --set-labels, since
	// we don't want an Assignee-only patch to silently clear other
	// labels the caller didn't intend to touch.
	agentExtra := agentLabels(patch.Assignee)
	switch {
	case len(patch.Labels) > 0:
		merged := stripAgentLabels(patch.Labels)
		merged = append(merged, agentExtra...)
		args = append(args, "--set-labels", strings.Join(merged, ","))
	case len(agentExtra) > 0:
		for _, l := range agentExtra {
			args = append(args, "--add-label", l)
		}
	}

	if _, err := w.run(ctx, args...); err != nil {
		if isNotFoundError(err) {
			return core.WorkItem{}, core.WrapAdaptorError(core.KindSessionNotFound, err,
				"beads: bead %q not found", native)
		}
		return core.WorkItem{}, wrapBdError(err, "bd update "+native+" --json")
	}
	// bd update --json output varies by version (some emit the updated
	// row, some emit only an ack). Re-reading through show is both the
	// simplest path to a populated WorkItem and the safer one — the
	// caller always sees the persisted state, not our optimistic
	// in-memory merge.
	wiOut, err := w.GetWorkItem(ctx, id)
	if err != nil {
		return core.WorkItem{}, err
	}
	kind := core.WorkItemEventUpdated
	// Treat a transition into a terminal state as a dedicated 'closed'
	// signal. Mirrors the gm-e12.2 SPA handler that invalidates
	// ['beads'] on workitem.closed in addition to workitem.updated.
	if patch.StateCategory != nil &&
		(*patch.StateCategory == core.StateCompleted || *patch.StateCategory == core.StateCanceled) {
		kind = core.WorkItemEventClosed
	} else if wiOut.StateCategory == core.StateCompleted || wiOut.StateCategory == core.StateCanceled {
		// StateCategory wasn't explicit in the patch but bd derived a
		// terminal bucket from a native status token (e.g. 'closed'
		// mapping to StateCompleted). The SPA needs the close signal
		// either way.
		kind = core.WorkItemEventClosed
	}
	w.emit(kind, wiOut)
	return wiOut, nil
}

// AttachEvidence is gated off until gm-e6.5 lands the synthesis
// pipeline. The manifest's EvidenceSynthesisRequired=false makes this
// a capability-denied op at the port; the adaptor-side check is
// defense in depth (docs/adaptors/workplane.md §Adaptor-side fail-fast).
func (w *WorkPlane) AttachEvidence(_ context.Context, _ core.WorkItemID, _ core.Evidence) error {
	return core.EnforceCapability(beadsManifest, core.OpAttachEvidence)
}

// ListSprints returns capability_denied: Beads has no native sprint
// concept. The manifest's SprintNative=false hides the UI's sprint
// chrome, and this shim keeps the adaptor-side guarantee (Group E).
func (w *WorkPlane) ListSprints(_ context.Context) ([]core.Sprint, error) {
	return nil, core.EnforceCapability(beadsManifest, core.OpListSprints)
}

// ReadBudgetRollup returns capability_denied: with SprintNative=false
// AND TokenBudgetEnforced=false there is nothing to roll up.
func (w *WorkPlane) ReadBudgetRollup(_ context.Context, _ string) (core.BudgetRollup, error) {
	return core.BudgetRollup{}, core.EnforceCapability(beadsManifest, core.OpReadBudgetRollup)
}

// Subscribe streams WorkPlaneEvents for mutations this adaptor
// instance performs. Mutations made by a separate `bd` process (e.g. a
// human running `bd update` in a terminal) do NOT surface here — the
// DoD for gm-e4.3.1 promises the in-process half of the chain; the
// cross-process half lands in a follow-up (file watcher or bd git
// hook). Returning a real channel rather than ErrUnsupported lets the
// /events SSE hub attach and fan out the subset it does see today.
// gm-e4.3.1.
func (w *WorkPlane) Subscribe(ctx context.Context, f core.WorkPlaneSubscribeFilter) (<-chan core.WorkPlaneEvent, error) {
	// gm-e6.6: lazy-start the fsnotify+poll watcher so out-of-process
	// `bd update` calls still propagate to subscribers. No-op when the
	// adaptor was built without a beadsDir (test runners).
	w.startBeadsWatcher()
	return w.emitter.Subscribe(ctx, f), nil
}

// Close releases the fsnotify watcher and any background polling
// goroutines. Idempotent; safe to call from a test cleanup or from
// the `gemba serve` shutdown path. Subscribers attached via the
// emitter remain — their channels close on their own context cancel
// per the in-emitter contract.
func (w *WorkPlane) Close() error {
	w.stopBeadsWatcher()
	return nil
}

// NotifyExternal publishes a workitem.* event for a mutation that
// landed via an out-of-process bd writer (terminal `bd update`, the
// post-commit git hook from gm-e4.3.3, etc.) rather than through this
// adaptor's own UpdateWorkItem path (gm-e4.3.2).
//
// The endpoint is a dumb trigger — Gemba does NOT trust caller-
// supplied state. We re-read the WorkItem through the same `bd show`
// path UpdateWorkItem uses, derive the kind from the persisted
// StateCategory (mirroring UpdateWorkItem's terminal-state detection),
// and Publish through the same emitter so /events SSE subscribers
// receive an event indistinguishable from the in-process case.
//
// Returns the re-read WorkItem and the emitted kind. The kind is
// useful for the HTTP handler's response and audit-log row.
//
// `source` is an optional hint (e.g. "bd-git-hook") echoed onto the
// emitted event's payload so subscribers can route on origin.
//
// Errors propagate verbatim from GetWorkItem — a missing id surfaces
// as session_not_found, a bd-binary failure as adaptor_degraded.
func (w *WorkPlane) NotifyExternal(
	ctx context.Context, id core.WorkItemID, source string,
) (core.WorkItem, string, error) {
	wi, err := w.GetWorkItem(ctx, id)
	if err != nil {
		return core.WorkItem{}, "", err
	}
	kind := core.WorkItemEventUpdated
	// Same terminal-state rule as UpdateWorkItem. Out-of-process
	// closes (e.g. a polecat ran `bd close`) need to fan out as
	// workitem_closed so the SPA invalidates ['beads'] correctly
	// (gm-e12.2).
	if wi.StateCategory == core.StateCompleted ||
		wi.StateCategory == core.StateCanceled {
		kind = core.WorkItemEventClosed
	}
	w.emitWithSource(kind, wi, source)
	return wi, kind, nil
}

// emit publishes a WorkPlaneEvent after a successful mutation. Called
// from CreateWorkItem / UpdateWorkItem.
func (w *WorkPlane) emit(kind string, wi core.WorkItem) {
	w.emitWithSource(kind, wi, "")
}

// emitWithSource is the underlying publish helper. The source string,
// when non-empty, lands on the event payload as `notify_source` so
// subscribers can distinguish events triggered by NotifyExternal from
// in-process ones.
func (w *WorkPlane) emitWithSource(kind string, wi core.WorkItem, source string) {
	now := time.Now().UTC()
	payload := map[string]any{
		"after": wi,
	}
	if source != "" {
		payload["notify_source"] = source
	}
	w.emitter.Publish(core.WorkPlaneEvent{
		ID:         fmt.Sprintf("bd-%s-%d", wi.ID, now.UnixNano()),
		Kind:       kind,
		At:         now,
		WorkItemID: wi.ID,
		Payload:    payload,
	})
}

// isNotFoundError reports whether err carries bd's "not found" signal.
// bd's vocabulary has drifted across versions — older builds emit
// "issue not found: gm-abc", current builds emit
// `Error fetching <id>: no issue found matching "<id>"`, and a few
// surfaces use "no such issue". We match all three so callers reliably
// get session_not_found rather than the generic adaptor_degraded
// envelope. gm-hif1 — surfaced when /api/workitems/notify on a real
// bd backend was returning 503 instead of 404 for unknown ids.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if exitErr, ok := err.(*exec.ExitError); ok {
		msg = msg + " " + string(exitErr.Stderr)
	}
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "not found") ||
		strings.Contains(lower, "no such issue") ||
		strings.Contains(lower, "no issue found")
}

// wrapBdError normalizes a bd subprocess failure into the tagged
// adaptor_degraded envelope every boundary error MUST wear (gm-faz,
// Group F). Dolt hangs, daemon outages, and broken pipes all flow
// through this path so the SPA banner can surface a consistent
// "backend degraded" signal.
func wrapBdError(cause error, cmdline string) error {
	msg := strings.TrimSpace(cause.Error())
	if exitErr, ok := cause.(*exec.ExitError); ok {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			msg = stderr
		}
	}
	return core.WrapAdaptorError(core.KindAdaptorDegraded, cause,
		"beads: %s: %s", cmdline, firstLine(msg))
}
