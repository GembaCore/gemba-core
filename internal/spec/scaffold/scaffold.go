// Package scaffold writes ASDD project artifacts (slash commands, hooks,
// constitution, CLAUDE.md stanza) into a target project root. All operations
// are idempotent — re-running over an existing project is a no-op.
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GembaCore/gemba-core/internal/spec"
)

// Options controls what to scaffold.
type Options struct {
	Slug               string // for {{slug}} replacement in slash-command templates
	WriteSlashCommands bool
	WriteHooks         bool
	WriteConstitution  bool
	UpdateClaudeMD     bool
}

// Result records the paths created or updated.
type Result struct {
	Created []string
	Updated []string
}

// Apply scaffolds artifacts into projectRoot per opts.
func Apply(projectRoot string, opts Options) (Result, error) {
	var r Result
	markCreated := func(path string, created bool) {
		if created {
			r.Created = append(r.Created, path)
		}
	}

	if opts.WriteSlashCommands {
		for name, body := range map[string]string{
			"tasks.md":     spec.TasksCommandTemplate,
			"implement.md": spec.ImplementCommandTemplate,
		} {
			out := strings.ReplaceAll(body, "{{slug}}", opts.Slug)
			path := filepath.Join(projectRoot, ".claude", "commands", name)
			created, err := writeIfAbsent(path, out)
			if err != nil {
				return r, err
			}
			markCreated(path, created)
		}
	}

	if opts.WriteHooks {
		path := filepath.Join(projectRoot, ".claude", "hooks.json")
		created, err := writeIfAbsent(path, spec.HooksJSONTemplate)
		if err != nil {
			return r, err
		}
		markCreated(path, created)
	}

	if opts.WriteConstitution {
		path := filepath.Join(projectRoot, ".gemba", "constitution.md")
		created, err := writeIfAbsent(path, spec.ConstitutionTemplate)
		if err != nil {
			return r, err
		}
		markCreated(path, created)
	}

	if opts.UpdateClaudeMD {
		path := filepath.Join(projectRoot, "CLAUDE.md")
		created, appended, err := appendStanza(path, spec.ClaudeMDTemplate, "## Working in this project")
		if err != nil {
			return r, err
		}
		if created {
			r.Created = append(r.Created, path)
		} else if appended {
			r.Updated = append(r.Updated, path)
		}
	}

	return r, nil
}

func writeIfAbsent(path, content string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func appendStanza(path, stanza, marker string) (created, appended bool, err error) {
	existing, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, false, readErr
	}
	if os.IsNotExist(readErr) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return false, false, err
		}
		if err := os.WriteFile(path, []byte(stanza), 0o644); err != nil {
			return false, false, err
		}
		return true, false, nil
	}
	if strings.Contains(string(existing), marker) {
		return false, false, nil
	}
	body := string(existing)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "\n" + stanza
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return false, false, fmt.Errorf("append CLAUDE.md: %w", err)
	}
	return false, true, nil
}
