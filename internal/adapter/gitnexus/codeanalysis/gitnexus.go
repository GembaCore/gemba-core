// GitNexus reference backend for codeanalysis.Provider (gm-56z).

package codeanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	corecodeanalysis "github.com/GembaCore/gemba-core/internal/core/codeanalysis"
)

// adapterVersion identifies this adapter's wire-shape revision.
// Bumped when the CLI subcommands the adapter relies on change in
// a backwards-incompatible way. The string is surfaced verbatim by
// [Adapter.Manifest] for operator diagnostics.
const adapterVersion = "gm-56z.1"

// defaultStaleAfter is the freshness window the adapter advertises
// for [HealthReport]. Matches the design's "analysis from 12h ago;
// reindex recommended" example. Can be tuned via Adapter.StaleAfter.
const defaultStaleAfter = 12 * time.Hour

// Adapter is the live GitNexus-backed implementation of
// codeanalysis.Provider. Construct via [New] for the production
// shape; tests construct the struct directly and inject the exec
// hook to stub the CLI.
//
// Concurrency: safe for concurrent reads. The exec hook is
// install-and-forget; the rest is stateless beyond struct fields.
type Adapter struct {
	// BinPath overrides the gitnexus binary location. Empty means
	// "look up `gitnexus` on PATH".
	BinPath string

	// WorkspaceRoot is the directory the gitnexus CLI runs from.
	// Used to locate per-repo .gitnexus/meta.json files when
	// computing freshness for [Health]. Empty falls back to
	// process cwd at call time.
	WorkspaceRoot string

	// StaleAfter overrides the default freshness window stamped
	// into [HealthReport.StaleAfter]. Zero falls back to
	// [defaultStaleAfter].
	StaleAfter time.Duration

	// exec is the subprocess hook tests stub out. Production calls
	// fall through to os/exec.CommandContext when nil.
	exec func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Compile-time interface assertion.
var _ corecodeanalysis.Provider = (*Adapter)(nil)

// New returns an [Adapter] with default settings. Pass-through
// overrides via the returned struct.
func New() *Adapter {
	return &Adapter{}
}

// Factory builds a fresh Adapter. Used as the codeanalysis.Factory
// the package init installs into the registry.
func Factory() (corecodeanalysis.Provider, error) {
	return New(), nil
}

func (a *Adapter) bin() string {
	if a.BinPath != "" {
		return a.BinPath
	}
	return "gitnexus"
}

func (a *Adapter) staleAfter() time.Duration {
	if a.StaleAfter > 0 {
		return a.StaleAfter
	}
	return defaultStaleAfter
}

// run invokes the gitnexus binary, returning combined output. The
// exec hook short-circuits to a stubbed implementation in tests.
func (a *Adapter) run(ctx context.Context, args ...string) ([]byte, error) {
	if a.exec != nil {
		return a.exec(ctx, a.bin(), args...)
	}
	return exec.CommandContext(ctx, a.bin(), args...).Output()
}

// isExecMissing reports whether err is a "binary not on PATH"
// failure — the cue [Manifest]/[ListRepos] use to surface a
// graceful degraded reading rather than a hard error.
func isExecMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var pathErr *exec.Error
	return errors.As(err, &pathErr)
}

// listEntry mirrors one row of `gitnexus list --json` output:
// the CLI prints an array of objects with name + path + remote.
type listEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Remote string `json:"remote,omitempty"`
}

