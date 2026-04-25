// gemba-bd-hook is the small companion binary an out-of-process bd
// writer (terminal `bd update`, an installed bd post-write hook, an
// ops cron) invokes after a Dolt commit lands so /api/workitems/notify
// gets a chance to fan out a workitem.* GembaEvent (gm-e4.3.3 →
// gm-e4.3.2).
//
// The bead spec rejects a bd CLI wrapper as the primary trigger
// because it only catches actors who use the wrapper. The intended
// long-term path is an upstream bd hook (post-write or post-commit
// after Dolt's auto-commit) that calls this binary; until that PR
// lands operators can wire it via cron, a manual `bd hook run`
// shim, or a per-mutation invocation in their own scripts. The
// id-detection + POST logic lives here so the upstream bd PR stays
// tiny.
//
// Modes:
//
//	gemba-bd-hook --id gm-foo [--id gm-bar ...]
//	gemba-bd-hook --stdin                 # reads ids one per line from stdin
//	gemba-bd-hook --from-dolt-diff HEAD~1 # parses `dolt diff <ref> issues` for changed ids
//
// Configuration via env (or flags as overrides):
//
//	GEMBA_NOTIFY_URL   — base URL of the gemba server (e.g. http://localhost:7666)
//	GEMBA_NOTIFY_AUTH  — optional bearer token for --auth=token gemba serve
//	GEMBA_NOTIFY_SOURCE — optional source hint echoed onto event payload (default "bd-hook")
//
// Fail-open contract: a hook-side error (gemba down, network, auth)
// MUST NOT break the bd mutation that triggered this hook. The
// binary always exits 0 unless the operator passes --strict; on
// errors it logs to stderr and proceeds. Per-id failures don't
// halt the batch.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	envURL    = "GEMBA_NOTIFY_URL"
	envAuth   = "GEMBA_NOTIFY_AUTH"
	envSource = "GEMBA_NOTIFY_SOURCE"

	defaultSource  = "bd-hook"
	defaultTimeout = 5 * time.Second
)

// idFlag accumulates --id flags into a slice.
type idFlag []string

func (i *idFlag) String() string     { return strings.Join(*i, ",") }
func (i *idFlag) Set(v string) error { *i = append(*i, v); return nil }

func main() {
	var (
		ids        idFlag
		fromStdin  bool
		fromDolt   string
		urlFlag    string
		authFlag   string
		sourceFlag string
		strict     bool
		timeout    time.Duration
	)
	flag.Var(&ids, "id", "work-item id to notify; repeat for multiple")
	flag.BoolVar(&fromStdin, "stdin", false, "read ids from stdin, one per line")
	flag.StringVar(&fromDolt, "from-dolt-diff", "",
		"detect ids from `dolt diff <ref> issues`; pass the ref to compare against (e.g. HEAD~1)")
	flag.StringVar(&urlFlag, "url", "", "gemba server base URL (overrides $GEMBA_NOTIFY_URL)")
	flag.StringVar(&authFlag, "auth", "", "bearer token (overrides $GEMBA_NOTIFY_AUTH)")
	flag.StringVar(&sourceFlag, "source", "", "source hint on the emitted event payload (default $GEMBA_NOTIFY_SOURCE or \"bd-hook\")")
	flag.BoolVar(&strict, "strict", false, "exit non-zero on any error (default fail-open)")
	flag.DurationVar(&timeout, "timeout", defaultTimeout, "per-request HTTP timeout")
	flag.Parse()

	cfg := loadConfig(urlFlag, authFlag, sourceFlag)
	if cfg.URL == "" {
		// Fail-open default: no URL configured = no-op. The upstream bd
		// PR's hook script invokes this unconditionally; operators who
		// haven't pointed it at a gemba server should see silent
		// success, not noisy errors. --strict overrides.
		if strict {
			fatal("GEMBA_NOTIFY_URL not set (use --url or env); --strict mode requires a target")
		}
		return
	}

	collected, err := collectIDs(ids, fromStdin, fromDolt)
	if err != nil {
		warn("collect ids: %v", err)
		if strict {
			os.Exit(1)
		}
		return
	}
	if len(collected) == 0 {
		// Nothing to notify. Hook fired on an empty change set
		// (typical for bd mutations that touch metadata only) — exit
		// success so the calling hook chain is happy.
		return
	}

	client := &http.Client{Timeout: timeout}
	failed := notifyAll(context.Background(), client, cfg, collected)
	if failed > 0 && strict {
		os.Exit(1)
	}
}

// config is the resolved hook configuration.
type config struct {
	URL    string // base URL, e.g. http://localhost:7666
	Auth   string // optional bearer token
	Source string // payload source hint
}

