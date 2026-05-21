package gt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// Role is the stable role identifier Gemba's UI branches on via
// AgentGroup.Extension["role"]. Keeping these as typed constants is the
// DoD for gm-e7.1 — the pack-agnostic UI MUST render groups without a
// switch statement on display names.
const (
	RoleMayor    = "mayor"
	RoleDeacon   = "deacon"
	RoleWitness  = "witness"
	RoleRefinery = "refinery"
	RolePolecats = "polecats"
	RoleCrew     = "crew"
)

// Group ID prefixes. IDs are stable across restarts so the UI can cache
// group membership and the event stream can reference groups by id.
const (
	groupIDTown       = "gastown/town"
	groupIDRigPrefix  = "gastown/rig/"
	groupIDRolePrefix = "gastown/role/"
)

// agentIDMayor and agentIDDeacon are the town-level singletons.
const (
	agentIDMayor  core.AgentID = "gastown/mayor"
	agentIDDeacon core.AgentID = "gastown/deacon"
)

// gtProbeRunner is the shape of the `gt` shell the adaptor uses. Tests
// override this to stub CLI output without spawning processes.
type gtProbeRunner func(ctx context.Context, args ...string) ([]byte, error)

// OrchestrationPlane is a partial core.OrchestrationPlaneAdaptor for Gas
// Town v1.0. This bead (gm-e7.1) implements ListAgents, ListGroups, and
// ResolveGroupMembers by shelling out to `gt`. The remaining lifecycle
// methods land in sibling beads (gm-e7.2–gm-e7.7); they return
// KindUnsupported here so callers can still branch on a tagged envelope.
//
// caps is populated once at construction by probeCapabilities (gm-e7.12)
// and consulted by feature-gated codepaths (e.g. RecycleSession needs
// `gt handoff` to exist). Tests construct OrchestrationPlane directly
// and may leave caps zero-valued — callers MUST tolerate that.
//
// recycledEvents is the side-channel through which RecycleSession
// (gm-e7.13) emits `session.recycled` events. The Subscribe goroutine
// drains it alongside the poll-based emitters so the
// session_recycles writer (gm-s47n.12) sees gt recycles too. nil
// channel disables emission (used by tests that don't care about
// recycle events).
type OrchestrationPlane struct {
	// UnsupportedSessionIO is the Phase A default-noop mixin for the
	// session-IO trio (SendInput / ResizeSession / StreamSession).
	// Phase B+ will override per gt's runtime model.
	core.UnsupportedSessionIO

	run            gtProbeRunner
	root           string
	caps           gtCapabilities
	recycledEvents chan core.OrchestrationEvent
}

// Config controls how the adaptor reaches Gas Town. Root is the Gas
// City/Town directory used as cwd for every `gt` invocation; empty keeps
// the historical "inherit gemba's cwd" behavior.
type Config struct {
	Root string
}

// NewOrchestrationPlane returns an adaptor that shells to the `gt` binary
// on PATH. Returns an error if `gt` is not installed or if the gt CLI
// itself fails to answer `gt --version` (capability probe — gm-e7.12).
func NewOrchestrationPlane() (*OrchestrationPlane, error) {
	return NewWithConfig(Config{})
}

// NewWithConfig returns a Gas Town adaptor rooted at cfg.Root. This is
// the first-class production path for `gemba serve --orchestration=gastown
// --city/--town ...`: Beads remain on the WorkPlane (often --dolt-url)
// while every orchestration command runs from the selected Gas Town
// workspace.
func NewWithConfig(cfg Config) (*OrchestrationPlane, error) {
	path, err := exec.LookPath("gt")
	if err != nil {
		return nil, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"gastown: gt CLI not on PATH")
	}
	root := strings.TrimSpace(cfg.Root)
	if root != "" {
		info, err := os.Stat(root)
		if err != nil {
			return nil, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
				"gastown: root %q is not reachable", root)
		}
		if !info.IsDir() {
			return nil, core.NewAdaptorError(core.KindAdaptorDegraded,
				"gastown: root %q is not a directory", root)
		}
	}
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		if root != "" {
			cmd.Dir = root
		}
		return cmd.Output()
	}
	o := &OrchestrationPlane{
		run:            runner,
		root:           root,
		recycledEvents: make(chan core.OrchestrationEvent, 16),
	}
	caps, err := probeCapabilities(context.Background(), liveProbeRunner(path, root), nil)
	if err != nil {
		return nil, err
	}
	o.caps = caps
	return o, nil
}

