// check-decisions enforces the bidirectional linkage between
// docs/design/*.md files and decision-type beads (D6 / gm-d1m1).
//
// Usage:
//
//	check-decisions [--design DIR] [--bd PATH] [--format text|json]
//
// Exit code is non-zero when any error-class violation is found.
// Warnings (e.g. doc points at a non-decision bead) print but don't
// fail the build.
//
// Three checks:
//
//  1. Doc → bead. Every docs/design/*.md has a `decision:` frontmatter
//     field set to either a bead id or "none". When non-none, the
//     bead must resolve and be type=decision.
//  2. Bead → doc. Every bead with type=decision has a `Doc:` line in
//     the description set to a docs/design/<file>.md path or "none".
//     When non-none, the doc must exist and its frontmatter
//     `decision:` must round-trip to this bead's id.
//  3. Number stability. A decision with label `d:#` must have a
//     matching `D#:` prefix in its title.
//
// The linter shells out to `bd query --json` so it doesn't need to
// import the bd adapter. That keeps cmd/check-decisions stateless and
// safe to run in CI without a DB connection.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type decisionBead struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
	Status      string   `json:"status"`
	IssueType   string   `json:"issue_type"`
}

type docFrontmatter struct {
	Path     string
	Title    string
	Decision string // bead id or "none"
	D        string // human handle "D6" when present
	Backfill string // "pending" when present
}

type finding struct {
	level   string // "error" | "warning"
	subject string
	message string
}

func (f finding) String() string {
	return fmt.Sprintf("%s\t%-40s\t%s", strings.ToUpper(f.level), f.subject, f.message)
}

var (
	frontmatterStart = regexp.MustCompile(`^---\s*$`)
	docLineRE        = regexp.MustCompile(`(?im)^\s*Doc:\s*(.+?)\s*$`)
	dPrefixRE        = regexp.MustCompile(`^D(\d+):`)
	dLabelRE         = regexp.MustCompile(`^d:(\d+)$`)
)

func main() {
	designDir := flag.String("design", "docs/design", "directory containing design markdown files")
	bdPath := flag.String("bd", "bd", "path to the bd CLI binary")
	format := flag.String("format", "text", "output format: text | json")
	docsOnly := flag.Bool("docs-only", false,
		"validate docs/design frontmatter only; skip the bead-side checks. "+
			"Use in CI environments without bd / Dolt access.")
	flag.Parse()

	docs, err := loadDocs(*designDir)
	if err != nil {
		fail("load docs: %v", err)
	}

	var beads []decisionBead
	if !*docsOnly {
		beads, err = loadDecisionBeads(*bdPath)
		if err != nil {
			fail("load decision beads (is `%s` on PATH?): %v", *bdPath, err)
		}
	}

	findings := []finding{}
	if *docsOnly {
		findings = append(findings, checkDocsFrontmatterShape(docs)...)
	} else {
		findings = append(findings, checkDocsToBeads(docs, beads)...)
		findings = append(findings, checkBeadsToDocs(beads, docs, *designDir)...)
		findings = append(findings, checkNumberStability(beads)...)
	}

	report(findings, *format, len(docs), len(beads))

	for _, f := range findings {
		if f.level == "error" {
			os.Exit(1)
		}
	}
}

func loadDocs(dir string) ([]docFrontmatter, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []docFrontmatter{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		fm, err := parseFrontmatter(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, fm)
	}
	return out, nil
}

// parseFrontmatter does a deliberately small, hand-rolled YAML parse.
// Frontmatter we care about is flat key:value, sometimes with quoted
// titles. Pulling in a real YAML lib for this would be over-scoped.
func parseFrontmatter(path string) (docFrontmatter, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return docFrontmatter{}, err
	}
	fm := docFrontmatter{Path: path}
	lines := strings.Split(string(body), "\n")
	if len(lines) == 0 || !frontmatterStart.MatchString(lines[0]) {
		// No frontmatter at all — the doc-to-bead check will flag this.
		return fm, nil
	}
	for _, line := range lines[1:] {
		if frontmatterStart.MatchString(line) {
			break
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip wrapping double-quotes on titles.
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		switch key {
		case "title":
			fm.Title = val
		case "decision":
			fm.Decision = val
		case "d":
			fm.D = val
		case "backfill":
			fm.Backfill = val
		}
	}
	return fm, nil
}