// listRepos shells out to `gitnexus list --json`. Returns
// (nil, nil) when the binary is missing — callers project that
// into an empty repo list rather than an error.
func (a *Adapter) listRepos(ctx context.Context) ([]listEntry, error) {
	out, err := a.run(ctx, "list", "--json")
	if err != nil {
		if isExecMissing(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("gitnexus list: %w", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" || s == "[]" {
		return nil, nil
	}
	var entries []listEntry
	if err := json.Unmarshal([]byte(s), &entries); err != nil {
		return nil, fmt.Errorf("gitnexus list: parse: %w", err)
	}
	return entries, nil
}

// Manifest returns the adapter's self-description. Capability
// flags reflect what the CLI surface actually supports today.
func (a *Adapter) Manifest(ctx context.Context) (corecodeanalysis.Manifest, error) {
	repos, err := a.listRepos(ctx)
	if err != nil {
		return corecodeanalysis.Manifest{}, err
	}
	indexed := make([]corecodeanalysis.RepoRef, 0, len(repos))
	for _, e := range repos {
		indexed = append(indexed, corecodeanalysis.RepoRef{
			Name:   e.Name,
			Path:   e.Path,
			Remote: e.Remote,
		})
	}
	return corecodeanalysis.Manifest{
		Backend:               corecodeanalysis.BackendGitNexus,
		Version:               adapterVersion,
		IndexedRepos:          indexed,
		SupportsSymbolContext: true,
		SupportsImpact:        true,
		// RouteMap + Embeddings stay false until the planner has a
		// caller — the CLI can do them, but the wider system
		// gates on the flag and "supported but unused" inflates
		// the adapter's perceived surface.
		SupportsRouteMap:   false,
		SupportsEmbeddings: false,
	}, nil
}

// ListRepos returns the configured repos this provider will
// answer queries for, sourced from `gitnexus list --json`.
func (a *Adapter) ListRepos(ctx context.Context) ([]corecodeanalysis.RepoRef, error) {
	repos, err := a.listRepos(ctx)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, nil
	}
	out := make([]corecodeanalysis.RepoRef, 0, len(repos))
	for _, e := range repos {
		out = append(out, corecodeanalysis.RepoRef{
			Name:   e.Name,
			Path:   e.Path,
			Remote: e.Remote,
		})
	}
	return out, nil
}

// Reindex shells out to `gitnexus analyze` with the appropriate
// flags. DryRun returns nil without invoking the CLI — the design
// promises no side effects in dry-run mode and the CLI lacks a
// dry-run subcommand of its own.
func (a *Adapter) Reindex(ctx context.Context, repo corecodeanalysis.RepoRef, flags corecodeanalysis.ReindexFlags) error {
	if repo.Name == "" && repo.Path == "" {
		return errors.New("gitnexus reindex: repo requires name or path")
	}
	if flags.DryRun {
		return nil
	}
	args := []string{"analyze"}
	if repo.Name != "" {
		args = append(args, "-r", repo.Name)
	}
	if repo.Path != "" {
		args = append(args, "--path", repo.Path)
	}
	if flags.Full {
		args = append(args, "--full")
	}
	if flags.WithEmbeddings {
		args = append(args, "--embeddings")
	}
	if _, err := a.run(ctx, args...); err != nil {
		if isExecMissing(err) {
			return fmt.Errorf("gitnexus reindex: %w", err)
		}
		return fmt.Errorf("gitnexus analyze: %w", err)
	}
	return nil
}

// cypherEnvelope mirrors the JSON shape `gitnexus cypher` emits
// for a successful query. Identical to the one in
// internal/sourceanalysis/gitnexus.go but kept private to this
// package so the two adapters stay independent.
type cypherEnvelope struct {
	Markdown string `json:"markdown"`
	RowCount int    `json:"row_count"`
}

// runCypher executes a Cypher query against the named repo and
// returns parsed (headers, rows). Empty result is (nil, nil, nil).
func (a *Adapter) runCypher(ctx context.Context, repoName, query string) ([]string, [][]string, error) {
	args := []string{"cypher"}
	if repoName != "" {
		args = append(args, "-r", repoName)
	}
	args = append(args, query)
	out, err := a.run(ctx, args...)
	if err != nil {
		if isExecMissing(err) {
			return nil, nil, fmt.Errorf("gitnexus cypher: %w", err)
		}
		return nil, nil, fmt.Errorf("gitnexus cypher: %w", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" || s == "[]" {
		return nil, nil, nil
	}
	var env cypherEnvelope
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		return nil, nil, fmt.Errorf("gitnexus cypher: parse envelope: %w", err)
	}
	headers, rows := parseMarkdownTable(env.Markdown)
	return headers, rows, nil
}

// parseMarkdownTable splits a pipe-rendered table into headers and
// rows. Mirrors the parser in internal/sourceanalysis/gitnexus.go.
func parseMarkdownTable(md string) ([]string, [][]string) {
	lines := strings.Split(strings.TrimSpace(md), "\n")
	if len(lines) == 0 {
		return nil, nil
	}
	parseRow := func(line string) []string {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "|")
		line = strings.TrimSuffix(line, "|")
		parts := strings.Split(line, "|")
		out := make([]string, len(parts))
		for i, p := range parts {
			out[i] = strings.TrimSpace(p)
		}
		return out
	}
	headers := parseRow(lines[0])
	if len(lines) < 3 {
		return headers, nil
	}
	rows := make([][]string, 0, len(lines)-2)
	for _, line := range lines[2:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, parseRow(line))
	}
	return headers, rows
}

func indexOf(headers []string, name string) int {
	for i, h := range headers {
		if h == name {
			return i
		}
	}
	return -1
}

