package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
)

// AuditLog is the on-disk store for [corepersona.PersonaConsultRecord]
// rows. Writes are append-only and atomic at the file level (write to
// a temp file in the same directory, then rename) so a process crash
// mid-write never produces a half-row that fails to decode.
//
// Layout:
//
//	<root>/
//	  consults/
//	    YYYY-MM-DD/
//	      <consult_id>.json
//
// Date partitioning keeps directories small even for high-volume
// workspaces (consults are operator-initiated, but a managed-mode
// auto-apply path could fire dozens per hour). Listing scans only
// directories whose name falls in the requested date range.
//
// Concurrency: AuditLog methods are safe for concurrent use. Writes
// take a per-instance mutex so two goroutines appending different
// consults don't race on the directory mkdir step; reads are
// lock-free (filesystem is the source of truth).
type AuditLog struct {
	root string
	mu   sync.Mutex
}

// NewAuditLog returns an AuditLog rooted at root. The directory does
// not need to exist yet — Append creates it on first write. Pass an
// empty string to use [DefaultRoot].
func NewAuditLog(root string) *AuditLog {
	if root == "" {
		root = DefaultRoot()
	}
	return &AuditLog{root: root}
}

// DefaultRoot returns the canonical audit-log directory under the
// current user's home. Falls back to /tmp/gemba-persona/ when the
// home dir is unavailable so tests under a process with no $HOME
// (some CI sandboxes) still produce a usable path.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "gemba-persona")
	}
	return filepath.Join(home, ".gemba", "persona")
}

// Root returns the audit-log root path. Exposed so HTTP handlers can
// surface it under a diagnostic endpoint.
func (l *AuditLog) Root() string { return l.root }

