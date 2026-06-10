package sourceanalysis

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
)

// GitNexus is the SourceAnalysis binding backed by the local gitnexus
// CLI (gm-s47n.3.3). It shells out to `gitnexus cypher` for the
// dependency / contract-change queries and reads the per-repo
// .gitnexus/meta.json directly for [Describe].
//
// The binding deliberately stays narrow:
//
//   - Dependents / Dependencies operate at file-path granularity
//     (the planner cares about which files import which); the
//     gitnexus index already encodes node→file relationships, so a
//     single Cypher query handles the lookup without per-symbol
//     iteration.
//   - PublicContractChanges over-reports per the interface contract:
//     every exported symbol declared in any file in the diff comes
//     back, even if the line-level diff didn't touch it. The
//     planner's downstream consumers prefer false positives to
//     false negatives.
//   - Describe never shells out — it parses meta.json on disk so the
//     planner can poll cheaply.
//
// Concurrency: safe for concurrent use. The exec hook is set once
// and never mutated; the rest is stateless beyond struct fields.
//
// When the gitnexus binary cannot be found on PATH (or the
// configured BinPath), every query returns [ErrUnavailable] and
// Describe surfaces the missing backend via Capabilities.Reason.
type GitNexus struct {
	// BinPath is the path to the gitnexus executable. Empty means
	// "look up `gitnexus` on PATH".
	BinPath string
	// RepoName is the logical name registered in the gitnexus index
	// registry (`gitnexus list`). Required when the operator has
	// indexed multiple repos — the CLI rejects ambiguous calls.
	RepoName string
	// RepoPath is the filesystem path to the repository checkout.
	// Used to locate .gitnexus/meta.json for [Describe].
	RepoPath string

	// exec is the subprocess hook tests stub out. Production calls
	// fall through to os/exec.CommandContext when nil.
	exec func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewGitNexus builds a binding pointed at one repo. RepoPath is the
// filesystem checkout (used by Describe); RepoName is the gitnexus
// registry name passed via -r to subcommands.
func NewGitNexus(repoName, repoPath string) *GitNexus {
	return &GitNexus{RepoName: repoName, RepoPath: repoPath}
}

// Compile-time interface check.
var _ SourceAnalysis = (*GitNexus)(nil)

func (g *GitNexus) bin() string {
	if g.BinPath != "" {
		return g.BinPath
	}
	return "gitnexus"
}

// run invokes the gitnexus binary, returning combined output. The
// exec hook short-circuits to a stubbed implementation in tests.
func (g *GitNexus) run(ctx context.Context, args ...string) ([]byte, error) {
	if g.exec != nil {
		return g.exec(ctx, g.bin(), args...)
	}
	return exec.CommandContext(ctx, g.bin(), args...).Output()
}

// cypherEnvelope mirrors the JSON the gitnexus CLI emits for a
// successful cypher call. The markdown field carries a pipe-table
// rendering of the rows; we parse it locally.
type cypherEnvelope struct {
	Markdown string `json:"markdown"`
	RowCount int    `json:"row_count"`
}

// metaFile mirrors the relevant subset of .gitnexus/meta.json.
type metaFile struct {
	RepoPath   string    `json:"repoPath"`
	LastCommit string    `json:"lastCommit"`
	IndexedAt  time.Time `json:"indexedAt"`
}

// runCypher executes a query and returns the parsed table — column
// headers + data rows. Empty result is (nil, nil, nil); a missing
// gitnexus binary is (nil, nil, ErrUnavailable).
func (g *GitNexus) runCypher(ctx context.Context, query string) ([]string, [][]string, error) {
	args := []string{"cypher"}
	if g.RepoName != "" {
		args = append(args, "-r", g.RepoName)
	}
	args = append(args, query)
	out, err := g.run(ctx, args...)
	if err != nil {
		if isExecMissing(err) {
			return nil, nil, ErrUnavailable
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
// rows. The middle "| --- | --- |" divider row is skipped. Cells are
// trimmed; the empty string is returned as-is for missing values.
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

func isExecMissing(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var pathErr *exec.Error
	return errors.As(err, &pathErr)
}

// Dependents returns the set of files containing a node with an
// edge into ANY node declared in target.Path. The result is
// deduplicated by file path; the target file itself is excluded.
func (g *GitNexus) Dependents(ctx context.Context, target Target) ([]Target, error) {
	if target.Path == "" {
		return nil, nil
	}
	q := fmt.Sprintf(
		`MATCH (other)-[]->(t) WHERE t.filePath = '%s' AND other.filePath IS NOT NULL AND other.filePath <> '%s' RETURN DISTINCT other.filePath`,
		cypherEscape(target.Path), cypherEscape(target.Path),
	)
	headers, rows, err := g.runCypher(ctx, q)
	if err != nil {
		return nil, err
	}
	return rowsToTargets(target.Repository, headers, rows, "other.filePath"), nil
}

// Dependencies is the inverse of Dependents over the same edge set.
func (g *GitNexus) Dependencies(ctx context.Context, target Target) ([]Target, error) {
	if target.Path == "" {
		return nil, nil
	}
	q := fmt.Sprintf(
		`MATCH (t)-[]->(other) WHERE t.filePath = '%s' AND other.filePath IS NOT NULL AND other.filePath <> '%s' RETURN DISTINCT other.filePath`,
		cypherEscape(target.Path), cypherEscape(target.Path),
	)
	headers, rows, err := g.runCypher(ctx, q)
	if err != nil {
		return nil, err
	}
	return rowsToTargets(target.Repository, headers, rows, "other.filePath"), nil
}

// PublicContractChanges returns every exported symbol declared in
// any file in the diff. Per the interface contract this is the
// over-reporting fallback — implementations that can reason about
// hunks would narrow further; this binding does not.
func (g *GitNexus) PublicContractChanges(ctx context.Context, diff Diff) ([]Symbol, error) {
	if len(diff.Files) == 0 {
		return nil, nil
	}
	quoted := make([]string, 0, len(diff.Files))
	for _, f := range diff.Files {
		quoted = append(quoted, "'"+cypherEscape(f)+"'")
	}
	q := fmt.Sprintf(
		`MATCH (n) WHERE n.filePath IN [%s] AND n.isExported = true AND n.name IS NOT NULL AND n.name <> '' RETURN n.filePath, n.name, n.label`,
		strings.Join(quoted, ", "),
	)
	headers, rows, err := g.runCypher(ctx, q)
	if err != nil {
		return nil, err
	}
	pathIdx := indexOf(headers, "n.filePath")
	nameIdx := indexOf(headers, "n.name")
	kindIdx := indexOf(headers, "n.label")
	if pathIdx < 0 || nameIdx < 0 {
		return nil, nil
	}
	out := make([]Symbol, 0, len(rows))
	for _, row := range rows {
		if pathIdx >= len(row) || nameIdx >= len(row) {
			continue
		}
		name := row[nameIdx]
		if name == "" {
			continue
		}
		kind := ""
		if kindIdx >= 0 && kindIdx < len(row) {
			kind = row[kindIdx]
		}
		out = append(out, Symbol{
			Target: Target{Repository: diff.Repository, Path: row[pathIdx]},
			Name:   name,
			Kind:   kind,
		})
	}
	return out, nil
}

// Describe reads .gitnexus/meta.json on disk. Available is true only
// when the file parses cleanly; otherwise Reason carries a short
// operator-visible explanation. Describe never returns
// ErrUnavailable per the SourceAnalysis contract.
func (g *GitNexus) Describe(_ context.Context) (Capabilities, error) {
	caps := Capabilities{Backend: "gitnexus"}
	if g.RepoPath == "" {
		caps.Reason = "GitNexus.RepoPath not set"
		return caps, nil
	}
	metaPath := filepath.Join(g.RepoPath, ".gitnexus", "meta.json")
	f, err := os.Open(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			caps.Reason = fmt.Sprintf("no index at %s (run `gitnexus analyze`)", metaPath)
			return caps, nil
		}
		caps.Reason = "meta.json unreadable: " + err.Error()
		return caps, nil
	}
	defer f.Close()
	var meta metaFile
	if err := json.NewDecoder(f).Decode(&meta); err != nil {
		caps.Reason = "meta.json parse: " + err.Error()
		return caps, nil
	}
	caps.Available = true
	caps.IndexUpdatedAt = meta.IndexedAt
	return caps, nil
}

func rowsToTargets(repo string, headers []string, rows [][]string, col string) []Target {
	idx := indexOf(headers, col)
	if idx < 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(rows))
	out := make([]Target, 0, len(rows))
	for _, row := range rows {
		if idx >= len(row) {
			continue
		}
		p := row[idx]
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, Target{Repository: repo, Path: p})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func indexOf(headers []string, name string) int {
	for i, h := range headers {
		if h == name {
			return i
		}
	}
	return -1
}

// cypherEscape handles the single-character class that breaks a
// quoted Cypher string literal. The gitnexus CLI inherits the
// graph engine's escape rules; ' is the only character that ends a
// literal early. Path strings may contain arbitrary characters but
// in practice repo-relative paths only need this minimal escape.
func cypherEscape(s string) string {
	return strings.ReplaceAll(s, "'", `\'`)
}
