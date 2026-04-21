package log

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Writer is an append-only NDJSON event log with size-based rotation and
// count+age retention. Safe for concurrent WriteEvent calls.
type Writer struct {
	policy Policy

	mu       sync.Mutex
	active   *os.File
	size     int64 // bytes written to the active file
	rotSeq   uint64
	closed   bool
	fsyncEvr bool // if true, fsync on every write (tests)

	// totalBytes is a snapshot of retained disk usage (active + rotated)
	// updated after each rotation. Reads from Stats lock mu to refresh.
	totalBytes int64

	// rotations count is useful in tests and observability.
	rotations atomic.Uint64
}

// Open prepares a Writer against p.Dir. The directory is created with 0o755
// if missing. An existing active file is reopened for append; rotated files
// already in the directory are preserved and counted by Stats.
func Open(p Policy) (*Writer, error) {
	if p.Dir == "" {
		return nil, errors.New("eventlog: Policy.Dir is required")
	}
	if p.MaxFileBytes < 0 || p.MaxFiles < 0 || p.MaxAge < 0 {
		return nil, errors.New("eventlog: policy fields must be non-negative")
	}
	p = p.withDefaults()

	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("eventlog: mkdir %q: %w", p.Dir, err)
	}

	active := filepath.Join(p.Dir, p.BaseName+".ndjson")
	f, err := os.OpenFile(active, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("eventlog: open active %q: %w", active, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("eventlog: stat active %q: %w", active, err)
	}

	w := &Writer{policy: p, active: f, size: info.Size()}
	// Seed a retained-bytes snapshot so early Stats calls are accurate
	// without forcing the caller to write first.
	w.refreshTotalsLocked()
	return w, nil
}

// WriteEvent appends payload as a single NDJSON line. payload MUST be a
// single JSON object with no embedded newline; a newline is appended by the
// writer. Returns after the bytes are written to the OS (not fsynced).
func (w *Writer) WriteEvent(payload []byte) error {
	if bytes.IndexByte(payload, '\n') >= 0 {
		return errors.New("eventlog: payload contains newline")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return errors.New("eventlog: writer closed")
	}

	// Rotate FIRST if the current file is already at/over the threshold.
	// Rotating before the write keeps the active file close to the
	// configured size and avoids a single event blowing past the cap by a
	// full event's worth of bytes each rotation cycle.
	if w.size >= w.policy.MaxFileBytes {
		if err := w.rotateLocked(); err != nil {
			return err
		}
	}

	n, err := w.active.Write(append(payload, '\n'))
	if err != nil {
		// Partial write: size is now inaccurate but the file is in an
		// unknown state anyway. Fail the Writer.
		w.closed = true
		_ = w.active.Close()
		return fmt.Errorf("eventlog: write: %w", err)
	}
	w.size += int64(n)
	if w.fsyncEvr {
		_ = w.active.Sync()
	}
	return nil
}

// Close closes the active file. After Close, WriteEvent returns an error.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.active.Close()
}

// Stats returns a snapshot of the log's on-disk state. The snapshot re-scans
// the directory under the Writer's lock so it is consistent even while other
// goroutines are writing.
func (w *Writer) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.statsLocked()
}

// Rotations returns the number of rotations performed in this writer's
// lifetime. Monotonic; concurrent-safe.
func (w *Writer) Rotations() uint64 { return w.rotations.Load() }