// Append writes one PersonaConsultRecord to disk. The record is
// keyed by (date, ID) — date taken from r.StartedAt — so the
// directory layout is partitioned by day. Append rejects records
// missing ID, StartedAt, PersonaID, or SkillID; those are the
// invariants the lookup paths rely on.
//
// Existing rows with the same ID are overwritten. This is intentional:
// a consult can be updated post-hoc (operator applies a SuggestedAction
// later, the AppliedIdx field gets new entries) and the latest row is
// authoritative. Callers needing append-only history should layer a
// version field on top.
func (l *AuditLog) Append(r corepersona.PersonaConsultRecord) error {
	if err := validateRecord(r); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	dir := l.dirFor(r.StartedAt)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("persona/auditlog: mkdir %s: %w", dir, err)
	}
	final := filepath.Join(dir, r.ID+".json")
	tmp, err := os.CreateTemp(dir, "."+r.ID+".*.tmp")
	if err != nil {
		return fmt.Errorf("persona/auditlog: temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup if anything below fails before rename.
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("persona/auditlog: encode %s: %w", r.ID, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("persona/auditlog: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, final); err != nil {
		return fmt.Errorf("persona/auditlog: rename to %s: %w", final, err)
	}
	cleanup = false
	return nil
}

// Get loads the record with the given ID. Returns
// [fs.ErrNotExist]-wrapped when no row matches (callers can use
// errors.Is to distinguish "absent" from "I/O failed").
//
// Get walks date directories newest-first and stops at the first
// match. For workloads where (id, date) is known up front, prefer
// GetOnDate to skip the walk.
func (l *AuditLog) Get(id string) (*corepersona.PersonaConsultRecord, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("persona/auditlog: id must not be empty")
	}
	consultsDir := filepath.Join(l.root, "consults")
	dates, err := readDateDirs(consultsDir)
	if err != nil {
		return nil, err
	}
	// Newest-first lookup: most reads are for recent consults.
	for i := len(dates) - 1; i >= 0; i-- {
		path := filepath.Join(consultsDir, dates[i], id+".json")
		rec, err := readRecord(path)
		if err == nil {
			return rec, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("persona/auditlog: id %q: %w", id, fs.ErrNotExist)
}

// GetOnDate is the fast path when the caller already knows the day
// the consult started. Saves a directory walk when the audit log has
// many partitions.
func (l *AuditLog) GetOnDate(id string, day time.Time) (*corepersona.PersonaConsultRecord, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("persona/auditlog: id must not be empty")
	}
	path := filepath.Join(l.dirFor(day), id+".json")
	return readRecord(path)
}

// ListFilter narrows what List returns. Zero-value filter returns
// every record across every partition, newest first.
type ListFilter struct {
	// Since, when non-zero, restricts the result to consults whose
	// StartedAt is on or after the truncated-to-day value. Use the
	// zero value to disable.
	Since time.Time

	// Until, when non-zero, restricts the result to consults whose
	// StartedAt is on or before the truncated-to-day value.
	Until time.Time

	// PersonaID, when non-empty, restricts to consults from that
	// persona.
	PersonaID string

	// SkillID, when non-empty, restricts to consults of that skill.
	SkillID string

	// Limit caps the number of records returned. Zero means no
	// limit. The newest matching records win when Limit < total.
	Limit int
}

// List walks the audit log and returns records matching f, newest
// first. The walk is bounded by f.Since/Until so /insights/personas
// can render "last 7 days" without paying for a full-store scan.
//
// Returns an empty slice (not an error) when no records match or
// the root dir does not exist yet.
func (l *AuditLog) List(f ListFilter) ([]corepersona.PersonaConsultRecord, error) {
	consultsDir := filepath.Join(l.root, "consults")
	dates, err := readDateDirs(consultsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	dates = filterDates(dates, f.Since, f.Until)
	// Walk newest-first so an early Limit cutoff drops only old rows.
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	out := make([]corepersona.PersonaConsultRecord, 0, len(dates))
	for _, d := range dates {
		dirPath := filepath.Join(consultsDir, d)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			return nil, fmt.Errorf("persona/auditlog: read %s: %w", dirPath, err)
		}
		// Reverse-sort within a day so newest-id-first when ids are
		// time-ordered (the dispatcher generates monotonic ids).
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			names = append(names, e.Name())
		}
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
		for _, name := range names {
			rec, err := readRecord(filepath.Join(dirPath, name))
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				return nil, err
			}
			if !matchesFilter(*rec, f) {
				continue
			}
			out = append(out, *rec)
			if f.Limit > 0 && len(out) >= f.Limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// dirFor returns the partition directory for a record started at t.
// UTC dates so consults round to the same day regardless of where
// the audit reader is running.
func (l *AuditLog) dirFor(t time.Time) string {
	return filepath.Join(l.root, "consults", t.UTC().Format("2006-01-02"))
}

// readRecord loads one record file. Wraps fs.ErrNotExist so callers
// can distinguish absent from corrupt.
func readRecord(path string) (*corepersona.PersonaConsultRecord, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec corepersona.PersonaConsultRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, fmt.Errorf("persona/auditlog: decode %s: %w", path, err)
	}
	return &rec, nil
}

// readDateDirs returns the names of partition subdirectories under
// dir. Names that don't parse as YYYY-MM-DD are skipped (a stray
// README would not break the listing). Returns ascending order.
func readDateDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := time.Parse("2006-01-02", e.Name()); err != nil {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// filterDates narrows partitions to the [since, until] inclusive
// window. Zero bounds disable that side of the filter.
func filterDates(dates []string, since, until time.Time) []string {
	if since.IsZero() && until.IsZero() {
		return dates
	}
	sinceStr := ""
	if !since.IsZero() {
		sinceStr = since.UTC().Format("2006-01-02")
	}
	untilStr := ""
	if !until.IsZero() {
		untilStr = until.UTC().Format("2006-01-02")
	}
	out := dates[:0:0]
	for _, d := range dates {
		if sinceStr != "" && d < sinceStr {
			continue
		}
		if untilStr != "" && d > untilStr {
			continue
		}
		out = append(out, d)
	}
	return out
}

// matchesFilter applies the persona/skill predicates. Date filtering
// happens at the directory layer; this only handles the per-record
// fields.
func matchesFilter(r corepersona.PersonaConsultRecord, f ListFilter) bool {
	if f.PersonaID != "" && r.PersonaID != f.PersonaID {
		return false
	}
	if f.SkillID != "" && r.SkillID != f.SkillID {
		return false
	}
	return true
}

// validateRecord enforces the invariants every audit row must
// satisfy. Empty IDs would collide on disk; missing StartedAt would
// not partition correctly; missing PersonaID/SkillID would break
// filter-by queries.
func validateRecord(r corepersona.PersonaConsultRecord) error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("persona/auditlog: record id must not be empty")
	}
	if r.StartedAt.IsZero() {
		return errors.New("persona/auditlog: record started_at must be set")
	}
	if strings.TrimSpace(r.PersonaID) == "" {
		return errors.New("persona/auditlog: record persona_id must not be empty")
	}
	if strings.TrimSpace(r.SkillID) == "" {
		return errors.New("persona/auditlog: record skill_id must not be empty")
	}
	if strings.ContainsAny(r.ID, `/\`) {
		return fmt.Errorf("persona/auditlog: record id %q must not contain path separators", r.ID)
	}
	return nil
}
