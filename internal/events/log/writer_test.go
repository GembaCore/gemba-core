package log

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// readAll reads and returns the full contents of every NDJSON file in dir,
// sorted by filename so tests get a deterministic order.
func readAll(t *testing.T, dir string) []byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ndjson") {
			names = append(names, e.Name())
		}
	}
	// Sorted by filename: active ("events.ndjson") sorts AFTER
	// "events-<ts>-<seq>.ndjson" because '-' < '.' in ASCII. That gives us
	// "oldest rotated → newest rotated → active" order when sequences and
	// timestamps are monotonically increasing.
	var buf bytes.Buffer
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		buf.Write(b)
	}
	return buf.Bytes()
}

func TestOpen_RequiresDir(t *testing.T) {
	if _, err := Open(Policy{}); err == nil {
		t.Fatal("want error when Dir empty, got nil")
	}
}

func TestOpen_RejectsNegativePolicy(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(Policy{Dir: dir, MaxFileBytes: -1}); err == nil {
		t.Fatal("want error for negative MaxFileBytes")
	}
}

func TestWriteEvent_RejectsNewlineInPayload(t *testing.T) {
	w, err := Open(Policy{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.WriteEvent([]byte(`{"a":"b\nc"}` + "\n" + `{"x":1}`)); err == nil {
		t.Fatal("want error for embedded newline, got nil")
	}
}

func TestWriteEvent_AppendsNewline(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(Policy{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := w.WriteEvent([]byte(fmt.Sprintf(`{"i":%d}`, i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "events.ndjson"))
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(lines), b)
	}
	var obj map[string]int
	if err := json.Unmarshal([]byte(lines[1]), &obj); err != nil {
		t.Fatalf("line not valid JSON: %v", err)
	}
	if obj["i"] != 1 {
		t.Fatalf("want i=1, got %v", obj)
	}
}

func TestRotation_SizeBased(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(Policy{
		Dir:          dir,
		MaxFileBytes: 50,  // tiny; forces rotation after a couple events
		MaxFiles:     100, // don't trigger count retention here
		MaxAge:       time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Each event is ~20 bytes; write 20, expect multiple rotations.
	for i := 0; i < 20; i++ {
		if err := w.WriteEvent([]byte(fmt.Sprintf(`{"i":%d}`, i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if w.Rotations() == 0 {
		t.Fatal("expected at least one rotation")
	}
	// All 20 events must be accounted for across files.
	all := readAll(t, dir)
	lines := strings.Split(strings.TrimRight(string(all), "\n"), "\n")
	if len(lines) != 20 {
		t.Fatalf("want 20 total events, got %d", len(lines))
	}
}

func TestRetention_CountCap(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(Policy{
		Dir:          dir,
		MaxFileBytes: 30,
		MaxFiles:     2, // keep only 2 rotated + active
		MaxAge:       time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if err := w.WriteEvent([]byte(fmt.Sprintf(`{"i":%d}`, i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	files, _ := listRotated(dir, DefaultBaseName)
	if len(files) > 2 {
		t.Fatalf("count cap violated: %d rotated files retained, want <=2", len(files))
	}
	// And the active file must still exist.
	if _, err := os.Stat(filepath.Join(dir, "events.ndjson")); err != nil {
		t.Fatalf("active file missing: %v", err)
	}
}

func TestRetention_AgeCap(t *testing.T) {
	dir := t.TempDir()
	// Fake clock: start in the past, jump forward after seeding files.
	var clockNano atomic.Int64
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clockNano.Store(start.UnixNano())
	nowFn := func() time.Time { return time.Unix(0, clockNano.Load()).UTC() }

	w, err := Open(Policy{
		Dir:          dir,
		MaxFileBytes: 30,
		MaxFiles:     100, // don't trip count cap
		MaxAge:       24 * time.Hour,
		Now:          nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Seed rotations while "time" is at start.
	for i := 0; i < 10; i++ {
		if err := w.WriteEvent([]byte(`{"seed":true}`)); err != nil {
			t.Fatal(err)
		}
	}
	// Back-date every existing rotated file by 2 days.
	files, _ := listRotated(dir, DefaultBaseName)
	if len(files) == 0 {
		t.Fatal("expected rotated files from seed phase")
	}
	past := start.Add(-2 * 24 * time.Hour)
	for _, f := range files {
		if err := os.Chtimes(f.path, past, past); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	// Jump the clock forward so retention's "now - MaxAge" is after `past`.
	clockNano.Store(start.Add(2 * time.Hour).UnixNano())
	// Force a fresh rotation by writing until rotation fires — that's when
	// retention runs.
	for i := 0; i < 20; i++ {
		if err := w.WriteEvent([]byte(`{"new":true}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	remaining, _ := listRotated(dir, DefaultBaseName)
	// Back-dated files should all be gone; any remaining must have mtime
	// within the retention window.
	cutoff := nowFn().Add(-24 * time.Hour)
	for _, f := range remaining {
		if f.modTime.Before(cutoff) {
			t.Fatalf("file %s survived age retention (mtime %s, cutoff %s)",
				f.path, f.modTime, cutoff)
		}
	}
}

func TestPostRotateHook_FiresOncePerRotation(t *testing.T) {
	dir := t.TempDir()
	var hookCalls atomic.Uint64
	var seen []string
	var mu sync.Mutex

	w, err := Open(Policy{
		Dir:          dir,
		MaxFileBytes: 30,
		MaxFiles:     100,
		MaxAge:       time.Hour,
		PostRotateHook: func(rotatedPath string) error {
			hookCalls.Add(1)
			mu.Lock()
			seen = append(seen, rotatedPath)
			mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := w.WriteEvent([]byte(fmt.Sprintf(`{"i":%d}`, i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if uint64(hookCalls.Load()) != w.Rotations() {
		t.Fatalf("hook fired %d times, rotations=%d", hookCalls.Load(), w.Rotations())
	}
	// Every path must exist at hook time (before retention) — verify by
	// asserting the paths are unique and end with .ndjson.
	seenMap := map[string]bool{}
	for _, p := range seen {
		if seenMap[p] {
			t.Fatalf("hook saw duplicate path %s", p)
		}
		seenMap[p] = true
		if !strings.HasSuffix(p, ".ndjson") {
			t.Fatalf("hook path not an ndjson file: %s", p)
		}
	}
}

func TestPostRotateHook_AsArchivalSink(t *testing.T) {
	// A hook that MOVES rotated files out of Dir demonstrates the
	// "archive to cold storage" pattern. Retention must not complain
	// about the files it expected to see.
	dir := t.TempDir()
	archive := t.TempDir()

	w, err := Open(Policy{
		Dir:          dir,
		MaxFileBytes: 30,
		MaxFiles:     2,
		MaxAge:       time.Hour,
		PostRotateHook: func(rotatedPath string) error {
			return os.Rename(rotatedPath, filepath.Join(archive, filepath.Base(rotatedPath)))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := w.WriteEvent([]byte(fmt.Sprintf(`{"i":%d}`, i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// All rotated files must be in the archive; Dir has only the active file.
	inDir, _ := listRotated(dir, DefaultBaseName)
	if len(inDir) != 0 {
		t.Fatalf("expected no rotated files in dir after archival, got %d", len(inDir))
	}
	archived, _ := listRotated(archive, DefaultBaseName)
	if len(archived) == 0 {
		t.Fatal("expected archived files in archive dir")
	}
}

func TestStats_ReflectsOnDiskState(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(Policy{Dir: dir, MaxFileBytes: 40, MaxFiles: 100, MaxAge: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_ = w.WriteEvent([]byte(fmt.Sprintf(`{"i":%d}`, i)))
	}
	s := w.Stats()
	if s.CurrentPath == "" {
		t.Fatal("Stats.CurrentPath empty")
	}
	if s.TotalSizeBytes <= 0 {
		t.Fatalf("Stats.TotalSizeBytes=%d want >0", s.TotalSizeBytes)
	}
	if s.RotatedFileCount == 0 {
		t.Fatal("Stats.RotatedFileCount=0; expected at least one rotation")
	}
	if s.OldestRetainedAt.IsZero() {
		t.Fatal("Stats.OldestRetainedAt zero after rotation")
	}
	_ = w.Close()

	// ScanDir must agree with the Writer's snapshot.
	scanned, err := ScanDir(dir, DefaultBaseName)
	if err != nil {
		t.Fatal(err)
	}
	if scanned.TotalSizeBytes != s.TotalSizeBytes {
		t.Fatalf("ScanDir total=%d, Writer.Stats total=%d",
			scanned.TotalSizeBytes, s.TotalSizeBytes)
	}
}

func TestScanDir_MissingDir(t *testing.T) {
	s, err := ScanDir(filepath.Join(t.TempDir(), "nope"), "events")
	if err != nil {
		t.Fatalf("missing dir should not error, got %v", err)
	}
	if s.TotalSizeBytes != 0 || s.RotatedFileCount != 0 {
		t.Fatalf("missing dir should yield zero stats, got %+v", s)
	}
}

func TestConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(Policy{Dir: dir, MaxFileBytes: 1024, MaxFiles: 100, MaxAge: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	const writers = 8
	const perWriter = 500
	var wg sync.WaitGroup
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if err := w.WriteEvent([]byte(fmt.Sprintf(`{"g":%d,"i":%d}`, g, i))); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// Count total lines across all files. No partial lines or data races
	// should reduce the total.
	all := readAll(t, dir)
	lines := 0
	for _, line := range strings.Split(string(all), "\n") {
		if line != "" {
			lines++
		}
	}
	if lines != writers*perWriter {
		t.Fatalf("lost writes under concurrency: want %d, got %d",
			writers*perWriter, lines)
	}
}

// TestStressBounded verifies the hard contract from gm-3hg DoD: "A 1M-event
// stress test does not grow beyond the configured window." We shrink the
// window so the test runs in seconds but the invariant is identical — after
// N events the on-disk footprint stays under (MaxFiles+1) * MaxFileBytes.
func TestStressBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped in short mode")
	}
	dir := t.TempDir()
	const (
		maxFileBytes int64 = 32 * 1024 // 32KB per file
		maxFiles           = 5
		totalEvents        = 1_000_000
	)
	w, err := Open(Policy{
		Dir:          dir,
		MaxFileBytes: maxFileBytes,
		MaxFiles:     maxFiles,
		MaxAge:       24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Fixed-ish-size payload; exact bytes don't matter, bounded footprint does.
	payload := []byte(`{"event":"workitem.updated","id":"abc-123","ts":"2026-04-21T09:35:18Z"}`)
	for i := 0; i < totalEvents; i++ {
		if err := w.WriteEvent(payload); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := ScanDir(dir, DefaultBaseName)
	if err != nil {
		t.Fatal(err)
	}
	// Upper bound: (maxFiles retained rotated) + 1 active, each ≤ maxFileBytes.
	// Retention runs after rotation, so the active file may hold up to
	// maxFileBytes of pending writes.
	upper := int64(maxFiles+1) * maxFileBytes
	// Allow 10% slack for the rotation overshoot — a single event may push
	// the active file past the threshold before the next-write rotation.
	upper = upper + upper/10
	if s.TotalSizeBytes > upper {
		t.Fatalf("disk footprint %d bytes exceeds upper bound %d bytes after %d events",
			s.TotalSizeBytes, upper, totalEvents)
	}
	// And we must have actually rotated — otherwise the test is not
	// exercising rotation at all.
	if w.Rotations() < 10 {
		t.Fatalf("expected many rotations, got %d", w.Rotations())
	}
}
