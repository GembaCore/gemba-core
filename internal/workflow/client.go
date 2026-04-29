package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner is the shape of the `bd` shell the workflow client uses. Args
// mirror what the operator types after `bd ` — flags + positional in
// CLI order. Tests stub this with an in-memory map keyed on argv. The
// production runner is NewExecRunner.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// Client wraps the bd formula + mol CLI surfaces. Always construct via
// NewClient (production) or NewClientWithRunner (tests). A nil
// receiver method-call is a programming bug — handlers should check
// that the Router has a workflow client wired before dispatching.
type Client struct {
	run Runner
}

// NewClient resolves `bd` on PATH and returns a Client backed by it.
// beadsDir, when non-empty, becomes the working directory of every
// spawned bd process — mirrors the bd WorkPlane adaptor's pattern so a
// single gemba server can point at any beads workspace on the host.
func NewClient(beadsDir string) (*Client, error) {
	path, err := exec.LookPath("bd")
	if err != nil {
		return nil, fmt.Errorf("workflow: bd CLI not on PATH: %w", err)
	}
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		if beadsDir != "" {
			cmd.Dir = beadsDir
		}
		return cmd.Output()
	}
	return &Client{run: runner}, nil
}

// NewClientWithRunner is the test constructor — handlers stub bd in
// memory by passing a Runner that pattern-matches argv.
func NewClientWithRunner(run Runner) *Client {
	return &Client{run: run}
}

// ErrNotFound is returned when bd reports a formula or mol id is
// unknown. Handler code maps this to 404 rather than the generic 500
// or 503.
var ErrNotFound = errors.New("workflow: not found")

// ErrNotWisp is returned by Burn when the run is a persistent mol —
// the contract is that Burn discards a wisp and refuses persistent
// ones (handlers map this to 409).
var ErrNotWisp = errors.New("workflow: cannot burn persistent mol")

// List wraps `bd formula list --json`. Returns an empty slice when bd
// has no formulas wired — never nil — so handlers can encode the
// empty case directly.
func (c *Client) List(ctx context.Context) ([]Formula, error) {
	out, err := c.run(ctx, "formula", "list", "--json")
	if err != nil {
		return nil, wrapBdErr(err, "bd formula list --json")
	}
	// bd emits a flat array of summary rows (name, type, source, steps,
	// vars counts). The SPA Library only needs name/type/description/
	// source/version, so we decode into a tolerant intermediate.
	var rows []formulaSummary
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("workflow: parse bd formula list: %w", err)
	}
	formulas := make([]Formula, 0, len(rows))
	for _, r := range rows {
		formulas = append(formulas, Formula{
			Name:        r.Name,
			Type:        r.Type,
			Version:     r.Version,
			Description: r.Description,
			SourcePath:  r.Source,
			Variables:   []Variable{}, // summary row carries only a count
		})
	}
	return formulas, nil
}

// Show wraps `bd formula show <name> --json` and `bd cook <name> --json`
// (the latter resolves the DAG so the response includes post-aspect /
// post-expansion steps). When cook fails — typically a formula that
// requires --var inputs even at compile time — we still return what
// formula show gave us; the SPA renders the unresolved view rather
// than 500.
func (c *Client) Show(ctx context.Context, name string) (Formula, error) {
	if strings.TrimSpace(name) == "" {
		return Formula{}, fmt.Errorf("workflow: empty formula name")
	}
	out, err := c.run(ctx, "formula", "show", name, "--json")
	if err != nil {
		if isNotFound(err) {
			return Formula{}, ErrNotFound
		}
		return Formula{}, wrapBdErr(err, "bd formula show "+name+" --json")
	}
	var raw formulaShow
	if err := json.Unmarshal(out, &raw); err != nil {
		return Formula{}, fmt.Errorf("workflow: parse bd formula show: %w", err)
	}
	f := Formula{
		Name:        raw.Formula,
		Type:        raw.Type,
		Version:     raw.Version,
		Description: raw.Description,
		SourcePath:  raw.Source,
		Variables:   raw.Variables,
		Steps:       raw.Steps,
	}
	if f.Variables == nil {
		f.Variables = []Variable{}
	}
	if raw.Extends != nil || raw.Aspects != nil || raw.Expansions != nil {
		f.Composition = &Composition{
			Extends:    raw.Extends,
			Aspects:    raw.Aspects,
			Expansions: raw.Expansions,
		}
	}
	// Best-effort cook for the resolved DAG. Failures here aren't
	// fatal — the unresolved formula view is a useful fallback.
	if cooked, cerr := c.cook(ctx, name); cerr == nil && len(cooked.Steps) > 0 {
		f.Steps = cooked.Steps
	}
	return f, nil
}

