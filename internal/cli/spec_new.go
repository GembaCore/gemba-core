// spec_new.go — ASDD Phase 2 scaffolding for the canonical
// docs/specs/<YYYY-MM-DD>-<slug>.md document and its companion lockfile.
//
// The Phase-1 spec-kit behavior (specs/<slug>/spec.md, .claude/commands)
// continues to live in spec.go.  This file adds the Phase-2 surface
// invoked by the same `gemba spec new <slug>` command.

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"

	"github.com/GembaCore/gemba-core/internal/spec"
	"github.com/GembaCore/gemba-core/internal/spec/lockfile"
)

// ErrSpecExists is returned by writePhase2Spec when the target file
// already exists and --force was not supplied.
var ErrSpecExists = errors.New("spec: target file exists (use --force)")

// phase2TemplateData is the data passed to spec.md.tmpl.
type phase2TemplateData struct {
	SpecID  string
	Title   string
	Created string
	Owner   string
	Nonce   string
}

// writePhase2Spec creates docs/specs/<YYYY-MM-DD>-<slug>.md and a
// sibling .spec.lock.json with empty mappings.  It returns the absolute
// path of the spec file and the lock file it wrote (or would have
// written if force=false and a collision occurred).
//
// projectRoot is the directory under which `docs/specs` lives.
// today provides the date prefix; injected so tests can pin it.
func writePhase2Spec(projectRoot, slug string, today time.Time, force bool, owner string) (specPath, lockPath string, err error) {
	if strings.TrimSpace(slug) == "" {
		return "", "", errors.New("spec: empty slug")
	}
	dateStr := today.Format("2006-01-02")
	specDir := filepath.Join(projectRoot, "docs", "specs")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		return "", "", err
	}
	fileName := fmt.Sprintf("%s-%s.md", dateStr, slug)
	specPath = filepath.Join(specDir, fileName)
	lockPath = filepath.Join(specDir, fmt.Sprintf(".%s-%s.spec.lock.json", dateStr, slug))

	if _, statErr := os.Stat(specPath); statErr == nil && !force {
		return specPath, lockPath, fmt.Errorf("%w: %s", ErrSpecExists, specPath)
	}

	nonce := uuid.NewString()
	data := phase2TemplateData{
		SpecID:  fmt.Sprintf("SP-%s-%s", dateStr, slug),
		Title:   slug,
		Created: today.UTC().Format(time.RFC3339),
		Owner:   owner,
		Nonce:   nonce,
	}

	tmpl, err := template.New("spec.md").Parse(spec.SpecMDTemplate)
	if err != nil {
		return specPath, lockPath, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return specPath, lockPath, err
	}
	if err := os.WriteFile(specPath, buf.Bytes(), 0o644); err != nil {
		return specPath, lockPath, err
	}

	// Seed an empty lockfile alongside the spec.  Use the same nonce so
	// the first reconciler run can detect that nothing has been applied.
	lock := &lockfile.Lock{
		SpecID:    data.SpecID,
		Nonce:     nonce,
		AppliedAt: today.UTC(),
		Mappings:  []lockfile.Mapping{},
	}
	if err := lockfile.Save(lockPath, lock); err != nil {
		return specPath, lockPath, err
	}
	return specPath, lockPath, nil
}

// resolveOwner returns `git config user.email` from the project root, or
// the GIT_AUTHOR_EMAIL env, or "unknown" if neither is set.
func resolveOwner(projectRoot string) string {
	if v := strings.TrimSpace(os.Getenv("GIT_AUTHOR_EMAIL")); v != "" {
		return v
	}
	cmd := exec.Command("git", "config", "user.email")
	cmd.Dir = projectRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err == nil {
		if v := strings.TrimSpace(out.String()); v != "" {
			return v
		}
	}
	return "unknown"
}
