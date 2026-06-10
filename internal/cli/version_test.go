package cli

import (
	"runtime/debug"
	"testing"
)

func TestEnrichBuildInfo_LDFlagsWin(t *testing.T) {
	b := BuildInfo{Version: "v1.2.3", Commit: "deadbee", Date: "2026-01-01T00:00:00Z"}
	got := enrichBuildInfo(b, panicRead, panicProbe)
	if got != b {
		t.Errorf("non-placeholder BuildInfo must be preserved verbatim: got %+v want %+v", got, b)
	}
}

func TestEnrichBuildInfo_DebugVCSFallback(t *testing.T) {
	b := BuildInfo{Version: "dev", Commit: "none", Date: "unknown"}
	read := func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v0.9.0"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef1234567890"},
				{Key: "vcs.modified", Value: "true"},
				{Key: "vcs.time", Value: "2026-02-02T02:02:02Z"},
			},
		}, true
	}
	got := enrichBuildInfo(b, read, panicProbe)
	if got.Version != "v0.9.0" || got.Commit != "abcdef1-dirty" || got.Date != "2026-02-02T02:02:02Z" {
		t.Errorf("unexpected enrichment: %+v", got)
	}
}

func TestEnrichBuildInfo_GitProbeFallback(t *testing.T) {
	b := BuildInfo{Version: "dev", Commit: "none", Date: "unknown"}
	read := func() (*debug.BuildInfo, bool) { return nil, false }
	probe := func() gitInfo {
		return gitInfo{Revision: "cafef00d", Modified: false, Time: "2026-03-03T03:03:03Z"}
	}
	got := enrichBuildInfo(b, read, probe)
	if got.Commit != "cafef00d" || got.Date != "2026-03-03T03:03:03Z" {
		t.Errorf("git probe fallback failed: %+v", got)
	}
	if got.Version != "dev" {
		t.Errorf("version should remain 'dev' when only git probe data is available: %q", got.Version)
	}
}

func TestEnrichBuildInfo_AllSilent(t *testing.T) {
	b := BuildInfo{Version: "dev", Commit: "none", Date: "unknown"}
	read := func() (*debug.BuildInfo, bool) { return nil, false }
	probe := func() gitInfo { return gitInfo{} }
	got := enrichBuildInfo(b, read, probe)
	if got != b {
		t.Errorf("placeholders must survive when no source has data: got %+v want %+v", got, b)
	}
}

func panicRead() (*debug.BuildInfo, bool) {
	panic("read should not be called when ldflags already populated BuildInfo")
}

func panicProbe() gitInfo {
	panic("probe should not be called when ldflags already populated BuildInfo")
}