// cook runs `bd cook <name> --json` and decodes the resolved formula.
// Cooks in compile-time mode (no --var) so the steps render with
// {{placeholders}} intact; the SPA's Library card is a preview.
func (c *Client) cook(ctx context.Context, name string) (formulaShow, error) {
	out, err := c.run(ctx, "cook", name, "--json")
	if err != nil {
		return formulaShow{}, wrapBdErr(err, "bd cook "+name+" --json")
	}
	var raw formulaShow
	if err := json.Unmarshal(out, &raw); err != nil {
		return formulaShow{}, fmt.Errorf("workflow: parse bd cook: %w", err)
	}
	return raw, nil
}

// ListRuns wraps the closest bd surface for "every active mol + wisp":
//
//	filter == "active" or "" — concat persistent mols (bd list --type
//	  molecule --status open) with active wisps (bd mol wisp list --json).
//	filter == "done"          — closed mols + wisps.
//	filter == "all"           — both.
//
// Note: bd has no `bd mol list` command for persistent mols — we
// emulate via `bd list --type molecule`. This is the documented gap
// in the bead's TODOs.
func (c *Client) ListRuns(ctx context.Context, filter string) ([]Run, error) {
	switch filter {
	case "", "active", "done", "all":
	default:
		return nil, fmt.Errorf("workflow: unknown status filter %q", filter)
	}
	if filter == "" {
		filter = "active"
	}
	out := []Run{}

	// Persistent mols via `bd list --type molecule`.
	molArgs := []string{"list", "--type", "molecule", "--json"}
	switch filter {
	case "active":
		molArgs = append(molArgs, "--status", "open")
	case "done":
		molArgs = append(molArgs, "--status", "closed")
	}
	if mols, err := c.runListBeads(ctx, molArgs); err == nil {
		for _, b := range mols {
			out = append(out, beadToRun(b, false))
		}
	} else {
		return nil, err
	}

	// Wisps via `bd mol wisp list --json` (--all includes closed).
	wispArgs := []string{"mol", "wisp", "list", "--json"}
	if filter == "all" || filter == "done" {
		wispArgs = append(wispArgs, "--all")
	}
	wispOut, err := c.run(ctx, wispArgs...)
	if err != nil {
		return nil, wrapBdErr(err, "bd mol wisp list --json")
	}
	var wispEnv struct {
		Wisps []wispRow `json:"wisps"`
	}
	if err := json.Unmarshal(wispOut, &wispEnv); err != nil {
		return nil, fmt.Errorf("workflow: parse bd mol wisp list: %w", err)
	}
	for _, w := range wispEnv.Wisps {
		// "done" filter: keep only closed wisps. "active" already
		// excluded via missing --all flag. "all" passes through.
		if filter == "done" && w.Status != "closed" {
			continue
		}
		if filter == "active" && w.Status == "closed" {
			continue
		}
		out = append(out, Run{
			ID:        w.ID,
			Formula:   w.Title,
			Status:    w.Status,
			Ephemeral: true,
			Progress:  Progress{},
			StartedAt: w.CreatedAt,
		})
	}
	return out, nil
}

// GetRun wraps `bd mol show <id> --json`. Decodes the root issue +
// children into a Run with derived progress + per-step state.
func (c *Client) GetRun(ctx context.Context, id string) (Run, error) {
	if strings.TrimSpace(id) == "" {
		return Run{}, fmt.Errorf("workflow: empty run id")
	}
	out, err := c.run(ctx, "mol", "show", id, "--json")
	if err != nil {
		if isNotFound(err) {
			return Run{}, ErrNotFound
		}
		return Run{}, wrapBdErr(err, "bd mol show "+id+" --json")
	}
	var raw molShow
	if err := json.Unmarshal(out, &raw); err != nil {
		return Run{}, fmt.Errorf("workflow: parse bd mol show: %w", err)
	}
	if raw.Error != "" {
		return Run{}, ErrNotFound
	}
	if raw.Root == nil {
		return Run{}, ErrNotFound
	}
	run := Run{
		ID:        raw.Root.ID,
		Formula:   raw.Root.Title,
		Status:    raw.Root.Status,
		Ephemeral: raw.Root.Ephemeral,
		StartedAt: raw.Root.CreatedAt,
		Progress:  progressOf(raw.Issues),
		Steps:     stepsOf(raw.Issues, raw.Root.ID),
	}
	if raw.Root.Status == "closed" {
		run.EndedAt = raw.Root.UpdatedAt
	}
	return run, nil
}