var _ core.OrchestrationPlaneAdaptor = (*OrchestrationPlane)(nil)

// gastownManifest is the bead-scoped manifest shipped today. It declares
// only what this bead supports: both group modes the adaptor returns
// (static for role/convoy groups, pool for polecat groups), and the
// transport/workspace the rest of gm-e7.x will fill in. Subsequent beads
// (gm-e7.2–gm-e7.7) update this manifest as they land lifecycle features.
var gastownManifest = core.OrchestrationCapabilityManifest{
	AdaptorID:               "gastown",
	AdaptorVersion:          "0.1.0",
	OrchestrationAPIVersion: core.ProtocolVersion,
	// Transport: API (HTTP+JSON per gm-root DD-12, gm-e7.6). The v1
	// channel is the `gt` CLI invoked with --json — every method is
	// a one-shot JSON request/response cycle. See transport.go.
	Transport:            core.TransportAPI,
	WorkspaceKinds:       []core.WorkspaceKind{core.WorkspaceWorktree},
	DefaultWorkspaceKind: core.WorkspaceWorktree,
	PerKindIsolation: map[core.WorkspaceKind]core.IsolationCapabilities{
		core.WorkspaceWorktree: {FSScoped: true},
	},
	GroupModes: []core.GroupMode{core.GroupStatic, core.GroupPool},
	// ClaimModel: gt's atomic-claim primitive is `gt sling` itself —
	// the bead-already-hooked rejection inside the gt CLI is the
	// race protection. The planner does not call ClaimNextReady
	// against this adaptor (those methods deliberately return
	// KindUnsupported per gm-e3.8); on a sling collision the
	// adaptor surfaces ErrBeadAlreadyClaimed and the daemon's
	// inline-path soft-skip moves to the next candidate.
	ClaimModel: core.ClaimModelInline,
	// CostAxes: gm-e7.4 added Tokens (synthesised from Claude Code
	// transcripts via cost.go). Wallclock is wall-clock seconds
	// from session StartedAt → EndedAt. DollarsEst rolls in when
	// pricing config is present (env-driven); the axis is implicit
	// to Tokens when configured — no separate axis declaration.
	CostAxes: []core.CostAxis{core.CostWallclock, core.CostTokens},
	// EscalationKinds: gm-e7.5 surfaces every `gt escalate` row as
	// EscalationOrchestratorPause; HITLApproval stays declared for
	// the persona-suspension surface that lands with gm-uf7.
	EscalationKinds: []core.EscalationKind{
		core.EscalationOrchestratorPause,
		core.EscalationHITLApproval,
	},
	PeekModes:     []core.PeekMode{core.PeekTranscript},
	EventDelivery: core.EventDeliveryPoll,
}

// Describe returns the capability manifest. Subsequent gm-e7.x beads
// expand it as they implement more of the interface.
//
// gm-e7.12 surfaces the capability probe results in the manifest's
// Extension map under the keys:
//
//   - "gt_version"          (parsed semver, e.g. "1.0.0")
//   - "gt_version_raw"      (full --version line for diagnostics)
//   - "gt_version_floor"    (the floor we expect)
//   - "gt_version_below_floor" (true when WARN was logged)
//   - "supports_sling_json" (bool)
//   - "has_scheduler"       (bool)
//   - "has_handoff"         (bool — gates RecycleSession)
//
// A zero-valued caps struct (tests, no probe run) emits empty/false
// values which the SPA renders as "unknown" without crashing.
func (o *OrchestrationPlane) Describe() core.OrchestrationCapabilityManifest {
	m := gastownManifest
	ext := map[string]any{
		"gt_version":             o.caps.Version,
		"gt_version_raw":         o.caps.VersionRaw,
		"gt_version_floor":       minimumGtVersion,
		"gt_version_below_floor": o.caps.VersionBelowFloor,
		"supports_sling_json":    o.caps.SupportsSlingJSON,
		"has_scheduler":          o.caps.HasScheduler,
		"has_handoff":            o.caps.HasHandoff,
		"gastown_root":           o.root,
	}
	m.Extension = ext
	return m
}