// rotateLocked closes the active file, renames it with a UTC timestamp +
// monotonic sequence, fires the post-rotate hook, enforces retention, and
// opens a new active file. Caller must hold w.mu.
func (w *Writer) rotateLocked() error {
	if err := w.active.Close(); err != nil {
		return fmt.Errorf("eventlog: close active on rotate: %w", err)
	}

	w.rotSeq++
	ts := w.policy.Now().UTC().Format("20060102T150405Z")
	rotated := filepath.Join(w.policy.Dir,
		fmt.Sprintf("%s-%s-%06d.ndjson", w.policy.BaseName, ts, w.rotSeq))

	activePath := filepath.Join(w.policy.Dir, w.policy.BaseName+".ndjson")
	if err := os.Rename(activePath, rotated); err != nil {
		return fmt.Errorf("eventlog: rename %q -> %q: %w", activePath, rotated, err)
	}

	var hookErr error
	if w.policy.PostRotateHook != nil {
		hookErr = w.policy.PostRotateHook(rotated)
	}

	// Enforce retention AFTER the hook runs so archival sees the file.
	if err := w.enforceRetentionLocked(); err != nil {
		// Retention failure is recoverable — log it via hookErr but keep
		// the writer alive; next rotation will retry.
		if hookErr == nil {
			hookErr = err
		}
	}

	// Re-open a fresh active file.
	f, err := os.OpenFile(activePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("eventlog: reopen active after rotate: %w", err)
	}
	w.active = f
	w.size = 0
	w.rotations.Add(1)
	w.refreshTotalsLocked()

	return hookErr
}

// enforceRetentionLocked deletes rotated files that exceed MaxFiles (by
// count, keeping newest) OR are older than MaxAge. Caller must hold w.mu.
// The active file is never touched.
func (w *Writer) enforceRetentionLocked() error {
	files, err := listRotated(w.policy.Dir, w.policy.BaseName)
	if err != nil {
		return err
	}
	// Newest first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	cutoff := w.policy.Now().Add(-w.policy.MaxAge)
	var firstErr error
	for i, fi := range files {
		drop := false
		if w.policy.MaxFiles > 0 && i >= w.policy.MaxFiles {
			drop = true
		}
		if w.policy.MaxAge > 0 && fi.modTime.Before(cutoff) {
			drop = true
		}
		if !drop {
			continue
		}
		if err := os.Remove(fi.path); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("eventlog: retention remove %q: %w", fi.path, err)
		}
	}
	return firstErr
}

func (w *Writer) refreshTotalsLocked() {
	s := w.statsLocked()
	w.totalBytes = s.TotalSizeBytes
}

func (w *Writer) statsLocked() Stats {
	files, err := listRotated(w.policy.Dir, w.policy.BaseName)
	s := Stats{CurrentPath: filepath.Join(w.policy.Dir, w.policy.BaseName+".ndjson")}
	if err == nil {
		s.RotatedFileCount = len(files)
		for _, f := range files {
			s.TotalSizeBytes += f.size
			if s.OldestRetainedAt.IsZero() || f.modTime.Before(s.OldestRetainedAt) {
				s.OldestRetainedAt = f.modTime
			}
		}
	}
	// Add active file size. Use our tracked counter when available (it
	// reflects in-flight writes before the OS has flushed stat).
	s.ActiveSizeBytes = w.size
	s.TotalSizeBytes += w.size
	return s
}

// SetFsyncOnWrite toggles fsync-after-every-write, intended for tests that
// need durability guarantees before inspecting on-disk state. The default
// (no fsync) matches production behavior: the OS flushes on its own cadence.
func (w *Writer) SetFsyncOnWrite(on bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fsyncEvr = on
}

// rotatedFile is internal bookkeeping for retention scans.
type rotatedFile struct {
	path    string
	size    int64
	modTime time.Time
}

// listRotated returns every file in dir matching "<base>-*.ndjson" — i.e.
// rotated files, excluding the active file "<base>.ndjson".
func listRotated(dir, base string) ([]rotatedFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("eventlog: readdir %q: %w", dir, err)
	}
	prefix := base + "-"
	const suffix = ".ndjson"
	out := make([]rotatedFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) <= len(prefix)+len(suffix) {
			continue
		}
		if name[:len(prefix)] != prefix || name[len(name)-len(suffix):] != suffix {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, rotatedFile{
			path:    filepath.Join(dir, name),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}
	return out, nil
}
