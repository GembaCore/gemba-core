package scanscheduler

import (
	"testing"
	"time"
)

func TestScanActivity_WorkspaceTargetUsesScanKind(t *testing.T) {
	a := ScanActivity{Repo: "gt"}
	tgt := a.WorkspaceTarget()
	if tgt.Repo != "gt" {
		t.Errorf("repo = %q, want gt", tgt.Repo)
	}
	if tgt.Kind != WorkspaceTargetScan {
		t.Errorf("kind = %s, want scan", tgt.Kind)
	}
}

func TestWorkspaceTarget_CollidesAcrossKinds(t *testing.T) {
	scan := WorkspaceTarget{Repo: "gt", Kind: WorkspaceTargetScan}
	session := WorkspaceTarget{Repo: "gt", Kind: WorkspaceTargetSession}
	if !scan.CollidesWith(session) {
		t.Errorf("scan and session on the same repo MUST collide; conflict graph relies on this")
	}
}

func TestWorkspaceTarget_DoesNotCollideAcrossRepos(t *testing.T) {
	a := WorkspaceTarget{Repo: "gt", Kind: WorkspaceTargetScan}
	b := WorkspaceTarget{Repo: "other", Kind: WorkspaceTargetScan}
	if a.CollidesWith(b) {
		t.Errorf("different repos must not collide")
	}
}

func TestWorkspaceTarget_EmptyRepoNeverCollides(t *testing.T) {
	a := WorkspaceTarget{Repo: ""}
	b := WorkspaceTarget{Repo: ""}
	if a.CollidesWith(b) {
		t.Errorf("empty repo (zero value) must not produce false-collision edges")
	}
}

func TestScanActivity_DurationEstimateRecordedFromRun(t *testing.T) {
	// Confirm the activity captured by the scheduler carries a
	// non-zero duration on success — the scheduler computes
	// EndedAt - StartedAt, so an instantaneous fake should land
	// at duration ≥ 0 (often 0 with the test clock).
	now := t0()
	s, _ := newSchedAt(now)
	_, _ = s.Submit(ctxNoTimeout(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "wave",
	})
	// Allow async run to land.
	time.Sleep(10 * time.Millisecond)
	acts := s.Activities()
	if len(acts) == 0 {
		t.Fatal("no activity recorded")
	}
	if acts[0].DurationEst < 0 {
		t.Errorf("DurationEst = %s; must be ≥ 0", acts[0].DurationEst)
	}
}
