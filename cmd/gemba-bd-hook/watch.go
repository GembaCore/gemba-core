package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// runWatch is the daemon entry point for `--watch <bd-dir>` (gm-1890).
// Watches `<bd-dir>/issues.jsonl` (bd's auto-export target) for
// modifications, diffs the new content against the cached previous
// snapshot, and POSTs every changed id to /api/workitems/notify.
//
// Closes the cross-process notify gap (gm-e4.3.3) without an upstream
// bd PR: bd's `export.auto=true` (default) rewrites issues.jsonl
// after every successful write within its throttle window, so a
// daemon watching that file gets near-real-time triggers for terminal
// `bd update` invocations and any other tool that goes through bd's
// public API.
//
// Lifecycle:
//   - SIGINT / SIGTERM cause a graceful shutdown (the watcher closes,
//     in-flight POSTs finish, then exit 0).
//   - A vanished issues.jsonl is treated as a transient — bd may
//     recreate it. Watcher continues monitoring the directory.
//   - fsnotify rename events (some editors / atomic-write patterns)
//     trigger a rebind to the new inode.
func runWatch(ctx context.Context, cfg config, client *http.Client, bdDir string, strict bool, source string) int {
	if cfg.URL == "" {
		if strict {
			fatal("watch mode requires GEMBA_NOTIFY_URL (use --url or env)")
		}
		return 0
	}
	if bdDir == "" {
		warn("--watch requires a directory argument")
		if strict {
			os.Exit(2)
		}
		return 0
	}
	abs, err := filepath.Abs(bdDir)
	if err != nil {
		warn("watch: abs(%s): %v", bdDir, err)
		if strict {
			return 1
		}
		return 0
	}
	target := filepath.Join(abs, "issues.jsonl")

	// Resolve the watch dir — we watch the parent so we catch atomic
	// rename-and-replace exporters (bd's auto-export uses one).
	watchDir := abs
	w, err := fsnotify.NewWatcher()
	if err != nil {
		warn("watch: new watcher: %v", err)
		if strict {
			return 1
		}
		return 0
	}
	defer func() { _ = w.Close() }()
	if err := w.Add(watchDir); err != nil {
		warn("watch: add %s: %v", watchDir, err)
		if strict {
			return 1
		}
		return 0
	}

	// Seed the snapshot from the current file (if any) so we don't
	// re-emit historical state on startup. A first-run on a fresh
	// workspace simply starts with an empty cache and emits whatever
	// the next bd write produces.
	snap, _ := loadIssuesSnapshot(target)

	// Trap signals so the daemon shuts down cleanly when the operator
	// hits Ctrl-C or a service supervisor sends SIGTERM.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Coalesce bursts: a single bd write may produce multiple fsnotify
	// events (rename + write + chmod). Drop into a 200ms quiet window
	// before re-reading to avoid POSTing the same diff three times.
	const debounce = 200 * time.Millisecond
	var (
		debounceTimer *time.Timer
		mu            sync.Mutex
	)
	flush := func() {
		mu.Lock()
		defer mu.Unlock()
		next, err := loadIssuesSnapshot(target)
		if err != nil {
			// Transient errors (file briefly absent during atomic
			// rename, partial write) shouldn't kill the daemon.
			warn("watch: read %s: %v", target, err)
			return
		}
		changed := diffSnapshots(snap, next)
		snap = next
		if len(changed) == 0 {
			return
		}
		failed := notifyAll(ctx, client, cfg, changed)
		if failed > 0 && strict {
			cancel()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return 0
		case ev, ok := <-w.Events:
			if !ok {
				return 0
			}
			if !relevantEvent(ev, target) {
				continue
			}
			// Some writers truncate-and-write; reset the debounce
			// timer on every relevant event in the burst.
			mu.Lock()
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounce, flush)
			mu.Unlock()
		case err, ok := <-w.Errors:
			if !ok {
				return 0
			}
			warn("watch: fsnotify: %v", err)
		}
	}
}

// relevantEvent reports whether ev concerns issues.jsonl in any
// meaningful way. We accept Write, Create, and Rename so atomic
// exporters (write-temp + rename-over) don't slip through. Chmod is
// noisy and uninteresting.
func relevantEvent(ev fsnotify.Event, target string) bool {
	if filepath.Clean(ev.Name) != target {
		return false
	}
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
		return true
	}
	return false
}

// issueRow is the minimal subset of bd's exporter shape we care
// about. bd's actual issues.jsonl row carries many more fields; the
// JSON decoder ignores the unknown ones thanks to the default
// permissive behavior.
type issueRow struct {
	ID        string `json:"id"`
	UpdatedAt string `json:"updated_at"`
}

// snapshot is the in-memory state we diff against. Map id →
// UpdatedAt string. Comparing strings is sufficient because bd
// emits a stable RFC3339-derived format; any mutation that doesn't
// bump UpdatedAt is invisible to gemba's UI anyway.
type snapshot map[string]string

// loadIssuesSnapshot reads target as JSONL and returns the
// {id: updated_at} map. A missing file yields an empty map (no
// error) — bd may not have run yet on a fresh workspace.
func loadIssuesSnapshot(target string) (snapshot, error) {
	out := make(snapshot)
	f, err := os.Open(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return out, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var row issueRow
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}
		if row.ID == "" {
			continue
		}
		out[row.ID] = row.UpdatedAt
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// diffSnapshots returns ids whose UpdatedAt is new or has advanced
// since prev. Deletions are NOT emitted today — the notify endpoint
// has no "deleted" semantics. If bd ever supports a soft-delete
// state that mutates UpdatedAt, those will surface naturally.
func diffSnapshots(prev, next snapshot) []string {
	if len(next) == 0 {
		return nil
	}
	out := make([]string, 0)
	for id, updatedAt := range next {
		if old, ok := prev[id]; !ok || old != updatedAt {
			out = append(out, id)
		}
	}
	return out
}

// (silence unused-imports when only some code paths compile under a
// build tag — none today, but keep the toolchain happy.)
var _ = io.EOF
var _ = strings.TrimSpace
var _ = fmt.Sprintf