func loadDecisionBeads(bdPath string) ([]decisionBead, error) {
	cmd := exec.Command(bdPath, "query", "label=decision", "-a", "--limit", "0", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bd query: %w", err)
	}
	var beads []decisionBead
	if len(out) == 0 || string(out) == "null\n" {
		return beads, nil
	}
	if err := json.Unmarshal(out, &beads); err != nil {
		return nil, fmt.Errorf("decode bd query json: %w", err)
	}
	// Filter to type=decision in case the label query catches noise.
	clean := beads[:0]
	for _, b := range beads {
		if b.IssueType == "decision" {
			clean = append(clean, b)
		}
	}
	sort.Slice(clean, func(i, j int) bool { return clean[i].ID < clean[j].ID })
	return clean, nil
}

// checkDocsFrontmatterShape is the CI-friendly subset that runs
// without bd. Validates that every doc has a 'decision:' field with
// a syntactically plausible value (a bead-id-shape, a docsite "none",
// or no field at all → error). Cross-validation against the live bead
// store is the operator's job (make lint-decisions, no --docs-only).
func checkDocsFrontmatterShape(docs []docFrontmatter) []finding {
	beadIDRE := regexp.MustCompile(`^(?:[a-z0-9_]+/)*[a-z]{2,}-[a-z0-9.]+$`)
	var out []finding
	for _, d := range docs {
		if d.Decision == "" {
			out = append(out, finding{
				level: "error", subject: d.Path,
				message: "missing 'decision:' frontmatter (set to a bead id or 'none')",
			})
			continue
		}
		if d.Decision == "none" {
			continue
		}
		if !beadIDRE.MatchString(d.Decision) {
			out = append(out, finding{
				level: "error", subject: d.Path,
				message: fmt.Sprintf("decision %q doesn't look like a bead id (expected e.g. 'gm-d1m1' or 'workspace/repo/gm-d1m1' or 'none')", d.Decision),
			})
		}
	}
	return out
}

func checkDocsToBeads(docs []docFrontmatter, beads []decisionBead) []finding {
	beadByID := map[string]decisionBead{}
	for _, b := range beads {
		beadByID[shortID(b.ID)] = b
	}
	var out []finding
	for _, d := range docs {
		if d.Decision == "" {
			out = append(out, finding{
				level: "error", subject: d.Path,
				message: "missing 'decision:' frontmatter (set to a bead id or 'none')",
			})
			continue
		}
		if d.Decision == "none" {
			continue // intentional opt-out
		}
		bead, ok := beadByID[shortID(d.Decision)]
		if !ok {
			out = append(out, finding{
				level: "error", subject: d.Path,
				message: fmt.Sprintf("decision %q does not resolve to a known decision-type bead", d.Decision),
			})
			continue
		}
		if bead.IssueType != "decision" {
			out = append(out, finding{
				level: "warning", subject: d.Path,
				message: fmt.Sprintf("decision %q points at a non-decision bead (type=%s)", d.Decision, bead.IssueType),
			})
		}
	}
	return out
}

