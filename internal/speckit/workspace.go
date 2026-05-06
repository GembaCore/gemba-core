package speckit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxEditableSpecKitFileBytes = 512 * 1024

type Workspace struct {
	Configured       bool            `json:"configured"`
	ScaffoldPresent  bool            `json:"scaffold_present"`
	Root             string          `json:"root"`
	Files            []WorkspaceFile `json:"files"`
	Features         []Feature       `json:"features"`
	RecommendedRoot  string          `json:"recommended_root"`
	InitializedPaths []string        `json:"initialized_paths,omitempty"`
}

type WorkspaceFile struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	FeatureID string `json:"feature_id,omitempty"`
	Size      int64  `json:"size"`
	Modified  string `json:"modified,omitempty"`
}

type FileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Modified string `json:"modified,omitempty"`
	Size     int64  `json:"size"`
}

type NewFeatureOptions struct {
	ID    string
	Title string
}

func (s *Scanner) Workspace(ctx context.Context) (Workspace, error) {
	list, err := s.List(ctx)
	if err != nil {
		return Workspace{}, err
	}
	root, err := s.absRoot()
	if err != nil {
		return Workspace{}, err
	}
	files, err := s.ListFiles(ctx)
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{
		Configured:      list.Configured,
		ScaffoldPresent: scaffoldPresent(root),
		Root:            root,
		Files:           files,
		Features:        list.Features,
		RecommendedRoot: "specs",
	}, nil
}

func (s *Scanner) ListFiles(ctx context.Context) ([]WorkspaceFile, error) {
	root, err := s.absRoot()
	if err != nil {
		return nil, err
	}
	var files []WorkspaceFile
	for _, relRoot := range []string{"specs", ".specify"} {
		base := filepath.Join(root, filepath.FromSlash(relRoot))
		info, err := os.Stat(base)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			name := entry.Name()
			if entry.IsDir() {
				if strings.HasPrefix(name, ".") && name != ".specify" {
					return filepath.SkipDir
				}
				return nil
			}
			if !isEditableSpecKitFile(name) {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			rel := filepath.ToSlash(pathRel(root, path))
			files = append(files, WorkspaceFile{
				Path:      rel,
				Name:      name,
				Role:      fileRole(rel),
				FeatureID: featureIDForPath(rel),
				Size:      info.Size(),
				Modified:  info.ModTime().UTC().Format(time.RFC3339),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool {
		li, lj := fileSortKey(files[i]), fileSortKey(files[j])
		if li != lj {
			return li < lj
		}
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func (s *Scanner) ReadWorkspaceFile(ctx context.Context, rel string) (FileContent, error) {
	if err := ctx.Err(); err != nil {
		return FileContent{}, err
	}
	path, rel, err := s.resolveEditablePath(rel)
	if err != nil {
		return FileContent{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return FileContent{}, err
	}
	if info.IsDir() {
		return FileContent{}, errors.New("Spec Kit path is a directory")
	}
	if info.Size() > maxEditableSpecKitFileBytes {
		return FileContent{}, fmt.Errorf("Spec Kit file is too large to edit in Gemba (%d bytes)", info.Size())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return FileContent{}, err
	}
	return FileContent{
		Path:     rel,
		Content:  string(content),
		Modified: info.ModTime().UTC().Format(time.RFC3339),
		Size:     info.Size(),
	}, nil
}

func (s *Scanner) WriteWorkspaceFile(ctx context.Context, rel, content string) (FileContent, error) {
	if err := ctx.Err(); err != nil {
		return FileContent{}, err
	}
	if len(content) > maxEditableSpecKitFileBytes {
		return FileContent{}, fmt.Errorf("Spec Kit file is too large to write in Gemba (%d bytes)", len(content))
	}
	path, rel, err := s.resolveEditablePath(rel)
	if err != nil {
		return FileContent{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return FileContent{}, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return FileContent{}, err
	}
	return s.ReadWorkspaceFile(ctx, rel)
}

func (s *Scanner) EnsureScaffold(ctx context.Context) (Workspace, error) {
	if err := ctx.Err(); err != nil {
		return Workspace{}, err
	}
	root, err := s.absRoot()
	if err != nil {
		return Workspace{}, err
	}
	created, err := ensureScaffold(root)
	if err != nil {
		return Workspace{}, err
	}
	workspace, err := s.Workspace(ctx)
	if err != nil {
		return Workspace{}, err
	}
	workspace.InitializedPaths = created
	return workspace, nil
}

func (s *Scanner) CreateFeature(ctx context.Context, opts NewFeatureOptions) (Feature, error) {
	if err := ctx.Err(); err != nil {
		return Feature{}, err
	}
	root, err := s.absRoot()
	if err != nil {
		return Feature{}, err
	}
	if _, err := ensureScaffold(root); err != nil {
		return Feature{}, err
	}
	id := sanitizeFeatureID(opts.ID)
	if id == "" {
		id = nextFeatureID(root, opts.Title)
	}
	if id == "" {
		return Feature{}, errors.New("Spec Kit feature id is required")
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = titleFromID(id)
	}
	dir := filepath.Join(root, "specs", id)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return Feature{}, fmt.Errorf("Spec Kit feature %q already exists", id)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Feature{}, err
	}
	files := map[string]string{
		"spec.md":  featureSpecTemplate(title),
		"plan.md":  featurePlanTemplate(title),
		"tasks.md": featureTasksTemplate(),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return Feature{}, err
		}
	}
	return s.Load(ctx, id)
}

func (s *Scanner) resolveEditablePath(rel string) (string, string, error) {
	root, err := s.absRoot()
	if err != nil {
		return "", "", err
	}
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\x00") {
		return "", "", errors.New("Spec Kit file path is required")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	rel = filepath.ToSlash(clean)
	if strings.HasPrefix(rel, "../") || rel == ".." {
		return "", "", errors.New("Spec Kit file path must stay inside the project")
	}
	if !strings.HasPrefix(rel, "specs/") && !strings.HasPrefix(rel, ".specify/") {
		return "", "", errors.New("Spec Kit file path must be under specs/ or .specify/")
	}
	if !isEditableSpecKitFile(filepath.Base(rel)) {
		return "", "", errors.New("Spec Kit editor only supports text planning files")
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	rootWithSep := root + string(filepath.Separator)
	if abs != root && !strings.HasPrefix(abs, rootWithSep) {
		return "", "", errors.New("Spec Kit file path must stay inside the project")
	}
	return abs, rel, nil
}

func (s *Scanner) absRoot() (string, error) {
	if strings.TrimSpace(s.root) == "" {
		s.root = "."
	}
	abs, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	s.root = abs
	return abs, nil
}

func ensureScaffold(root string) ([]string, error) {
	files := map[string]string{
		".specify/memory/constitution.md":           constitutionTemplate(),
		".specify/templates/spec-template.md":       specTemplate(),
		".specify/templates/plan-template.md":       planTemplate(),
		".specify/templates/tasks-template.md":      tasksTemplate(),
		".specify/templates/agent-file-template.md": "# Agent Notes\n\nUse this file for project-specific agent guidance.\n",
	}
	var created []string
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, err
		}
		created = append(created, rel)
	}
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		return nil, err
	}
	sort.Strings(created)
	return created, nil
}

func scaffoldPresent(root string) bool {
	if info, err := os.Stat(filepath.Join(root, ".specify")); err == nil && info.IsDir() {
		return true
	}
	return false
}

func isEditableSpecKitFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".txt", ".yml", ".yaml", ".toml", ".json":
		return true
	default:
		return false
	}
}

func fileRole(rel string) string {
	base := filepath.Base(rel)
	switch base {
	case "spec.md", "spec-template.md":
		return "spec"
	case "plan.md", "plan-template.md":
		return "plan"
	case "tasks.md", "tasks-template.md":
		return "tasks"
	case "constitution.md", "constitution-template.md":
		return "constitution"
	default:
		if strings.HasPrefix(rel, ".specify/templates/") {
			return "template"
		}
		return "support"
	}
}

func featureIDForPath(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) >= 3 && parts[0] == "specs" {
		return parts[1]
	}
	if len(parts) >= 4 && parts[0] == ".specify" && parts[1] == "specs" {
		return parts[2]
	}
	return ""
}

func fileSortKey(file WorkspaceFile) string {
	group := "2"
	if file.FeatureID != "" {
		group = "0"
	} else if strings.HasPrefix(file.Path, ".specify/templates/") {
		group = "1"
	}
	return group + ":" + file.FeatureID + ":" + fileRoleRank(file.Role) + ":" + file.Path
}

func fileRoleRank(role string) string {
	switch role {
	case "spec":
		return "0"
	case "plan":
		return "1"
	case "tasks":
		return "2"
	default:
		return "9"
	}
}

var featureIDInvalidRe = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeFeatureID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "_", "-")
	raw = featureIDInvalidRe.ReplaceAllString(raw, "-")
	raw = strings.Trim(raw, "-")
	for strings.Contains(raw, "--") {
		raw = strings.ReplaceAll(raw, "--", "-")
	}
	return raw
}