// Modules returns the module inventory for repo. Maps
// `gitnexus`'s Cluster/Module nodes onto the typed [Module]
// projection. Missing optional columns degrade to zero values
// per the design.
func (a *Adapter) Modules(ctx context.Context, repo corecodeanalysis.RepoRef) ([]corecodeanalysis.Module, error) {
	if repo.Name == "" {
		return nil, errors.New("gitnexus modules: repo.Name required")
	}
	q := `MATCH (m:Module) RETURN m.name, m.path, m.summary, m.loc, m.fileCount`
	headers, rows, err := a.runCypher(ctx, repo.Name, q)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	nameIdx := indexOf(headers, "m.name")
	pathIdx := indexOf(headers, "m.path")
	summaryIdx := indexOf(headers, "m.summary")
	locIdx := indexOf(headers, "m.loc")
	fileCountIdx := indexOf(headers, "m.fileCount")
	if nameIdx < 0 {
		return nil, nil
	}
	out := make([]corecodeanalysis.Module, 0, len(rows))
	for _, row := range rows {
		if nameIdx >= len(row) {
			continue
		}
		name := row[nameIdx]
		if name == "" {
			continue
		}
		mod := corecodeanalysis.Module{
			Name:         name,
			TestCoverage: -1, // contract: -1 = unknown
		}
		if pathIdx >= 0 && pathIdx < len(row) {
			mod.Path = row[pathIdx]
		}
		if summaryIdx >= 0 && summaryIdx < len(row) {
			mod.Summary = row[summaryIdx]
		}
		if locIdx >= 0 && locIdx < len(row) {
			mod.LOC = atoiSafe(row[locIdx])
		}
		if fileCountIdx >= 0 && fileCountIdx < len(row) {
			mod.FileCount = atoiSafe(row[fileCountIdx])
		}
		out = append(out, mod)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// metaFile mirrors the relevant subset of .gitnexus/meta.json.
type metaFile struct {
	IndexedAt time.Time `json:"indexedAt"`
}

// Health returns a health report for repo. Combines a trio of
// Cypher queries (cycles / undocumented / god-classes) with a
// freshness read from .gitnexus/meta.json.
func (a *Adapter) Health(ctx context.Context, repo corecodeanalysis.RepoRef) (corecodeanalysis.HealthReport, error) {
	if repo.Name == "" {
		return corecodeanalysis.HealthReport{}, errors.New("gitnexus health: repo.Name required")
	}
	report := corecodeanalysis.HealthReport{
		Repo:       repo,
		FetchedAt:  time.Now().UTC(),
		StaleAfter: a.staleAfter(),
	}

	// Freshness: read .gitnexus/meta.json from repo.Path (or
	// WorkspaceRoot/<name> as a fallback). A missing file is not
	// fatal — we just emit a Notes entry.
	if metaIndexed, ok := a.readMeta(repo); ok {
		report.FetchedAt = metaIndexed
		if time.Since(metaIndexed) > a.staleAfter() {
			report.Notes = append(report.Notes,
				fmt.Sprintf("index last refreshed %s ago; reindex recommended",
					time.Since(metaIndexed).Truncate(time.Minute)))
		}
	}

	// Cycle count.
	if headers, rows, err := a.runCypher(ctx, repo.Name,
		`MATCH p=(a:Module)-[:DEPENDS_ON*]->(a) RETURN count(DISTINCT p) AS cycles`); err == nil {
		idx := indexOf(headers, "cycles")
		if idx >= 0 && len(rows) > 0 && idx < len(rows[0]) {
			report.CycleCount = atoiSafe(rows[0][idx])
		}
	} else {
		// Cypher errors degrade to a Notes entry rather than
		// failing the whole report — health is best-effort.
		report.Notes = append(report.Notes, "cycle scan failed: "+err.Error())
	}

	// Undocumented public APIs.
	if headers, rows, err := a.runCypher(ctx, repo.Name,
		`MATCH (n) WHERE n.isExported = true AND (n.docstring IS NULL OR n.docstring = '') RETURN n.name, n.filePath, n.line`); err == nil {
		report.UndocumentedAPIs = parseSymbolRows(headers, rows, "n.name", "n.filePath", "n.line")
	}

	// God-classes (high fan-in types).
	if headers, rows, err := a.runCypher(ctx, repo.Name,
		`MATCH (t:Type)<-[r]-() WITH t, count(r) AS fanIn WHERE fanIn > 20 RETURN t.name, t.filePath, t.line`); err == nil {
		report.GodClasses = parseSymbolRows(headers, rows, "t.name", "t.filePath", "t.line")
	}

	return report, nil
}

// readMeta opens `.gitnexus/meta.json` rooted at repo.Path (or, when
// that's empty, at WorkspaceRoot). Returns (zero, false) on any
// read/parse failure — health degrades to "freshness unknown".
func (a *Adapter) readMeta(repo corecodeanalysis.RepoRef) (time.Time, bool) {
	root := repo.Path
	if root == "" {
		root = a.WorkspaceRoot
	}
	if root == "" {
		return time.Time{}, false
	}
	f, err := os.Open(filepath.Join(root, ".gitnexus", "meta.json"))
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	var meta metaFile
	if err := json.NewDecoder(f).Decode(&meta); err != nil {
		return time.Time{}, false
	}
	if meta.IndexedAt.IsZero() {
		return time.Time{}, false
	}
	return meta.IndexedAt, true
}

// parseSymbolRows projects (name, file, line) tuples out of a
// Cypher result. Rows missing the name column or carrying an empty
// name are dropped.
func parseSymbolRows(headers []string, rows [][]string, nameCol, fileCol, lineCol string) []corecodeanalysis.SymbolRef {
	if len(rows) == 0 {
		return nil
	}
	nameIdx := indexOf(headers, nameCol)
	if nameIdx < 0 {
		return nil
	}
	fileIdx := indexOf(headers, fileCol)
	lineIdx := indexOf(headers, lineCol)
	out := make([]corecodeanalysis.SymbolRef, 0, len(rows))
	for _, row := range rows {
		if nameIdx >= len(row) {
			continue
		}
		name := row[nameIdx]
		if name == "" {
			continue
		}
		ref := corecodeanalysis.SymbolRef{Name: name}
		if fileIdx >= 0 && fileIdx < len(row) {
			ref.File = row[fileIdx]
		}
		if lineIdx >= 0 && lineIdx < len(row) {
			ref.Line = atoiSafe(row[lineIdx])
		}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Impact walks the dependency graph from target in the requested
// direction. Direction defaults to Upstream when empty/invalid —
// matches the design's "what would BREAK if I change this" framing.
func (a *Adapter) Impact(ctx context.Context, repo corecodeanalysis.RepoRef, target string, direction corecodeanalysis.ImpactDirection) (corecodeanalysis.ImpactReport, error) {
	if repo.Name == "" {
		return corecodeanalysis.ImpactReport{}, errors.New("gitnexus impact: repo.Name required")
	}
	if target == "" {
		return corecodeanalysis.ImpactReport{}, errors.New("gitnexus impact: target required")
	}
	if !direction.IsValid() {
		direction = corecodeanalysis.ImpactUpstream
	}
	report := corecodeanalysis.ImpactReport{
		Target:    target,
		Direction: direction,
		Depth:     1,
	}

	// Build the edge predicate based on direction.
	var match string
	switch direction {
	case corecodeanalysis.ImpactDownstream:
		match = `MATCH (t)-[]->(other) WHERE t.name = '%s' AND other.name IS NOT NULL AND other.name <> t.name RETURN DISTINCT other.name, other.filePath, other.line`
	case corecodeanalysis.ImpactBoth:
		match = `MATCH (t)-[]-(other) WHERE t.name = '%s' AND other.name IS NOT NULL AND other.name <> t.name RETURN DISTINCT other.name, other.filePath, other.line`
	default: // upstream
		match = `MATCH (other)-[]->(t) WHERE t.name = '%s' AND other.name IS NOT NULL AND other.name <> t.name RETURN DISTINCT other.name, other.filePath, other.line`
	}
	q := fmt.Sprintf(match, cypherEscape(target))
	headers, rows, err := a.runCypher(ctx, repo.Name, q)
	if err != nil {
		return corecodeanalysis.ImpactReport{}, err
	}
	report.Affected = parseSymbolRows(headers, rows, "other.name", "other.filePath", "other.line")
	report.Risk = classifyRisk(len(report.Affected))
	if len(report.Affected) > 0 {
		report.Notes = append(report.Notes,
			fmt.Sprintf("%d %s symbol(s) reachable at depth 1",
				len(report.Affected), direction))
	}
	return report, nil
}

// classifyRisk projects a hit count to the coarse risk bucket the
// design calls for. Tuned conservatively — even one upstream
// caller is "low" rather than "none" because the change still
// requires verification.
func classifyRisk(n int) corecodeanalysis.RiskLevel {
	switch {
	case n == 0:
		return corecodeanalysis.RiskLow
	case n <= 5:
		return corecodeanalysis.RiskLow
	case n <= 20:
		return corecodeanalysis.RiskMedium
	case n <= 50:
		return corecodeanalysis.RiskHigh
	default:
		return corecodeanalysis.RiskCritical
	}
}

// cypherEscape handles the only character that breaks a quoted
// Cypher string literal.
func cypherEscape(s string) string {
	return strings.ReplaceAll(s, "'", `\'`)
}

// atoiSafe parses s into an int, returning 0 on any failure.
// Intentionally permissive: missing-or-non-numeric backend cells
// degrade to "unknown" rather than poisoning the whole call.
func atoiSafe(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 0
	neg := false
	for i, r := range s {
		if i == 0 && r == '-' {
			neg = true
			continue
		}
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		return -n
	}
	return n
}
