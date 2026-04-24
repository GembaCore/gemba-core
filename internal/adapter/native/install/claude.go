// Claude installer (gm-native.18). Manages three artifacts in the
// workspace:
//
//   .claude/settings.local.json — JSON merge: hooks key + sentinel.
//                                  Operator's other keys are preserved.
//   CLAUDE.md                   — sentinel-block merge: gemba content
//                                  lives between MarkerStart/MarkerEnd
//                                  comment lines; everything outside
//                                  is operator-owned and untouched.
//   .claude/skills/             — copy bundled coaching + manager skill
//                                  files when absent. Existing skills
//                                  are NEVER touched.

package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SentinelKey marks settings.local.json as gemba-managed so a second
// run can detect "already installed" without re-parsing our stanza.
const SentinelKey = "_gemba_bridge"

// MarkerStart / MarkerEnd bracket the gemba-owned region of CLAUDE.md.
// Anything between these lines is owned by gemba and replaced on
// install; anything outside is operator-owned and preserved.
const (
	MarkerStart = "<!-- gemba-bridge:start -->"
	MarkerEnd   = "<!-- gemba-bridge:end -->"
)

// SkillsFS is the embedded.FS the binary ships with — coaching +
// manager skill files. Set by the cmd/gemba-bridge skills package via
// init; left nil in this package so the install layer doesn't pull
// the embed in itself (keeps the install package importable from tests
// without dragging the whole bridge binary).
var SkillsFS fs.FS

// SetSkillsFS lets the binary inject the embedded skills FS at startup.
// Tests can also call this to inject a fixture FS.
func SetSkillsFS(f fs.FS) { SkillsFS = f }

// claudeInstaller implements Installer for the "claude" agent.
type claudeInstaller struct{}

// NewClaude returns a fresh Claude installer. Exposed for tests that
// don't want to depend on the global registry.
func NewClaude() Installer { return claudeInstaller{} }

func (claudeInstaller) Name() string { return "claude" }

func (claudeInstaller) Install(_ context.Context, opts Options) (Report, error) {
	if opts.Dir == "" {
		return Report{Agent: "claude"}, fmt.Errorf("install/claude: Options.Dir required")
	}
	rep := Report{Agent: "claude"}

	if a, err := installSettingsJSON(opts); err != nil {
		return rep, err
	} else {
		rep.Actions = append(rep.Actions, a)
	}
	if a, err := installClaudeMD(opts); err != nil {
		return rep, err
	} else {
		rep.Actions = append(rep.Actions, a)
	}
	skillActions, err := installSkills(opts, SkillsFS)
	rep.Actions = append(rep.Actions, skillActions...)
	if err != nil {
		return rep, err
	}
	return rep, nil
}

// ---------- settings.local.json ----------