func nextFeatureID(root, title string) string {
	slug := sanitizeFeatureID(title)
	if slug == "" {
		slug = "new-feature"
	}
	max := 0
	entries, _ := os.ReadDir(filepath.Join(root, "specs"))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		parts := strings.SplitN(entry.Name(), "-", 2)
		if len(parts) == 2 && allDigits(parts[0]) {
			var n int
			_, _ = fmt.Sscanf(parts[0], "%d", &n)
			if n > max {
				max = n
			}
		}
	}
	return fmt.Sprintf("%03d-%s", max+1, slug)
}

func constitutionTemplate() string {
	return "# Project Constitution\n\n## Principles\n\n- Capture planning decisions close to the work.\n- Keep generated implementation plans reviewable before execution.\n\n## Constraints\n\n- Draft Beads remain uncommitted until ratified.\n"
}

func specTemplate() string {
	return "# Feature Specification: [FEATURE NAME]\n\n## User Stories & Testing\n\n### User Story 1 - [Short title] (Priority: P1)\n\n[Describe the user goal and why it matters.]\n\n**Acceptance Scenarios**:\n\n1. **Given** [state], **When** [action], **Then** [observable result]\n\n## Requirements\n\n- **FR-001**: The system MUST [capability].\n"
}

func planTemplate() string {
	return "# Implementation Plan: [FEATURE NAME]\n\n## Summary\n\n[Technical approach]\n\n## Context\n\n- Project type: [web/service/library]\n- Primary dependencies: [list]\n\n## Risks\n\n- [risk]\n"
}

func tasksTemplate() string {
	return "# Tasks: [FEATURE NAME]\n\n## Phase 1: Setup\n\n- [ ] T001 Create project structure for the feature\n\n## Phase 2: User Story 1\n\n- [ ] T002 [US1] Implement the first user-facing behavior\n- [ ] T003 [P] [US1] Add focused tests for the behavior\n"
}

func featureSpecTemplate(title string) string {
	return strings.ReplaceAll(specTemplate(), "[FEATURE NAME]", title)
}

func featurePlanTemplate(title string) string {
	return strings.ReplaceAll(planTemplate(), "[FEATURE NAME]", title)
}

func featureTasksTemplate() string {
	return tasksTemplate()
}