func loadConfig(urlFlag, authFlag, sourceFlag string) config {
	c := config{
		URL:    firstNonEmpty(urlFlag, os.Getenv(envURL)),
		Auth:   firstNonEmpty(authFlag, os.Getenv(envAuth)),
		Source: firstNonEmpty(sourceFlag, os.Getenv(envSource), defaultSource),
	}
	c.URL = strings.TrimRight(c.URL, "/")
	return c
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// collectIDs assembles the union of every requested id source. Empty
// ids and duplicates are dropped silently — the upstream caller may
// emit blanks (a mutation that touched no rows under the watched
// table) and those are not errors.
func collectIDs(flagIDs []string, fromStdin bool, fromDoltRef string) ([]string, error) {
	seen := make(map[string]bool)
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range flagIDs {
		add(id)
	}
	if fromStdin {
		ids, err := readIDsFromStdin()
		if err != nil {
			return out, fmt.Errorf("--stdin: %w", err)
		}
		for _, id := range ids {
			add(id)
		}
	}
	if fromDoltRef != "" {
		ids, err := readIDsFromDoltDiff(fromDoltRef)
		if err != nil {
			return out, fmt.Errorf("--from-dolt-diff %q: %w", fromDoltRef, err)
		}
		for _, id := range ids {
			add(id)
		}
	}
	return out, nil
}

// readIDsFromStdin parses one id per line. Comment lines (starting #)
// and blank lines are skipped so a heredoc-style input is convenient
// for ad-hoc invocations.
func readIDsFromStdin() ([]string, error) {
	var out []string
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// readIDsFromDoltDiff shells to `dolt diff <ref> issues --result-format json`
// and extracts the changed `id` column values. Falls back to the
// plain-text format if the JSON flag isn't supported by the local
// Dolt build. Caller is responsible for setting cwd to a directory
// inside the bd Dolt workspace.
func readIDsFromDoltDiff(ref string) ([]string, error) {
	// JSON format first — stable across Dolt versions that support it.
	cmd := exec.Command("dolt", "diff", "--result-format", "json", ref, "HEAD", "issues")
	out, err := cmd.Output()
	if err == nil {
		ids, jerr := parseDoltJSONIDs(out)
		if jerr == nil {
			return ids, nil
		}
		// Fall through to plain-text if the JSON shape is unfamiliar.
	}
	// Plain-text fallback: parse the unified diff for `id` column values.
	cmd = exec.Command("dolt", "diff", ref, "HEAD", "issues")
	out, err = cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("dolt diff: %w (%s)", err, string(ee.Stderr))
		}
		return nil, fmt.Errorf("dolt diff: %w", err)
	}
	return parseDoltPlainIDs(out), nil
}

// parseDoltJSONIDs walks Dolt's JSON diff output and collects every
// distinct `id` value across added / modified / deleted rows.
//
// Dolt's --result-format json shape varies across versions; this
// function tries the most common envelopes.
func parseDoltJSONIDs(b []byte) ([]string, error) {
	// Probe shape A: {"diff":[{"to_id":"gm-foo"},{"from_id":"gm-bar"}]}
	var shapeA struct {
		Diff []map[string]any `json:"diff"`
	}
	if err := json.Unmarshal(b, &shapeA); err == nil && len(shapeA.Diff) > 0 {
		ids := make([]string, 0, len(shapeA.Diff))
		seen := make(map[string]bool)
		for _, row := range shapeA.Diff {
			for _, k := range []string{"to_id", "from_id", "id"} {
				if v, ok := row[k].(string); ok && v != "" && !seen[v] {
					seen[v] = true
					ids = append(ids, v)
				}
			}
		}
		return ids, nil
	}
	// Probe shape B: top-level array of row objects keyed by column name.
	var shapeB []map[string]any
	if err := json.Unmarshal(b, &shapeB); err == nil && len(shapeB) > 0 {
		ids := make([]string, 0, len(shapeB))
		seen := make(map[string]bool)
		for _, row := range shapeB {
			for _, k := range []string{"to_id", "from_id", "id"} {
				if v, ok := row[k].(string); ok && v != "" && !seen[v] {
					seen[v] = true
					ids = append(ids, v)
				}
			}
		}
		return ids, nil
	}
	return nil, errors.New("unrecognized dolt diff json shape")
}

// parseDoltPlainIDs scans the unified-diff text format from
// `dolt diff <ref> HEAD issues` for `id` cell values. The format
// is whitespace-separated columns; the id column appears as the
// first non-marker token on each + or - line.
func parseDoltPlainIDs(b []byte) []string {
	var ids []string
	seen := make(map[string]bool)
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 2 {
			continue
		}
		if line[0] != '+' && line[0] != '-' {
			continue
		}
		// Skip the +++ / --- file-marker lines.
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		// Strip the leading +/- and any markdown table pipes Dolt
		// uses ("|" or "│"). The first non-whitespace cell after the
		// marker is the id.
		body := strings.TrimSpace(line[1:])
		body = strings.TrimLeft(body, "|│ \t")
		fields := strings.FieldsFunc(body, func(r rune) bool {
			return r == '|' || r == '│' || r == ' ' || r == '\t'
		})
		if len(fields) == 0 {
			continue
		}
		id := strings.TrimSpace(fields[0])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// notifyAll POSTs each id to /api/workitems/notify. Returns the
// count of failures; the caller decides how to surface them based on
// --strict.
func notifyAll(ctx context.Context, client *http.Client, cfg config, ids []string) int {
	endpoint := cfg.URL + "/api/workitems/notify"
	failed := 0
	for _, id := range ids {
		if err := postOne(ctx, client, endpoint, cfg.Auth, cfg.Source, id); err != nil {
			warn("notify %s: %v", id, err)
			failed++
			continue
		}
	}
	return failed
}

// postOne issues a single POST. Body shape mirrors the handler at
// internal/server/workitem_notify.go (gm-e4.3.2).
func postOne(ctx context.Context, client *http.Client, endpoint, auth, source, id string) error {
	body, err := json.Marshal(map[string]string{
		"work_item_id": id,
		"source":       source,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read up to 1KiB of the error body for the log line.
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(preview)))
	}
	return nil
}

func warn(format string, args ...any) {
	log.New(os.Stderr, "gemba-bd-hook: ", log.LstdFlags).Printf(format, args...)
}

func fatal(format string, args ...any) {
	warn(format, args...)
	os.Exit(2)
}