// Pour wraps `bd mol pour <formula> --var k=v ...`. Returns the
// resulting Run by re-reading via GetRun so the response shape matches
// what the SPA polls for in the Active runs tab. attachToBead, when
// non-empty, becomes the --parent flag.
func (c *Client) Pour(ctx context.Context, formula string, vars map[string]string, attachToBead string) (Run, error) {
	id, err := c.spawn(ctx, false, formula, vars, attachToBead)
	if err != nil {
		return Run{}, err
	}
	return c.GetRun(ctx, id)
}

// Wisp wraps `bd mol wisp <formula> --var k=v ...`.
func (c *Client) Wisp(ctx context.Context, formula string, vars map[string]string, attachToBead string) (Run, error) {
	id, err := c.spawn(ctx, true, formula, vars, attachToBead)
	if err != nil {
		return Run{}, err
	}
	return c.GetRun(ctx, id)
}

// spawn is the shared pour/wisp invocation. Builds the argv, runs bd,
// and returns the new run's id by parsing bd's stdout. We ask for
// --json so bd emits a structured envelope rather than the human
// banner.
func (c *Client) spawn(ctx context.Context, ephemeral bool, formula string, vars map[string]string, attachToBead string) (string, error) {
	if strings.TrimSpace(formula) == "" {
		return "", fmt.Errorf("workflow: empty formula name")
	}
	verb := "pour"
	if ephemeral {
		verb = "wisp"
	}
	args := []string{"mol", verb, formula, "--json"}
	for k, v := range vars {
		args = append(args, "--var", k+"="+v)
	}
	// bd mol pour exposes --parent via the underlying create flow on
	// some versions; older builds attach via a labels flag. We ride
	// what `bd mol pour --help` advertises today (--assignee), and
	// attach_to_bead lands as a label so the workflow stays linkable
	// even if a future bd version changes the parent flag.
	if attachToBead != "" {
		args = append(args, "--attach", attachToBead)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		if isNotFound(err) {
			return "", ErrNotFound
		}
		return "", wrapBdErr(err, "bd mol "+verb+" "+formula)
	}
	id := parseRunID(out)
	if id == "" {
		return "", fmt.Errorf("workflow: bd mol %s returned no id (output=%q)", verb, truncate(out, 200))
	}
	return id, nil
}

// Burn wraps `bd mol burn <id> --force --json`. Refuses to burn a
// persistent mol — the contract per the bead is that Burn discards a
// wisp; persistent mols 409. We pre-check via GetRun so the rejection
// is visible to handlers as ErrNotWisp without running bd burn at all.
func (c *Client) Burn(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("workflow: empty run id")
	}
	run, err := c.GetRun(ctx, id)
	if err != nil {
		return err
	}
	if !run.Ephemeral {
		return ErrNotWisp
	}
	if _, err := c.run(ctx, "mol", "burn", id, "--force"); err != nil {
		if isNotFound(err) {
			return ErrNotFound
		}
		return wrapBdErr(err, "bd mol burn "+id)
	}
	return nil
}

// ----- internal decoder helpers + bd output shapes ---------------------

// formulaSummary mirrors one row of `bd formula list --json`.
type formulaSummary struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Version     int    `json:"version"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// formulaShow mirrors `bd formula show --json` and `bd cook --json`.
// Both endpoints emit nearly the same envelope; fields not present in
// one stay zero-valued rather than failing the decode.
type formulaShow struct {
	Formula     string     `json:"formula"`
	Type        string     `json:"type"`
	Version     int        `json:"version"`
	Description string     `json:"description"`
	Source      string     `json:"source"`
	Steps       []Step     `json:"steps"`
	Variables   []Variable `json:"variables"`
	Extends     []string   `json:"extends"`
	Aspects     []string   `json:"aspects"`
	Expansions  []string   `json:"expansions"`
}

// molShow mirrors `bd mol show --json`. Error is populated when the id
// is unknown; we treat that as ErrNotFound.
type molShow struct {
	Error  string     `json:"error"`
	Root   *molIssue  `json:"root"`
	Issues []molIssue `json:"issues"`
}

type molIssue struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	IssueType string `json:"issue_type"`
	Ephemeral bool   `json:"ephemeral"`
	Assignee  string `json:"assignee"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// wispRow mirrors `bd mol wisp list --json`'s wisps[].
