package log

import (
	"os"
	"path/filepath"
	"time"
)

// Stats snapshots the on-disk state of an event log directory. Zero values
// mean "no rotated files yet"; OldestRetainedAt is the zero time in that
// case.
//
// Stats is the shape `gemba doctor` and the `/metrics` endpoint expose:
//   - eventlog.total_size_bytes   → TotalSizeBytes
//   - eventlog.oldest_retained_at → OldestRetainedAt (unix seconds)
type Stats struct {
	CurrentPath      string
	ActiveSizeBytes  int64
	RotatedFileCount int
	TotalSizeBytes   int64
	OldestRetainedAt time.Time
}

// ScanDir returns Stats for an event log directory without opening a Writer.
// Useful for out-of-process tools (doctor, ad-hoc inspection). The active
// file "<baseName>.ndjson" is counted if present; rotated files match the
// "<baseName>-*.ndjson" pattern. A missing directory is not an error — the
// returned Stats is the zero value with CurrentPath populated.
func ScanDir(dir, baseName string) (Stats, error) {
	if baseName == "" {
		baseName = DefaultBaseName
	}
	active := filepath.Join(dir, baseName+".ndjson")
	s := Stats{CurrentPath: active}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return s, nil
	}

	rot, err := listRotated(dir, baseName)
	if err != nil {
		return s, err
	}
	for _, f := range rot {
		s.TotalSizeBytes += f.size
		if s.OldestRetainedAt.IsZero() || f.modTime.Before(s.OldestRetainedAt) {
			s.OldestRetainedAt = f.modTime
		}
	}
	s.RotatedFileCount = len(rot)

	if info, statErr := os.Stat(active); statErr == nil {
		s.ActiveSizeBytes = info.Size()
		s.TotalSizeBytes += info.Size()
	}
	return s, nil
}
