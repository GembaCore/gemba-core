package log

import "time"

// Defaults intentionally mirror the gm-3hg contract: 100MB per file, retain
// 10 files, delete files older than 30 days. Any install that never tunes
// these sees a hard cap around 1GB with a 30-day lookback.
const (
	DefaultMaxFileBytes int64         = 100 * 1024 * 1024
	DefaultMaxFiles     int           = 10
	DefaultMaxAge       time.Duration = 30 * 24 * time.Hour
	DefaultBaseName     string        = "events"
)

// Policy is the knobs a Writer obeys. Fields left at the zero value are
// filled with the defaults above when the Writer opens.
type Policy struct {
	// Dir is where files live. Required; no default.
	Dir string

	// BaseName is the filename stem. The active file is "<BaseName>.ndjson";
	// rotated files are "<BaseName>-<UTC-timestamp>-<seq>.ndjson".
	BaseName string

	// MaxFileBytes is the rotation threshold for the active file. Rotation
	// is checked after each write; the active file may briefly exceed this
	// by the size of one event.
	MaxFileBytes int64

	// MaxFiles caps the count of retained rotated files (not counting the
	// active file). Oldest files beyond this are deleted. Zero disables
	// the count cap; negative is invalid.
	MaxFiles int

	// MaxAge is the retention window. Rotated files with mtime older than
	// now-MaxAge are deleted. Zero disables the age cap.
	MaxAge time.Duration

	// PostRotateHook, if set, fires once per rotated file AFTER the rename
	// and BEFORE retention enforcement. Returning an error is surfaced on
	// the Write that triggered rotation, but the rotated file is NOT
	// rolled back — the hook owns archival semantics. A hook that ships to
	// cold storage should return nil only after a durable write.
	PostRotateHook func(rotatedPath string) error

	// Now lets tests inject a clock. Nil means time.Now.
	Now func() time.Time
}

// withDefaults returns a copy of p with zero-valued fields filled in.
// Dir is left as-is (caller error if empty).
func (p Policy) withDefaults() Policy {
	if p.BaseName == "" {
		p.BaseName = DefaultBaseName
	}
	if p.MaxFileBytes == 0 {
		p.MaxFileBytes = DefaultMaxFileBytes
	}
	if p.MaxFiles == 0 {
		p.MaxFiles = DefaultMaxFiles
	}
	if p.MaxAge == 0 {
		p.MaxAge = DefaultMaxAge
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	return p
}
