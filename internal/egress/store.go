package egress

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrRuleNotFound is returned when a (wsid, id) tuple is not present
// in the workspace's rule set.
var ErrRuleNotFound = errors.New("egress: rule not found")

// ErrDefaultImmutable is returned by Put / Delete when the caller
// targets one of the hard-coded baseline rules. Defaults can be
// shadowed via a higher-priority workspace rule but never deleted or
// edited in place.
var ErrDefaultImmutable = errors.New("egress: default rules are immutable")

// Store is the persistence contract for egress rules.
//
// List returns only the rules the workspace owns (no defaults).
// Effective() returns the merged + priority-sorted view used by the
// runtime enforcer (3.6.2) and the operator-facing preview endpoint.
type Store interface {
	List(ctx context.Context, wsid string) ([]Rule, error)
	Put(ctx context.Context, r Rule) error
	Delete(ctx context.Context, wsid, id string) error
	Effective(ctx context.Context, wsid string) ([]Rule, error)
}

// effectiveFromList sorts (workspace ∪ defaults) by Priority desc,
// stable for ties. Shared by Mem and SQL stores so behavior is
// identical across backends.
func effectiveFromList(workspace []Rule) []Rule {
	defs := Defaults()
	out := make([]Rule, 0, len(workspace)+len(defs))
	out = append(out, workspace...)
	out = append(out, defs...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Priority > out[j].Priority
	})
	return out
}

// --- in-memory store -------------------------------------------------

// MemStore is a process-local Store used by tests and by the
// single-user fallback when a Dolt pool isn't wired.
type MemStore struct {
	mu   sync.RWMutex
	byWS map[string]map[string]Rule // wsid -> id -> rule
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{byWS: map[string]map[string]Rule{}}
}

