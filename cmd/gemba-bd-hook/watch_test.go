package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

// gm-1890: snapshot diff returns ids that are new OR have an
// advanced UpdatedAt. Unchanged ids stay quiet.
func TestDiffSnapshots(t *testing.T) {
	prev := snapshot{
		"gm-a": "2026-04-25T10:00:00Z",
		"gm-b": "2026-04-25T10:00:00Z",
	}
	next := snapshot{
		"gm-a": "2026-04-25T10:00:00Z", // unchanged
		"gm-b": "2026-04-25T11:00:00Z", // bumped
		"gm-c": "2026-04-25T12:00:00Z", // new
	}
	got := diffSnapshots(prev, next)
	sort.Strings(got)
	want := []string{"gm-b", "gm-c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiffSnapshots_FirstRun(t *testing.T) {
	// An empty prev (cold start) emits every id in next.
	next := snapshot{"gm-a": "t", "gm-b": "t"}
	got := diffSnapshots(nil, next)
	sort.Strings(got)
	want := []string{"gm-a", "gm-b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiffSnapshots_DeletionsAreSilent(t *testing.T) {
	// id present in prev but absent from next emits nothing — the
	// notify endpoint has no deletion semantics today.
	prev := snapshot{"gm-a": "t", "gm-b": "t"}
	next := snapshot{"gm-a": "t"}
	if got := diffSnapshots(prev, next); len(got) != 0 {
		t.Errorf("got %v, want empty (deletions silent)", got)
	}
}

func TestLoadIssuesSnapshot_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	body := `{"id":"gm-1","title":"a","updated_at":"2026-04-25T10:00:00Z"}
{"id":"gm-2","updated_at":"2026-04-25T11:00:00Z"}
` + "\n" /* trailing blank line */
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := loadIssuesSnapshot(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got["gm-1"] != "2026-04-25T10:00:00Z" || got["gm-2"] != "2026-04-25T11:00:00Z" {
		t.Errorf("got %+v", got)
	}
}

func TestLoadIssuesSnapshot_MissingFileEmpty(t *testing.T) {
	got, err := loadIssuesSnapshot(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Errorf("missing file should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want empty", got)
	}
}

func TestLoadIssuesSnapshot_MalformedLineSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	// Mix of valid + garbage + comment-style + empty lines.
	body := `{"id":"gm-1","updated_at":"t1"}
not json
# bd auto-export sometimes emits header comments
{"id":"gm-2","updated_at":"t2"}
`
	_ = os.WriteFile(path, []byte(body), 0o644)
	got, _ := loadIssuesSnapshot(path)
	if len(got) != 2 || got["gm-1"] != "t1" || got["gm-2"] != "t2" {
		t.Errorf("got %+v, want both gm-1 + gm-2", got)
	}
}

// runWatch end-to-end: write issues.jsonl, mutate it, expect a POST
// to land on the test server within ~1s. Validates the fsnotify →
// debounce → diff → POST path.
func TestRunWatch_PostsOnFileWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "issues.jsonl")

	// Initial snapshot: one id.
	if err := os.WriteFile(target, []byte(`{"id":"gm-existing","updated_at":"t0"}`+"\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var (
		mu     sync.Mutex
		posted []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			WorkItemID string `json:"work_item_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		posted = append(posted, body.WorkItemID)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"work_item_id": body.WorkItemID, "kind": "workitem_updated",
		})
	}))
	t.Cleanup(srv.Close)

	cfg := config{URL: srv.URL, Source: "watch-test"}
	client := &http.Client{Timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan int, 1)
	go func() {
		done <- runWatch(ctx, cfg, client, dir, false, cfg.Source)
	}()
	// Give the watcher a moment to bind + seed snapshot.
	time.Sleep(150 * time.Millisecond)

	// Append a new id — should trigger a POST after the debounce window.
	body := `{"id":"gm-existing","updated_at":"t0"}
{"id":"gm-new","updated_at":"t1"}
`
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(posted)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(posted) != 1 || posted[0] != "gm-new" {
		t.Errorf("got %v, want [gm-new]", posted)
	}
}

// runWatch returns 0 (fail-open) when GEMBA_NOTIFY_URL is unset.
// Invariant: the watch-mode binary is safe to invoke unconditionally
// from a service-supervisor / cron without a gemba server.
func TestRunWatch_NoURLIsFailOpen(t *testing.T) {
	dir := t.TempDir()
	cfg := config{URL: ""} // not configured
	rc := runWatch(context.Background(), cfg, &http.Client{}, dir, false, "test")
	if rc != 0 {
		t.Errorf("rc = %d, want 0 (fail-open)", rc)
	}
}

// --strict + no URL → exit 2.
func TestRunWatch_StrictRequiresURL(t *testing.T) {
	if os.Getenv("GO_TEST_RUNWATCH_STRICT") == "1" {
		// Subprocess body: would call runWatch with strict + no URL,
		// which fatals via os.Exit. We can't easily exercise that
		// path inline without forking. Skipped here to keep the
		// test file pure-unit; the strict path is exercised via
		// the integration test in internal/adapter/bd/.
		os.Exit(2)
	}
	t.Skip("strict-no-url exits via fatal(); covered by integration test")
}
