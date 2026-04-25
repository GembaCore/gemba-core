package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// osPipe returns a connected pipe; tests use it to pump fake stdin.
func osPipe(t *testing.T) (*os.File, *os.File, error) {
	t.Helper()
	return os.Pipe()
}

// swapStdin redirects os.Stdin to r for the duration of the test.
func swapStdin(t *testing.T, r *os.File) {
	t.Helper()
	saved := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = saved })
}

func TestParseDoltPlainIDs_StandardForm(t *testing.T) {
	in := []byte(`+++ b/issues
--- a/issues
@@ ...
| id        | title    | status |
+ gm-foo    | x        | open
- gm-bar    | y        | closed
+ gm-foo    | z        | in_progress
`)
	got := parseDoltPlainIDs(in)
	want := []string{"gm-foo", "gm-bar"} // dedup; order = first-seen
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

// The +++/--- header lines must be ignored — they're file markers,
// not row diffs.
func TestParseDoltPlainIDs_SkipsFileMarkers(t *testing.T) {
	in := []byte(`+++ b/issues
--- a/issues
+ gm-real | x | open
`)
	got := parseDoltPlainIDs(in)
	if len(got) != 1 || got[0] != "gm-real" {
		t.Errorf("got %v", got)
	}
}

func TestParseDoltJSONIDs_ShapeA(t *testing.T) {
	body := []byte(`{
		"diff": [
			{"to_id": "gm-foo", "from_id": "gm-foo", "title": "x"},
			{"to_id": "gm-bar"},
			{"from_id": "gm-baz"}
		]
	}`)
	got, err := parseDoltJSONIDs(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{"gm-foo", "gm-bar", "gm-baz"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseDoltJSONIDs_ShapeB_ArrayOfRows(t *testing.T) {
	body := []byte(`[
		{"to_id": "gm-foo"},
		{"id": "gm-bar"}
	]`)
	got, err := parseDoltJSONIDs(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v", got)
	}
}

func TestParseDoltJSONIDs_RejectsUnknownShape(t *testing.T) {
	if _, err := parseDoltJSONIDs([]byte(`"a string"`)); err == nil {
		t.Error("expected error on unknown shape")
	}
	if _, err := parseDoltJSONIDs([]byte(`{}`)); err == nil {
		t.Error("expected error on empty object")
	}
}

func TestCollectIDs_FlagAndStdin(t *testing.T) {
	// Pipe ids through stdin via os.Pipe — simulates a heredoc.
	r, w, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	go func() {
		defer w.Close()
		_, _ = w.Write([]byte("# comment\ngm-stdin-1\n\ngm-stdin-2\n"))
	}()

	swapStdin(t, r)
	ids, err := collectIDs([]string{"gm-flag-1", "gm-flag-1"}, true, "")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := []string{"gm-flag-1", "gm-stdin-1", "gm-stdin-2"}
	if len(ids) != len(want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
}

// loadConfig flag wins over env; env wins over default.
func TestLoadConfig_FlagWinsOverEnv(t *testing.T) {
	t.Setenv(envURL, "http://env-host")
	t.Setenv(envSource, "env-source")
	cfg := loadConfig("http://flag-host/", "", "flag-source")
	if cfg.URL != "http://flag-host" {
		t.Errorf("URL = %q, want flag-host (no trailing slash)", cfg.URL)
	}
	if cfg.Source != "flag-source" {
		t.Errorf("Source = %q, want flag-source", cfg.Source)
	}
}

func TestLoadConfig_EnvFillsDefaults(t *testing.T) {
	t.Setenv(envURL, "http://env-host")
	t.Setenv(envAuth, "tok")
	t.Setenv(envSource, "")
	cfg := loadConfig("", "", "")
	if cfg.URL != "http://env-host" {
		t.Errorf("URL = %q", cfg.URL)
	}
	if cfg.Auth != "tok" {
		t.Errorf("Auth = %q", cfg.Auth)
	}
	if cfg.Source != defaultSource {
		t.Errorf("Source = %q, want default %q", cfg.Source, defaultSource)
	}
}

// notifyAll sends one POST per id and counts failures.
func TestNotifyAll_HappyPath(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			WorkItemID string `json:"work_item_id"`
			Source     string `json:"source"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		seen = append(seen, body.WorkItemID)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"work_item_id": body.WorkItemID, "kind": "workitem_updated",
		})
	}))
	t.Cleanup(srv.Close)

	cfg := config{URL: srv.URL, Auth: "tok", Source: "test"}
	failed := notifyAll(context.Background(), &http.Client{Timeout: time.Second},
		cfg, []string{"gm-foo", "gm-bar"})
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
	if len(seen) != 2 || seen[0] != "gm-foo" || seen[1] != "gm-bar" {
		t.Errorf("seen = %v", seen)
	}
}

// 4xx/5xx from the server count as failures but don't halt the batch.
func TestNotifyAll_PartialFailureCounted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			WorkItemID string `json:"work_item_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.WorkItemID == "gm-bad" {
			http.Error(w, `{"error":"bad"}`, http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := config{URL: srv.URL}
	failed := notifyAll(context.Background(), &http.Client{Timeout: time.Second},
		cfg, []string{"gm-good", "gm-bad", "gm-also-good"})
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
}

// Network error → still proceeds to next id.
func TestNotifyAll_NetworkErrorContinues(t *testing.T) {
	cfg := config{URL: "http://127.0.0.1:1"} // closed port; connect-refused
	client := &http.Client{Timeout: 200 * time.Millisecond}
	failed := notifyAll(context.Background(), client, cfg,
		[]string{"gm-a", "gm-b", "gm-c"})
	if failed != 3 {
		t.Errorf("failed = %d, want 3 (every post errors)", failed)
	}
}

func TestPostOne_NonJSONErrorBodyStillSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal goof", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	err := postOne(context.Background(),
		&http.Client{Timeout: time.Second},
		srv.URL+"/api/workitems/notify", "", "src", "gm-x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error missing status: %v", err)
	}
	if !strings.Contains(err.Error(), "internal goof") {
		t.Errorf("error missing body preview: %v", err)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "x"); got != "x" {
		t.Errorf("got %q", got)
	}
	if got := firstNonEmpty("a", "b"); got != "a" {
		t.Errorf("got %q, want first", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
