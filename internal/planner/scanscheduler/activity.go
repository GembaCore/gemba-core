package scanscheduler

import (
	"time"
)

// ScanStatus is the terminal result of a scan run.
type ScanStatus int

const (
	// ScanRunning is the placeholder for an entry whose end-time
	// hasn't landed yet. Activities() snapshots may briefly include
	// this; finalised entries are Succeeded / Failed / SkippedCooldown.
	ScanRunning ScanStatus = iota
	ScanSucceeded
	ScanFailed
	ScanSkippedCooldown
)

func (s ScanStatus) String() string {
	switch s {
	case ScanRunning:
		return "running"
	case ScanSucceeded:
		return "succeeded"
	case ScanFailed:
		return "failed"
	case ScanSkippedCooldown:
		return "skipped-cooldown"
	default:
		return "unknown"
	}
}

// ScanActivity is one entry in the planner's scan-activity stream
// (gm-s47n.9.4). Designed to be:
//
//   - participating in the workspace-conflict graph: WorkspaceTarget
//     names the (repo, "scan") pair so the conflict scorer can flag
//     a session that wants to write into the same repo while a scan
//     is in flight.
//   - duration-aware: DurationEst is filled from the last N runs of
//     the same trigger kind so the scheduler can decide whether a
//     given scan is fast enough to block dispatch on (sync) or must
//     run async.
//   - stream-loggable: the same record shape feeds the operator-
//     visible activity log alongside dispatch + retrospective
//     entries, so an operator browsing "what did the planner do at
//     14:02" sees scans in the same column as sessions.
//
// Wire-shape stable: every field is JSON-tag-friendly so the SPA's
// /insights surface can render the activity stream without a
// translation layer.
type ScanActivity struct {
	Repo        string        `json:"repo"`
	Trigger     TriggerKind   `json:"trigger"`
	Status      ScanStatus    `json:"status"`
	StartedAt   time.Time     `json:"started_at"`
	EndedAt     time.Time     `json:"ended_at,omitempty"`
	DurationEst time.Duration `json:"duration_estimate_ns,omitempty"`
	// Reason mirrors the triggering Trigger.Reason verbatim so a
	// reader of the activity log can see WHY the scan ran without
	// joining back to the trigger stream.
	Reason string `json:"reason,omitempty"`
	// Error is non-empty only when Status == ScanFailed. The
	// scheduler's retry policy (TODO: file under gm-s47n.9 if it
	// turns out to be needed) reads this to decide whether the next
	// firing of the same trigger should bypass cooldown.
	Error string `json:"error,omitempty"`
}

// WorkspaceTarget returns the (repo, "scan") pair the workspace-
// conflict scorer in internal/planner/conflicts uses. A scan and a
// session that both want exclusive access to the same repo's
// worktree should not run together — the scan would index a
// half-mutated tree.
//
// The "scan" suffix differentiates this from a session's
// (repo, branch) target so the conflicts.WorkspaceCollisionDetector
// can route both kinds of activity through one comparator.
func (a ScanActivity) WorkspaceTarget() WorkspaceTarget {
	return WorkspaceTarget{Repo: a.Repo, Kind: WorkspaceTargetScan}
}

// WorkspaceTargetKind names the kind of operational target a
// workspace-conflict edge represents. Sessions and scans are the two
// today; future kinds (lockfile-touching jobs, snapshot ops) plug in
// here.
type WorkspaceTargetKind int

const (
	WorkspaceTargetSession WorkspaceTargetKind = iota
	WorkspaceTargetScan
)

func (k WorkspaceTargetKind) String() string {
	switch k {
	case WorkspaceTargetSession:
		return "session"
	case WorkspaceTargetScan:
		return "scan"
	default:
		return "unknown"
	}
}

// WorkspaceTarget is the (repo, kind) pair the workspace-conflict
// scorer compares. Two activities collide when their targets share a
// repo regardless of kind — a scan vs a session both touch the
// worktree.
type WorkspaceTarget struct {
	Repo string
	Kind WorkspaceTargetKind
}

// CollidesWith reports whether two workspace targets share an
// operational footprint. The comparison is repo-only at the
// scheduler boundary; finer-grained gates (which paths inside the
// repo) live in the conflicts package's TargetOverlap scorer.
func (t WorkspaceTarget) CollidesWith(other WorkspaceTarget) bool {
	return t.Repo != "" && t.Repo == other.Repo
}