// gtRig is the shape of a row from `gt rig list --json`.
type gtRig struct {
	Name         string `json:"name"`
	Prefix       string `json:"beads_prefix"`
	Status       string `json:"status"`
	Witness      string `json:"witness"`
	Refinery     string `json:"refinery"`
	Polecats     int    `json:"polecats"`
	Crew         int    `json:"crew"`
	Repository   string `json:"repository,omitempty"`
	Repo         string `json:"repo,omitempty"`
	Branch       string `json:"branch,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Path         string `json:"path,omitempty"`
	Dir          string `json:"dir,omitempty"`
}

// gtPolecat is the shape of a row from `gt polecat list --all --json`.
type gtPolecat struct {
	Rig            string `json:"rig"`
	Name           string `json:"name"`
	State          string `json:"state"`
	Issue          string `json:"issue,omitempty"`
	SessionRunning bool   `json:"session_running"`
}

func (o *OrchestrationPlane) loadRigs(ctx context.Context) ([]gtRig, error) {
	out, err := o.run(ctx, "rig", "list", "--json")
	if err != nil {
		return nil, wrapGtError(err, "gt rig list --json")
	}
	var rigs []gtRig
	if err := json.Unmarshal(out, &rigs); err != nil {
		return nil, core.WrapAdaptorError(core.KindProcessFailed, err,
			"gastown: parse gt rig list --json")
	}
	return rigs, nil
}

func (o *OrchestrationPlane) loadPolecats(ctx context.Context) ([]gtPolecat, error) {
	out, err := o.run(ctx, "polecat", "list", "--all", "--json")
	if err != nil {
		return nil, wrapGtError(err, "gt polecat list --all --json")
	}
	var pcs []gtPolecat
	if err := json.Unmarshal(out, &pcs); err != nil {
		return nil, core.WrapAdaptorError(core.KindProcessFailed, err,
			"gastown: parse gt polecat list --json")
	}
	return pcs, nil
}

// ListAgents enumerates Gas Town agents: the town-level Mayor and Deacon,
// per-rig Witness/Refinery/Crew standing agents, and spawned polecats.
// AgentFilter is applied in-memory after collection.
func (o *OrchestrationPlane) ListAgents(ctx context.Context, f core.AgentFilter) ([]core.AgentRef, error) {
	rigs, err := o.loadRigs(ctx)
	if err != nil {
		return nil, err
	}
	polecats, err := o.loadPolecats(ctx)
	if err != nil {
		return nil, err
	}

	agents := make([]core.AgentRef, 0, 4+len(rigs)*3+len(polecats))

	// Town-level singletons. Every gt town has exactly one of each; the
	// rig-list result is the proof-of-life that the town is reachable.
	agents = append(agents,
		core.AgentRef{
			ID:   agentIDMayor,
			Name: "Mayor",
			Kind: core.AgentKindAgent,
			Role: RoleMayor,
		},
		core.AgentRef{
			ID:   agentIDDeacon,
			Name: "Deacon",
			Kind: core.AgentKindAgent,
			Role: RoleDeacon,
		},
	)

	// Per-rig standing agents. Witness/Refinery presence is gated on their
	// running status; crew count produces one AgentRef per seat (`crew/<n>`
	// when the name is unavailable — the CLI exposes only a count today).
	for _, r := range rigs {
		if r.Witness == "running" {
			agents = append(agents, core.AgentRef{
				ID:        rigRoleAgentID(r.Name, RoleWitness),
				Name:      r.Name + "/witness",
				Kind:      core.AgentKindAgent,
				Role:      RoleWitness,
				Workspace: r.Name,
			})
		}
		if r.Refinery == "running" {
			agents = append(agents, core.AgentRef{
				ID:        rigRoleAgentID(r.Name, RoleRefinery),
				Name:      r.Name + "/refinery",
				Kind:      core.AgentKindAgent,
				Role:      RoleRefinery,
				Workspace: r.Name,
			})
		}
		for i := 0; i < r.Crew; i++ {
			agents = append(agents, core.AgentRef{
				ID:        core.AgentID(fmt.Sprintf("gastown/%s/crew/%d", r.Name, i)),
				Name:      fmt.Sprintf("%s/crew/%d", r.Name, i),
				Kind:      core.AgentKindHuman,
				Role:      RoleCrew,
				Workspace: r.Name,
			})
		}
	}

	for _, p := range polecats {
		agents = append(agents, core.AgentRef{
			ID:        core.AgentID(fmt.Sprintf("gastown/%s/polecats/%s", p.Rig, p.Name)),
			Name:      fmt.Sprintf("%s/polecats/%s", p.Rig, p.Name),
			Kind:      core.AgentKindAgent,
			Role:      RolePolecats,
			Workspace: p.Rig,
		})
	}

	return applyAgentFilter(agents, f), nil
}

// ReadAgent resolves a single agent id by scanning the ListAgents output.
// Returns nil when the agent is unknown (per the interface contract).
func (o *OrchestrationPlane) ReadAgent(ctx context.Context, id core.AgentID) (*core.AgentRef, error) {
	all, err := o.ListAgents(ctx, core.AgentFilter{})
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, nil
}

// ListGroups returns the role-based AgentGroups DD-7 expects: static
// groups for the singletons and per-role crews, plus one pool group per
// rig whose membership is resolved on demand by ResolveGroupMembers.
//
// Role taxonomy is preserved in Extension["role"] so the SPA can style
// each group from a stable key rather than matching on the display name.
func (o *OrchestrationPlane) ListGroups(ctx context.Context) ([]core.AgentGroup, error) {
	rigs, err := o.loadRigs(ctx)
	if err != nil {
		return nil, err
	}
	polecats, err := o.loadPolecats(ctx)
	if err != nil {
		return nil, err
	}

	rigNames := make([]string, 0, len(rigs))
	for _, r := range rigs {
		rigNames = append(rigNames, r.Name)
	}
	sort.Strings(rigNames)

	groups := make([]core.AgentGroup, 0, 4+2*len(rigs))

	// Singleton role groups. Each is a static group with exactly one
	// member so the UI can treat them uniformly with larger role groups.
	groups = append(groups, core.AgentGroup{
		ID:          groupIDRolePrefix + RoleMayor,
		DisplayName: "Mayor",
		Mode:        core.GroupStatic,
		Members:     core.GroupMembers{Static: []core.AgentID{agentIDMayor}},
		Extension:   map[string]any{"role": RoleMayor},
	})
	groups = append(groups, core.AgentGroup{
		ID:          groupIDRolePrefix + RoleDeacon,
		DisplayName: "Deacon",
		Mode:        core.GroupStatic,
		Members:     core.GroupMembers{Static: []core.AgentID{agentIDDeacon}},
		Extension:   map[string]any{"role": RoleDeacon},
	})

	// Cross-rig Witness / Refinery groups. These capture the convoy sense
	// of DD-7: an enumerated fixed crew that moves together across every
	// rig. Members are omitted for rigs whose service is not running.
	witnessMembers := make([]core.AgentID, 0, len(rigs))
	refineryMembers := make([]core.AgentID, 0, len(rigs))
	witnessRepos := make([]string, 0, len(rigs))
	refineryRepos := make([]string, 0, len(rigs))
	for _, r := range rigs {
		if r.Witness == "running" {
			witnessMembers = append(witnessMembers, rigRoleAgentID(r.Name, RoleWitness))
			witnessRepos = append(witnessRepos, r.Name)
		}
		if r.Refinery == "running" {
			refineryMembers = append(refineryMembers, rigRoleAgentID(r.Name, RoleRefinery))
			refineryRepos = append(refineryRepos, r.Name)
		}
	}
	groups = append(groups, core.AgentGroup{
		ID:          groupIDRolePrefix + RoleWitness,
		DisplayName: "Witnesses",
		Mode:        core.GroupStatic,
		Members:     core.GroupMembers{Static: witnessMembers},
		Repository:  witnessRepos,
		Extension:   map[string]any{"role": RoleWitness},
	})
	groups = append(groups, core.AgentGroup{
		ID:          groupIDRolePrefix + RoleRefinery,
		DisplayName: "Refineries",
		Mode:        core.GroupStatic,
		Members:     core.GroupMembers{Static: refineryMembers},
		Repository:  refineryRepos,
		Extension:   map[string]any{"role": RoleRefinery},
	})

	// Per-rig polecat pool. Pool membership is elastic — `gt polecat list`
	// is the canonical check endpoint; ResolveGroupMembers exposes it.
	polecatsByRig := map[string]int{}
	for _, p := range polecats {
		polecatsByRig[p.Rig]++
	}
	for _, rigName := range rigNames {
		groups = append(groups, core.AgentGroup{
			ID:          groupIDRigPrefix + rigName + "/polecats",
			DisplayName: rigName + " polecats",
			Mode:        core.GroupPool,
			Members: core.GroupMembers{
				PoolCheckURL: "gt://polecat/list?rig=" + rigName,
			},
			Repository: []string{rigName},
			Extension: map[string]any{
				"role": RolePolecats,
				"rig":  rigName,
			},
		})
	}

	return groups, nil
}

// ResolveGroupMembers returns the current members of a group. Static
// groups echo their declared members; pool groups re-run the underlying
// gt list so the caller always sees current membership.
func (o *OrchestrationPlane) ResolveGroupMembers(ctx context.Context, groupID string) ([]core.AgentRef, error) {
	groups, err := o.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	var match *core.AgentGroup
	for i := range groups {
		if groups[i].ID == groupID {
			match = &groups[i]
			break
		}
	}
	if match == nil {
		return nil, core.NewAdaptorError(core.KindSessionNotFound,
			"gastown: group %q unknown", groupID)
	}

	switch match.Mode {
	case core.GroupStatic:
		return o.resolveStaticMembers(ctx, match)
	case core.GroupPool:
		return o.resolvePoolMembers(ctx, match)
	default:
		return nil, core.NewAdaptorError(core.KindUnsupported,
			"gastown: group %q has unsupported mode %q", groupID, match.Mode)
	}
}

func (o *OrchestrationPlane) resolveStaticMembers(ctx context.Context, g *core.AgentGroup) ([]core.AgentRef, error) {
	all, err := o.ListAgents(ctx, core.AgentFilter{})
	if err != nil {
		return nil, err
	}
	byID := make(map[core.AgentID]core.AgentRef, len(all))
	for _, a := range all {
		byID[a.ID] = a
	}
	out := make([]core.AgentRef, 0, len(g.Members.Static))
	for _, id := range g.Members.Static {
		if a, ok := byID[id]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

func (o *OrchestrationPlane) resolvePoolMembers(ctx context.Context, g *core.AgentGroup) ([]core.AgentRef, error) {
	rig, _ := g.Extension["rig"].(string)
	if rig == "" {
		return nil, core.NewAdaptorError(core.KindValidation,
			"gastown: pool group %q missing extension[\"rig\"]", g.ID)
	}
	polecats, err := o.loadPolecats(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.AgentRef, 0, len(polecats))
	for _, p := range polecats {
		if p.Rig != rig {
			continue
		}
		out = append(out, core.AgentRef{
			ID:        core.AgentID(fmt.Sprintf("gastown/%s/polecats/%s", p.Rig, p.Name)),
			Name:      fmt.Sprintf("%s/polecats/%s", p.Rig, p.Name),
			Kind:      core.AgentKindAgent,
			Role:      RolePolecats,
			Workspace: p.Rig,
		})
	}
	return out, nil
}

func rigRoleAgentID(rig, role string) core.AgentID {
	return core.AgentID(fmt.Sprintf("gastown/%s/%s", rig, role))
}

func applyAgentFilter(in []core.AgentRef, f core.AgentFilter) []core.AgentRef {
	if f.ParentID == nil && f.Kind == "" && f.GroupID == "" && f.Workspace == "" {
		return in
	}
	out := in[:0:len(in)]
	for _, a := range in {
		if f.ParentID != nil {
			if a.ParentID == nil || *a.ParentID != *f.ParentID {
				continue
			}
		}
		if f.Kind != "" && a.Kind != f.Kind {
			continue
		}
		if f.Workspace != "" && a.Workspace != f.Workspace {
			continue
		}
		if f.GroupID != "" && !agentInGroup(a, f.GroupID) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// agentInGroup reports whether a role-based group ID matches an agent by
// role, mirroring the membership logic ListGroups encodes. Pool groups
// check by workspace rather than role since the rig is their scope.
func agentInGroup(a core.AgentRef, groupID string) bool {
	switch {
	case strings.HasPrefix(groupID, groupIDRolePrefix):
		return a.Role == strings.TrimPrefix(groupID, groupIDRolePrefix)
	case strings.HasPrefix(groupID, groupIDRigPrefix) && strings.HasSuffix(groupID, "/polecats"):
		rig := strings.TrimSuffix(strings.TrimPrefix(groupID, groupIDRigPrefix), "/polecats")
		return a.Role == RolePolecats && a.Workspace == rig
	default:
		return false
	}
}

func wrapGtError(cause error, cmdline string) error {
	msg := strings.TrimSpace(cause.Error())
	if exitErr, ok := cause.(*exec.ExitError); ok {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			msg = stderr
		}
	}
	return core.WrapAdaptorError(core.KindAdaptorDegraded, cause,
		"gastown: %s: %s", cmdline, firstLineErr(msg))
}

func firstLineErr(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// --- Remaining OrchestrationPlaneAdaptor methods ---
//
// Workspace lifecycle (AcquireWorkspace / ReleaseWorkspace /
// InspectWorkspace) is implemented in workspace.go (gm-e7.2).
// Cost synthesis ships in cost.go (gm-e7.4). Escalation listing ships
// in escalations.go (gm-e7.5). Session lifecycle ships in sessions.go
// (gm-e7.9). The remaining queue-claim methods (ClaimNextReady /
// ReleaseReservation) stay tagged-unsupported: gt has its own claim
// semantics (sling/hook/unsling) that don't map onto the
// reservation/ttl shape Gemba's pull strategy needs. Adding a
// reservation surface is filed as follow-up under gm-e7.

func (o *OrchestrationPlane) DeclaredState(context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{CapturedAt: time.Now()}, nil
}

func (o *OrchestrationPlane) ObservedState(ctx context.Context) (core.WorkspaceTopology, error) {
	agents, err := o.ListAgents(ctx, core.AgentFilter{})
	if err != nil {
		return core.WorkspaceTopology{}, err
	}
	groups, err := o.ListGroups(ctx)
	if err != nil {
		return core.WorkspaceTopology{}, err
	}
	return core.WorkspaceTopology{
		Agents:     agents,
		Groups:     groups,
		CapturedAt: time.Now(),
	}, nil
}

func (o *OrchestrationPlane) ClaimNextReady(context.Context, core.ReadyFilter, core.AgentRef) (*core.Reservation, error) {
	return nil, unsupported("ClaimNextReady")
}

func (o *OrchestrationPlane) ReleaseReservation(context.Context, string) error {
	return unsupported("ReleaseReservation")
}

// RecycleSession (gm-e7.13) lives in sessions.go alongside the rest
// of the lifecycle code. It wraps `gt handoff <session-id>` and
// honours the §5.2 dirty-worktree refusal contract.

// ListOpenEscalations is implemented in escalations.go (gm-e7.5).
// Session lifecycle (StartSession, PauseSession, ResumeSession,
// EndSession, PeekSession, ListPendingRequests, ListSessions,
// ResolveEscalation, Subscribe, RecycleSession) is implemented in
// sessions.go (gm-e7.9, gm-e7.13).

func unsupported(method string) error {
	return core.NewAdaptorError(core.KindUnsupported,
		"gastown: %s not supported by gt CLI (filed under gm-e7 follow-up)", method)
}