// List implements Store.
func (m *MemStore) List(_ context.Context, wsid string) ([]Rule, error) {
	if wsid == "" {
		return nil, errors.New("egress: wsid required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	bucket := m.byWS[wsid]
	out := make([]Rule, 0, len(bucket))
	for _, r := range bucket {
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Put implements Store. Rejects attempts to overwrite a default rule
// id (those live outside the table).
func (m *MemStore) Put(_ context.Context, r Rule) error {
	if r.WorkspaceID == "" {
		return errors.New("egress: WorkspaceID required")
	}
	if r.ID == "" {
		return errors.New("egress: ID required")
	}
	if IsDefaultID(r.ID) {
		return ErrDefaultImmutable
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byWS[r.WorkspaceID] == nil {
		m.byWS[r.WorkspaceID] = map[string]Rule{}
	}
	m.byWS[r.WorkspaceID][r.ID] = r
	return nil
}

// Delete implements Store.
func (m *MemStore) Delete(_ context.Context, wsid, id string) error {
	if wsid == "" || id == "" {
		return errors.New("egress: wsid and id required")
	}
	if IsDefaultID(id) {
		return ErrDefaultImmutable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket := m.byWS[wsid]
	if _, ok := bucket[id]; !ok {
		return ErrRuleNotFound
	}
	delete(bucket, id)
	return nil
}

// Effective implements Store.
func (m *MemStore) Effective(ctx context.Context, wsid string) ([]Rule, error) {
	rules, err := m.List(ctx, wsid)
	if err != nil {
		return nil, err
	}
	return effectiveFromList(rules), nil
}

// --- SQL store -------------------------------------------------------

// SQLStore is the production Store backed by the same Dolt SQL pool
// the WorkPlane adaptor uses. Schema: see createEgressRulesTable.
type SQLStore struct{ db *sql.DB }

// NewSQLStore wraps db. The pool's lifecycle is the caller's.
func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }

// createEgressRulesTable is the gemba-owned migration. Idempotent so
// callers can run it on every boot without a separate migration
// runner.
//
// Columns mirror Rule; priority is signed so the default-deny
// sentinel can carry a large negative value without colliding with
// operator-set priorities.
const createEgressRulesTable = `CREATE TABLE IF NOT EXISTS egress_rules (
	id           VARCHAR(64)  NOT NULL,
	workspace_id VARCHAR(128) NOT NULL,
	action       ENUM('allow','deny') NOT NULL,
	proto        ENUM('tcp','udp','any') NOT NULL DEFAULT 'tcp',
	host_pattern VARCHAR(255) NOT NULL,
	port_start   INT          NOT NULL DEFAULT 0,
	port_end     INT          NOT NULL DEFAULT 0,
	priority     INT          NOT NULL DEFAULT 0,
	created_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
	created_by   VARCHAR(128) DEFAULT NULL,
	PRIMARY KEY (workspace_id, id),
	KEY idx_egress_rules_wsid (workspace_id)
)`

// Migrate ensures the egress_rules table exists. Safe to call on
// every server boot.
func (s *SQLStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, createEgressRulesTable); err != nil {
		return fmt.Errorf("egress: create egress_rules table: %w", err)
	}
	return nil
}

// List implements Store.
func (s *SQLStore) List(ctx context.Context, wsid string) ([]Rule, error) {
	if wsid == "" {
		return nil, errors.New("egress: wsid required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workspace_id, action, proto, host_pattern, port_start, port_end, priority, created_at, created_by
		 FROM egress_rules WHERE workspace_id = ?
		 ORDER BY priority DESC, id ASC`, wsid)
	if err != nil {
		return nil, fmt.Errorf("egress: list: %w", err)
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Put implements Store as a MySQL-flavored upsert.
func (s *SQLStore) Put(ctx context.Context, r Rule) error {
	if r.WorkspaceID == "" {
		return errors.New("egress: WorkspaceID required")
	}
	if r.ID == "" {
		return errors.New("egress: ID required")
	}
	if IsDefaultID(r.ID) {
		return ErrDefaultImmutable
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO egress_rules
		   (id, workspace_id, action, proto, host_pattern, port_start, port_end, priority, created_at, created_by)
		 VALUES (?,?,?,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE
		   action=VALUES(action), proto=VALUES(proto), host_pattern=VALUES(host_pattern),
		   port_start=VALUES(port_start), port_end=VALUES(port_end), priority=VALUES(priority),
		   created_by=VALUES(created_by)`,
		r.ID, r.WorkspaceID, string(r.Action), string(r.Proto),
		r.HostPattern, r.PortStart, r.PortEnd, r.Priority,
		r.CreatedAt, nullableString(r.CreatedBy))
	if err != nil {
		return fmt.Errorf("egress: put %s/%s: %w", r.WorkspaceID, r.ID, err)
	}
	return nil
}

// Delete implements Store.
func (s *SQLStore) Delete(ctx context.Context, wsid, id string) error {
	if wsid == "" || id == "" {
		return errors.New("egress: wsid and id required")
	}
	if IsDefaultID(id) {
		return ErrDefaultImmutable
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM egress_rules WHERE workspace_id = ? AND id = ?`, wsid, id)
	if err != nil {
		return fmt.Errorf("egress: delete %s/%s: %w", wsid, id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// Effective implements Store.
func (s *SQLStore) Effective(ctx context.Context, wsid string) ([]Rule, error) {
	rules, err := s.List(ctx, wsid)
	if err != nil {
		return nil, err
	}
	return effectiveFromList(rules), nil
}

// scanner abstracts *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func scanRule(r scanner) (Rule, error) {
	var (
		rule    Rule
		action  string
		proto   string
		created sql.NullString
	)
	if err := r.Scan(
		&rule.ID, &rule.WorkspaceID, &action, &proto,
		&rule.HostPattern, &rule.PortStart, &rule.PortEnd, &rule.Priority,
		&rule.CreatedAt, &created,
	); err != nil {
		return Rule{}, fmt.Errorf("egress: scan: %w", err)
	}
	rule.Action = Action(action)
	rule.Proto = Proto(proto)
	if created.Valid {
		rule.CreatedBy = created.String
	}
	return rule, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