func checkBeadsToDocs(beads []decisionBead, docs []docFrontmatter, designDir string) []finding {
	docByPath := map[string]docFrontmatter{}
	for _, d := range docs {
		docByPath[filepath.Base(d.Path)] = d
	}
	var out []finding
	for _, b := range beads {
		match := docLineRE.FindStringSubmatch(b.Description)
		if match == nil {
			out = append(out, finding{
				level: "error", subject: b.ID,
				message: "decision description has no 'Doc:' line (set to a docs/design/<file>.md path or 'none')",
			})
			continue
		}
		val := strings.TrimSpace(match[1])
		if val == "none" {
			continue
		}
		// Accept both "docs/design/foo.md" and bare "foo.md".
		base := filepath.Base(val)
		doc, ok := docByPath[base]
		if !ok {
			out = append(out, finding{
				level: "error", subject: b.ID,
				message: fmt.Sprintf("Doc: %q does not resolve to a file under %s/", val, designDir),
			})
			continue
		}
		if shortID(doc.Decision) != shortID(b.ID) {
			out = append(out, finding{
				level: "error", subject: b.ID,
				message: fmt.Sprintf("Doc: %q frontmatter points at %q, not this bead's id", val, doc.Decision),
			})
		}
	}
	return out
}

func checkNumberStability(beads []decisionBead) []finding {
	var out []finding
	for _, b := range beads {
		var labelNum string
		for _, l := range b.Labels {
			if m := dLabelRE.FindStringSubmatch(l); m != nil {
				labelNum = m[1]
				break
			}
		}
		titleMatch := dPrefixRE.FindStringSubmatch(b.Title)
		titleNum := ""
		if titleMatch != nil {
			titleNum = titleMatch[1]
		}
		if labelNum == "" && titleNum == "" {
			out = append(out, finding{
				level: "error", subject: b.ID,
				message: "missing both 'd:#' label and 'D#:' title prefix",
			})
			continue
		}
		if labelNum != "" && titleNum == "" {
			out = append(out, finding{
				level: "error", subject: b.ID,
				message: fmt.Sprintf("has label d:%s but title doesn't carry 'D%s:' prefix", labelNum, labelNum),
			})
			continue
		}
		if labelNum == "" && titleNum != "" {
			out = append(out, finding{
				level: "error", subject: b.ID,
				message: fmt.Sprintf("title has 'D%s:' but no matching 'd:%s' label", titleNum, titleNum),
			})
			continue
		}
		if labelNum != titleNum {
			out = append(out, finding{
				level: "error", subject: b.ID,
				message: fmt.Sprintf("number drift: title says D%s, label says d:%s", titleNum, labelNum),
			})
		}
	}
	return out
}

// shortID reduces a workspace-prefixed bead id like
// "gemba/gemba/gm-d1m1" to "gm-d1m1" so doc-frontmatter ids and
// bd-query-emitted ids compare cleanly.
func shortID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func report(findings []finding, format string, ndocs, nbeads int) {
	errs, warns := 0, 0
	for _, f := range findings {
		if f.level == "error" {
			errs++
		} else {
			warns++
		}
	}
	if format == "json" {
		out := map[string]any{
			"docs":     ndocs,
			"beads":    nbeads,
			"errors":   errs,
			"warnings": warns,
			"findings": findings,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	fmt.Printf("checking %d docs/design/*.md files...\n", ndocs)
	fmt.Printf("checking %d decision beads...\n\n", nbeads)
	if len(findings) == 0 {
		fmt.Printf("OK: %d docs, %d decisions\n", ndocs, nbeads)
		return
	}
	if errs > 0 {
		fmt.Printf("ERRORS (%d):\n", errs)
		for _, f := range findings {
			if f.level == "error" {
				fmt.Println("  " + f.String())
			}
		}
		fmt.Println()
	}
	if warns > 0 {
		fmt.Printf("WARNINGS (%d):\n", warns)
		for _, f := range findings {
			if f.level == "warning" {
				fmt.Println("  " + f.String())
			}
		}
		fmt.Println()
	}
	fmt.Printf("OK: %d docs, %d decisions, %d errors, %d warnings\n",
		ndocs, nbeads, errs, warns)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "check-decisions: "+format+"\n", args...)
	os.Exit(2)
}

// findings JSON-encode as `{level, subject, message}` for tooling.
func (f finding) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Level   string `json:"level"`
		Subject string `json:"subject"`
		Message string `json:"message"`
	}{f.level, f.subject, f.message})
}