func installSettingsJSON(opts Options) (Action, error) {
	path := filepath.Join(opts.Dir, ".claude", "settings.local.json")
	existing := map[string]interface{}{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &existing); err != nil {
			return Action{Path: path, Kind: "preserved",
				Reason: "existing JSON unparseable; left untouched"}, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if _, alreadyOurs := existing[SentinelKey]; alreadyOurs {
		return Action{Path: path, Kind: "skipped", Reason: "sentinel present; already installed"}, nil
	}
	preexisting := len(existing) > 0
	existing["hooks"] = claudeHookStanza()
	existing[SentinelKey] = map[string]interface{}{
		"profile": "claude",
		"version": "2",
	}
	if opts.DryRun {
		kind := "created"
		if preexisting {
			kind = "merged"
		}
		return Action{Path: path, Kind: kind, Reason: "dry-run"}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Action{Path: path}, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	body, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return Action{Path: path}, fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return Action{Path: path}, fmt.Errorf("write %s: %w", path, err)
	}
	if preexisting {
		return Action{Path: path, Kind: "merged",
			Reason: "added hooks + sentinel; preserved operator keys"}, nil
	}
	return Action{Path: path, Kind: "created", Reason: "fresh install"}, nil
}

func claudeHookStanza() map[string]interface{} {
	entry := func() map[string]interface{} {
		return map[string]interface{}{
			"hooks": []map[string]interface{}{
				{"type": "command", "command": "gemba-bridge"},
			},
		}
	}
	return map[string]interface{}{
		"SessionStart":     []map[string]interface{}{entry()},
		"UserPromptSubmit": []map[string]interface{}{entry()},
		"PreToolUse": []map[string]interface{}{
			{"matcher": "*", "hooks": entry()["hooks"]},
		},
		"PostToolUse": []map[string]interface{}{
			{"matcher": "Bash", "hooks": entry()["hooks"]},
		},
		"Notification": []map[string]interface{}{entry()},
		"Stop":         []map[string]interface{}{entry()},
	}
}

// ---------- CLAUDE.md ----------

// claudeMDBlock is what we write between MarkerStart / MarkerEnd. Kept
// terse on purpose: gemba's preamble pipeline (gm-native.10) injects
// the per-session detail; this static block just announces gemba-bridge
// is wired and points at the skills directory.
const claudeMDBlock = `## gemba-bridge

Native sessions emit structured events to the gemba server via
.claude/settings.local.json hooks. This worktree was set up by
` + "`gemba install-bridge`" + ` (or the StartSession eager install).

Coaching + manager skills live under .claude/skills/. Per-session
preamble (project values + epic context + work-item detail) is
injected by the orchestrator at start time; you don't need to manage
it from this file.`

func installClaudeMD(opts Options) (Action, error) {
	path := filepath.Join(opts.Dir, "CLAUDE.md")
	current, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		// Fresh write: just our block, no surrounding markers needed
		// since the file didn't exist (operator can add their own
		// content above/below later and we'll preserve it on re-run).
		body := MarkerStart + "\n" + claudeMDBlock + "\n" + MarkerEnd + "\n"
		if opts.DryRun {
			return Action{Path: path, Kind: "created", Reason: "dry-run"}, nil
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return Action{Path: path}, fmt.Errorf("write %s: %w", path, err)
		}
		return Action{Path: path, Kind: "created", Reason: "fresh install"}, nil
	case err != nil:
		return Action{Path: path}, fmt.Errorf("read %s: %w", path, err)
	}
	// File exists — check for our markers. If present, replace the
	// block; otherwise prepend ours and preserve everything that was
	// there.
	if hasMarkers(string(current)) {
		merged := replaceBlock(string(current))
		if string(current) == merged {
			return Action{Path: path, Kind: "skipped",
				Reason: "sentinel-block content unchanged"}, nil
		}
		if opts.DryRun {
			return Action{Path: path, Kind: "merged", Reason: "dry-run; would replace block"}, nil
		}
		if err := os.WriteFile(path, []byte(merged), 0o644); err != nil {
			return Action{Path: path}, fmt.Errorf("write %s: %w", path, err)
		}
		return Action{Path: path, Kind: "merged",
			Reason: "replaced sentinel block; preserved surrounding bytes"}, nil
	}
	// No markers; prepend our block + a separator.
	prepended := MarkerStart + "\n" + claudeMDBlock + "\n" + MarkerEnd + "\n\n" + string(current)
	if opts.DryRun {
		return Action{Path: path, Kind: "merged",
			Reason: "dry-run; would prepend sentinel block"}, nil
	}
	if err := os.WriteFile(path, []byte(prepended), 0o644); err != nil {
		return Action{Path: path}, fmt.Errorf("write %s: %w", path, err)
	}
	return Action{Path: path, Kind: "merged",
		Reason: "prepended sentinel block; preserved existing content"}, nil
}

func hasMarkers(s string) bool {
	return strings.Contains(s, MarkerStart) && strings.Contains(s, MarkerEnd)
}

// replaceBlock swaps the CURRENT content between MarkerStart and
// MarkerEnd with our latest claudeMDBlock. Outside-the-markers bytes
// are preserved verbatim.
var blockRE = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(MarkerStart) + `.*?` + regexp.QuoteMeta(MarkerEnd))

func replaceBlock(s string) string {
	return blockRE.ReplaceAllString(s, MarkerStart+"\n"+claudeMDBlock+"\n"+MarkerEnd)
}

// ---------- skills ----------

// installSkills walks the embedded skills FS and copies each file into
// .claude/skills/<rel-path>, but ONLY if the destination doesn't exist.
// Operator-authored skills are sacred — we never overwrite them.
func installSkills(opts Options, src fs.FS) ([]Action, error) {
	if src == nil {
		return nil, nil
	}
	skillsRoot := filepath.Join(opts.Dir, ".claude", "skills")
	var actions []Action
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		dest := filepath.Join(skillsRoot, p)
		if _, err := os.Stat(dest); err == nil {
			actions = append(actions, Action{Path: dest, Kind: "preserved",
				Reason: "operator skill file present; never clobber"})
			return nil
		}
		if opts.DryRun {
			actions = append(actions, Action{Path: dest, Kind: "created",
				Reason: "dry-run; would copy bundled skill"})
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
		}
		body, err := fs.ReadFile(src, p)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", p, err)
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		actions = append(actions, Action{Path: dest, Kind: "created",
			Reason: "copied bundled skill"})
		return nil
	})
	return actions, err
}

func init() {
	Register(NewClaude())
}
