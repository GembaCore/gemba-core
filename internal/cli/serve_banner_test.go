package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/core"
)

func bannerManifest() core.CapabilityManifest {
	return core.CapabilityManifest{
		AdaptorName:     "beads",
		AdaptorVersion:  "0.1.0",
		ProtocolVersion: core.ProtocolVersion,
		Transport:       core.TransportAPI,
		StateMap: core.StateMap{
			"backlog":     core.StateBacklog,
			"open":        core.StateUnstarted,
			"in_progress": core.StateStarted,
			"closed":      core.StateCompleted,
			"abandoned":   core.StateCanceled,
		},
		EdgeExtensions: []core.EdgeExtension{
			{Name: "depends_on"}, {Name: "discovered_from"},
			{Name: "pr_branch"}, {Name: "epic_of"},
		},
	}
}

// TestPrintStartupBanner_BdDirShape pins the operator-facing banner shape for
// the bd-dir path: three lines, version + listen + auth on line 1, workplane
// adaptor + mode + dir on line 2, manifest state/edge/flag digest on line 3.
// Shape regressions break gm-root.1.8 manual smoke, so this is a tripwire.
//
// Shader identity is intentionally NOT in the banner — it's implied by
// the orchestration adaptor configuration, so reporting it would be
// redundant noise.
func TestPrintStartupBanner_BdDirShape(t *testing.T) {
	var buf bytes.Buffer
	b := BuildInfo{Version: "v0.3.1"}
	cfg := config.ServeConfig{Listen: "127.0.0.1", Port: 7666}
	reg := &workPlaneReg{
		Manifest:   bannerManifest(),
		Mode:       "bd-dir",
		SourceKind: "dir",
		Source:     "/Users/x/gt/gemba",
	}
	printStartupBanner(&buf, b, cfg, reg)

	got := buf.String()
	wants := []string{
		"▶ gemba v0.3.1  listen=127.0.0.1:7666  auth=none",
		"▶ workplane: beads 0.1.0 (mode=bd-dir dir=/Users/x/gt/gemba)",
		"▶ manifest: 5 states, 3 core + 4 extension edges, feature flags: sprint=no budget=no",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("banner missing line %q\nfull output:\n%s", w, got)
		}
	}
	if lines := strings.Count(strings.TrimSpace(got), "\n"); lines != 2 {
		t.Errorf("banner should be exactly three lines; got %d newlines\noutput:\n%s",
			lines, got)
	}
}

// TestPrintStartupBanner_DoltRedacted guards the credential-leak surface: the
// banner must carry whatever Source the caller hands it verbatim, and the
// caller (registerDoltWorkPlane) is responsible for redaction. This test
// flows through redactDoltURL to pin the integration.
func TestPrintStartupBanner_DoltRedacted(t *testing.T) {
	var buf bytes.Buffer
	b := BuildInfo{Version: "v0.3.1"}
	cfg := config.ServeConfig{Listen: "127.0.0.1", Port: 7666, AuthMode: "token"}
	reg := &workPlaneReg{
		Manifest:   bannerManifest(),
		Mode:       "dolt-sql",
		SourceKind: "url",
		Source:     redactDoltURL("mysql://root:hunter2@127.0.0.1:3307/gemba"),
	}
	printStartupBanner(&buf, b, cfg, reg)

	got := buf.String()
	if strings.Contains(got, "hunter2") {
		t.Fatalf("banner leaked password:\n%s", got)
	}
	if !strings.Contains(got, "mode=dolt-sql") {
		t.Errorf("want mode=dolt-sql in banner; got:\n%s", got)
	}
	if !strings.Contains(got, "auth=token") {
		t.Errorf("want auth=token in banner; got:\n%s", got)
	}
}

// TestPrintStartupBanner_VersionFallback covers the `go run` / unstamped-build
// case: BuildInfo.Version empty must render as "dev" rather than an empty
// token that would produce "▶ gemba   listen=…".
func TestPrintStartupBanner_VersionFallback(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.ServeConfig{Listen: "127.0.0.1", Port: 7666}
	reg := &workPlaneReg{Manifest: bannerManifest(), Mode: "bd-dir", SourceKind: "dir", Source: "/tmp"}
	printStartupBanner(&buf, BuildInfo{}, cfg, reg)

	if !strings.Contains(buf.String(), "gemba dev ") {
		t.Errorf("want 'gemba dev ' fallback in banner; got:\n%s", buf.String())
	}
}

// TestPrintStartupBanner_FlagsYes toggles SprintNative and TokenBudgetEnforced
// to verify the sprint=/budget= projection is real, not a hardcoded "no".
func TestPrintStartupBanner_FlagsYes(t *testing.T) {
	var buf bytes.Buffer
	m := bannerManifest()
	m.SprintNative = true
	m.TokenBudgetEnforced = true
	cfg := config.ServeConfig{Listen: "127.0.0.1", Port: 7666}
	reg := &workPlaneReg{Manifest: m, Mode: "bd-dir", SourceKind: "dir", Source: "/tmp"}
	printStartupBanner(&buf, BuildInfo{Version: "v1"}, cfg, reg)

	if !strings.Contains(buf.String(), "sprint=yes budget=yes") {
		t.Errorf("want 'sprint=yes budget=yes'; got:\n%s", buf.String())
	}
}
