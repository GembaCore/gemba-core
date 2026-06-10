package speckit

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Scanner struct {
	root string
}

func NewScanner(root string) *Scanner {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	return &Scanner{root: root}
}

func (s *Scanner) List(ctx context.Context) (ListResult, error) {
	roots, err := s.specRoots()
	if err != nil {
		return ListResult{}, err
	}
	if len(roots) == 0 {
		return ListResult{Configured: false, Features: []Feature{}}, nil
	}
	seen := map[string]bool{}
	var features []Feature
	for _, specRoot := range roots {
		if err := ctx.Err(); err != nil {
			return ListResult{}, err
		}
		entries, err := os.ReadDir(specRoot)
		if err != nil {
			return ListResult{}, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			dir := filepath.Join(specRoot, entry.Name())
			rel, _ := filepath.Rel(s.root, dir)
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				continue
			}
			seen[rel] = true
			f, err := s.Load(ctx, entry.Name())
			if err != nil {
				return ListResult{}, err
			}
			if f.Directory == "" {
				f.Directory = rel
			}
			features = append(features, f)
		}
	}
	sort.Slice(features, func(i, j int) bool { return features[i].ID < features[j].ID })
	return ListResult{Configured: true, Features: features, Total: len(features)}, nil
}

func (s *Scanner) Load(ctx context.Context, id string) (Feature, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Feature{}, errors.New("spec kit feature id is required")
	}
	roots, err := s.specRoots()
	if err != nil {
		return Feature{}, err
	}
	for _, specRoot := range roots {
		if err := ctx.Err(); err != nil {
			return Feature{}, err
		}
		dir := filepath.Join(specRoot, id)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		return s.loadFeature(dir, id)
	}
	return Feature{}, os.ErrNotExist
}

func (s *Scanner) specRoots() ([]string, error) {
	abs, err := filepath.Abs(s.root)
	if err != nil {
		return nil, err
	}
	candidates := []string{
		filepath.Join(abs, "specs"),
		filepath.Join(abs, ".specify", "specs"),
	}
	var roots []string
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			roots = append(roots, candidate)
		}
	}
	s.root = abs
	return roots, nil
}

func (s *Scanner) loadFeature(dir, id string) (Feature, error) {
	relDir, _ := filepath.Rel(s.root, dir)
	f := Feature{
		ID:        id,
		Title:     titleFromID(id),
		Directory: filepath.ToSlash(relDir),
	}
	spec := filepath.Join(dir, "spec.md")
	plan := filepath.Join(dir, "plan.md")
	tasks := filepath.Join(dir, "tasks.md")
	if existsFile(spec) {
		f.HasSpec = true
		f.SpecPath = filepath.ToSlash(pathRel(s.root, spec))
		f.Spec = parseSpecFile(spec)
		if f.Spec.Title != "" {
			f.Title = f.Spec.Title
		}
	}
	if existsFile(plan) {
		f.HasPlan = true
		f.PlanPath = filepath.ToSlash(pathRel(s.root, plan))
	}
	if existsFile(tasks) {
		f.HasTasks = true
		f.TasksPath = filepath.ToSlash(pathRel(s.root, tasks))
		f.Tasks = parseTasksFile(tasks)
		f.TaskCount = len(f.Tasks)
		for _, task := range f.Tasks {
			if task.Parallel {
				f.ParallelTaskCount++
			}
		}
	}
	return f, nil
}

var (
	userStoryHeadingRe = regexp.MustCompile(`(?i)^#{2,4}\s+User Story\s+([0-9]+)\s*[-:]?\s*(.*)$`)
	priorityRe         = regexp.MustCompile(`(?i)\((?:Priority|P):\s*([^)]+)\)`)
	taskLineRe         = regexp.MustCompile(`^\s*[-*]\s+(?:\[([ xX])\]\s+)?\[?(T[0-9]+)\]?\s*(?:\[([Pp])\]\s*)?(?:\[(US[0-9]+)\]\s*)?(.*)$`)
)

func parseSpecFile(path string) SpecSummary {
	f, err := os.Open(path)
	if err != nil {
		return SpecSummary{}
	}
	defer f.Close()
	var out SpecSummary
	var current *UserStory
	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if strings.HasPrefix(line, "# Feature Specification:") {
				out.Title = strings.TrimSpace(strings.TrimPrefix(line, "# Feature Specification:"))
			} else if out.Title == "" && strings.HasPrefix(line, "# ") {
				out.Title = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			}
			if m := userStoryHeadingRe.FindStringSubmatch(line); len(m) > 0 {
				title := strings.TrimSpace(priorityRe.ReplaceAllString(m[2], ""))
				us := UserStory{
					ID:       "US" + m[1],
					Title:    strings.Trim(title, " -:"),
					Priority: priorityFrom(line),
				}
				out.UserStories = append(out.UserStories, us)
				current = &out.UserStories[len(out.UserStories)-1]
				section = ""
				continue
			}
			lower := strings.ToLower(line)
			switch {
			case strings.Contains(lower, "acceptance"):
				section = "acceptance"
			case strings.Contains(lower, "requirement"):
				section = "requirements"
			default:
				section = ""
			}
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "acceptance") && strings.Contains(lower, "scenario") {
			section = "acceptance"
			continue
		}
		if strings.Contains(lower, "functional requirement") || strings.Contains(lower, "requirements") {
			section = "requirements"
			continue
		}
		if section == "acceptance" && isListLine(line) {
			v := cleanMarkdownList(line)
			out.AcceptanceScenarios = append(out.AcceptanceScenarios, v)
			if current != nil {
				current.AcceptanceScenarios = append(current.AcceptanceScenarios, v)
			}
		}
		if section == "requirements" && isListLine(line) {
			out.FunctionalRequirements = append(out.FunctionalRequirements, cleanMarkdownList(line))
		}
	}
	return out
}

func parseTasksFile(path string) []Task {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var tasks []Task
	phase := ""
	lineNo := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "##") {
			phase = strings.Trim(strings.TrimLeft(trimmed, "#"), " ")
			continue
		}
		m := taskLineRe.FindStringSubmatch(line)
		if len(m) == 0 {
			continue
		}
		title := strings.TrimSpace(m[5])
		tasks = append(tasks, Task{
			ID:          strings.ToUpper(m[2]),
			Title:       title,
			Phase:       phase,
			StoryID:     strings.ToUpper(m[4]),
			Parallel:    strings.EqualFold(m[3], "P"),
			Done:        strings.EqualFold(m[1], "x"),
			Line:        lineNo,
			Description: title,
		})
	}
	return tasks
}

func priorityFrom(line string) string {
	m := priorityRe.FindStringSubmatch(line)
	if len(m) == 0 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func titleFromID(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) > 0 && allDigits(parts[0]) {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return id
	}
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func existsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pathRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func isListLine(line string) bool {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return true
	}
	dot := strings.Index(line, ".")
	return dot > 0 && allDigits(line[:dot])
}

func cleanMarkdownList(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "-")
	line = strings.TrimPrefix(line, "*")
	if dot := strings.Index(line, "."); dot > 0 && allDigits(strings.TrimSpace(line[:dot])) {
		line = line[dot+1:]
	}
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "`")
	return line
}