type wispRow struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// runListBeads runs `bd list ... --json` and decodes a flat array of
// molIssue rows. Used by ListRuns to emulate the missing
// `bd mol list` command.
func (c *Client) runListBeads(ctx context.Context, args []string) ([]molIssue, error) {
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, wrapBdErr(err, "bd "+strings.Join(args, " "))
	}
	var rows []molIssue
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("workflow: parse bd list molecules: %w", err)
	}
	return rows, nil
}

// beadToRun projects a molIssue into a Run. Persistent mols (the
// emulated `bd mol list` rows) carry no progress data — that lands
// when the SPA opens the run detail and we re-read via mol show.
func beadToRun(b molIssue, ephemeral bool) Run {
	r := Run{
		ID:        b.ID,
		Formula:   b.Title,
		Status:    b.Status,
		Ephemeral: ephemeral || b.Ephemeral,
		StartedAt: b.CreatedAt,
	}
	if b.Status == "closed" {
		r.EndedAt = b.UpdatedAt
	}
	return r
}

// progressOf counts done/total over the run's child issues. The root
// itself isn't counted (it's the run, not a step).
func progressOf(issues []molIssue) Progress {
	p := Progress{}
	if len(issues) <= 1 {
		return p
	}
	for i, iss := range issues {
		if i == 0 {
			continue // root
		}
		p.Total++
		if iss.Status == "closed" {
			p.Done++
		}
	}
	return p
}

// stepsOf projects child issues into RunStep records with a derived
// state. State buckets are intentionally coarse — handlers don't need
// to inspect the raw bd status grammar.
func stepsOf(issues []molIssue, rootID string) []RunStep {
	out := make([]RunStep, 0, len(issues))
	for _, iss := range issues {
		if iss.ID == rootID {
			continue
		}
		out = append(out, RunStep{
			ID:            iss.ID,
			Title:         iss.Title,
			State:         stepState(iss.Status),
			AssignedAgent: iss.Assignee,
		})
	}
	return out
}

func stepState(s string) string {
	switch s {
	case "closed":
		return "done"
	case "in_progress":
		return "in-progress"
	case "blocked":
		return "blocked"
	case "open":
		return "ready"
	default:
		return s
	}
}

// parseRunID extracts the new run's id from `bd mol pour|wisp --json`
// stdout. bd's spawn output varies a touch by version: some emit a
// bare `{"id": "..."}` envelope, others the full mol show shape.
// Both have an "id" key at the top level or under a "root" key, so
// we tolerate both.
func parseRunID(out []byte) string {
	var env struct {
		ID   string `json:"id"`
		Root *struct {
			ID string `json:"id"`
		} `json:"root"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return ""
	}
	if env.ID != "" {
		return env.ID
	}
	if env.Root != nil {
		return env.Root.ID
	}
	return ""
}

// isNotFound reports whether err carries bd's "not found" signal.
// Mirrors the bd WorkPlane adaptor's matcher (gm-hif1) so handlers
// see a consistent ErrNotFound regardless of which bd surface
// produced the error.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if exitErr, ok := err.(*exec.ExitError); ok {
		msg = strings.ToLower(msg + " " + string(exitErr.Stderr))
	}
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such") ||
		strings.Contains(msg, "no issue found") ||
		strings.Contains(msg, "no formula found")
}

// wrapBdErr surfaces a bd subprocess failure with the cmdline + first
// stderr line so handlers can pass the message straight to httperr.
func wrapBdErr(cause error, cmdline string) error {
	if cause == nil {
		return nil
	}
	msg := strings.TrimSpace(cause.Error())
	if exitErr, ok := cause.(*exec.ExitError); ok {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			msg = stderr
		}
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return fmt.Errorf("workflow: %s: %s", cmdline, msg)
}

func truncate(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
