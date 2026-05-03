package dolt

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os/user"
	"strings"
	"time"

	// The adaptor opens MySQL connections to a Dolt server via
	// database/sql; the driver registers itself under the "mysql"
	// name on import.
	_ "github.com/go-sql-driver/mysql"

	"github.com/GembaCore/gemba-core/core"
)

// Config configures a direct-to-Dolt WorkPlane. URL is the only
// required field; everything else has operator-friendly defaults that
// match what gas-town ships on port 3307.
type Config struct {
	// URL is a mysql://user[:password]@host:port/dbname connection
	// string. The driver spoken underneath is go-sql-driver/mysql,
	// but we expose the URL form rather than the driver's DSN so
	// flag text stays readable (--dolt-url mysql://... vs the less
	// obvious user:pass@tcp(host:port)/db).
	URL string

	// Prefix is the <workspace>/<repo> chunk prepended to every
	// beads id when emitting a core.WorkItemID. Empty means
	// "use the package default," matching the bd adaptor.
	Prefix string

	// MaxOpenConns caps the pool's open connection count. 0 means
	// "use the adaptor default (8)" — low enough that a shared Dolt
	// server serving several gemba processes doesn't run the
	// connection table dry, high enough that a few concurrent board
	// reads don't serialize.
	MaxOpenConns int

	// ConnectTimeout bounds the startup ping that verifies the Dolt
	// server is reachable. 0 means "use the adaptor default (3s)".
	ConnectTimeout time.Duration

	// DescriptionFormat overrides the CapabilityManifest's declared
	// description content type. Empty → defaults to
	// core.DescriptionFormatMarkdown, matching the bd adaptor since the
	// underlying beads database stores markdown either way.
	DescriptionFormat string

	// ReadOnly forces mutation methods to fail with KindReadOnly. URL
	// mode is otherwise writable when the Dolt user has permission.
	ReadOnly bool
}

// defaultPrefix mirrors the bd adaptor so the two work-planes can
// point at the same beads database and emit identical WorkItemIDs.
const (
	defaultPrefix         = "gemba/gemba"
	defaultMaxOpenConns   = 8
	defaultConnectTimeout = 3 * time.Second
	adaptorName           = "beads-dolt"
	adaptorVersion        = "0.1.0"
)

// WorkPlane is the direct Dolt SQL implementation of core.WorkPlane.
// It opens a single pooled *sql.DB against the configured Dolt server.
// Mutations are SQL transactions unless Config.ReadOnly is set.
type WorkPlane struct {
	db                *sql.DB
	prefix            string
	dbName            string
	descriptionFormat string
	readOnly          bool
	emitter           *core.WorkPlaneEmitter
}

var _ core.WorkPlane = (*WorkPlane)(nil)

// NewWorkPlane dials the Dolt server named by cfg.URL, verifies the
// connection with a cheap ping, and returns a ready WorkPlane. A
// dial failure is wrapped as KindAdaptorDegraded so callers can
// distinguish "operator mis-configured the URL" (validation) from
// "server unreachable" (degraded / retry).
func NewWorkPlane(cfg Config) (*WorkPlane, error) {
	if cfg.URL == "" {
		return nil, core.NewAdaptorError(core.KindValidation,
			"dolt: --dolt-url is required")
	}
	dsn, dbName, err := parseDoltURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, core.WrapAdaptorError(core.KindValidation, err,
			"dolt: open %s", redact(cfg.URL))
	}

	max := cfg.MaxOpenConns
	if max <= 0 {
		max = defaultMaxOpenConns
	}
	db.SetMaxOpenConns(max)
	db.SetMaxIdleConns(max)
	db.SetConnMaxIdleTime(5 * time.Minute)

	timeout := cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = defaultConnectTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, wrapPingError(err, cfg.URL)
	}

	if err := verifySchema(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}

	prefix := cfg.Prefix
	if prefix == "" {
		prefix = defaultPrefix
	}
	format := cfg.DescriptionFormat
	if format == "" {
		format = core.DescriptionFormatMarkdown
	}
	return &WorkPlane{
		db:                db,
		prefix:            prefix,
		dbName:            dbName,
		descriptionFormat: format,
		readOnly:          cfg.ReadOnly,
		emitter:           core.NewWorkPlaneEmitter(),
	}, nil
}

// NewWorkPlaneFromDB is the constructor tests use to inject a
// pre-seeded *sql.DB (typically one fronted by go-sqlmock). It
// skips the URL parse and ping; callers take responsibility for
// driving the mock correctly.
func NewWorkPlaneFromDB(db *sql.DB, prefix, dbName string) *WorkPlane {
	if prefix == "" {
		prefix = defaultPrefix
	}
	return &WorkPlane{
		db:                db,
		prefix:            prefix,
		dbName:            dbName,
		descriptionFormat: core.DescriptionFormatMarkdown,
		emitter:           core.NewWorkPlaneEmitter(),
	}
}

// NewReadOnlyWorkPlaneFromDB mirrors NewWorkPlaneFromDB but marks the
// injected adaptor read-only for tests and explicit --beads-read-only mode.
func NewReadOnlyWorkPlaneFromDB(db *sql.DB, prefix, dbName string) *WorkPlane {
	wp := NewWorkPlaneFromDB(db, prefix, dbName)
	wp.readOnly = true
	return wp
}

// Close releases the underlying connection pool. Safe to call more
// than once; subsequent queries return KindAdaptorDegraded.
func (w *WorkPlane) Close() error {
	if w == nil || w.db == nil {
		return nil
	}
	return w.db.Close()
}

// DB returns the underlying *sql.DB so the registry-side health probe
// (registry.Probe via SetProbeDB) can ping the same pool the WorkPlane
// uses. Returning the live handle rather than dialing again means the
// probe surfaces real pool exhaustion / circuit-breaker state, not a
// fresh-connection coincidence.
func (w *WorkPlane) DB() *sql.DB {
	if w == nil {
		return nil
	}
	return w.db
}

// Describe returns the capability manifest for the Dolt adaptor. The
// rest mirrors the bd sibling so the SPA does not have to special-case
// beads when served via SQL.
func (w *WorkPlane) Describe(context.Context) (core.CapabilityManifest, error) {
	m := doltManifest
	m.DescriptionFormat = w.descriptionFormat
	m.ReadOnly = w.readOnly
	return m, nil
}

var doltManifest = core.CapabilityManifest{
	AdaptorName:     adaptorName,
	AdaptorVersion:  adaptorVersion,
	ProtocolVersion: core.ProtocolVersion,
	Transport:       core.TransportAPI,
	StateMap:        beadsStateMap,
	FieldExtensions: []core.FieldExtension{
		{Name: "beads:issue_type", Type: "string",
			Description: "Beads issue_type (task|feature|bug|decision|epic|chore|molecule|event)"},
		{Name: "beads:notes", Type: "markdown",
			Description: "Free-text notes field bd writes independently of description"},
		{Name: "beads:parent", Type: "string",
			Description: "Parent bead id — populated for hierarchical children"},
	},
	SprintNative:              false,
	TokenBudgetEnforced:       false,
	EvidenceSynthesisRequired: false,
	ReadOnly:                  false,
}

// beadsStateMap is intentionally duplicated from the bd adaptor so
// the two packages stay independently compilable even if one moves.
// The canonical list lives in bd's types.go; changes there must be
// mirrored here, and a Validate() at startup guards drift.
var beadsStateMap = core.StateMap{
	"open":        core.StateUnstarted,
	"in_progress": core.StateStarted,
	"hooked":      core.StateStarted,
	"pinned":      core.StateStarted,
	"blocked":     core.StateStarted,
	"deferred":    core.StateBacklog,
	"closed":      core.StateCompleted,
}

// listColumns is the column set selected from `issues` for
// ListWorkItems / GetWorkItem. Keeping it in one const prevents
// drift between the SELECT and the Scan target ordering in types.go.
const listColumns = "id, title, description, status, priority, issue_type, " +
	"assignee, owner, created_at, created_by, updated_at, notes"

// ListWorkItems runs a single SELECT against the beads `issues`
// table and applies the remaining filter predicates client-side.
// Filters we can push down (status, assignee, limit, updated_since)
// go into the WHERE clause so Dolt does the selective work; the
// rest (multi-status intersections, state-category sets, label
// joins) ride through matchesFilter after the rows come back.
func (w *WorkPlane) ListWorkItems(ctx context.Context, f core.WorkItemFilter) ([]core.WorkItem, error) {
	var (
		clauses []string
		args    []any
	)
	if len(f.Statuses) == 1 {
		clauses = append(clauses, "status = ?")
		args = append(args, f.Statuses[0])
	}
	if f.AssigneeID != nil && *f.AssigneeID != "" {
		clauses = append(clauses, "assignee = ?")
		args = append(args, string(*f.AssigneeID))
	}
	if f.UpdatedSince != nil {
		clauses = append(clauses, "updated_at >= ?")
		args = append(args, f.UpdatedSince.UTC())
	}
	if f.CreatedSince != nil {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, f.CreatedSince.UTC())
	}
	query := "SELECT " + listColumns + " FROM issues"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := w.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapQueryError(err, "issues")
	}
	defer rows.Close()

	var beads []*issueRow
	for rows.Next() {
		r, err := scanIssueRow(rows)
		if err != nil {
			return nil, err
		}
		beads = append(beads, r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapQueryError(err, "issues")
	}

	// Hydrate labels + dependencies for the rows we actually plan to
	// return; this saves a join on the selective-filter path.
	if err := w.hydrate(ctx, beads); err != nil {
		return nil, err
	}

	out := make([]core.WorkItem, 0, len(beads))
	for _, r := range beads {
		wi := r.toWorkItem(w.prefix)
		if !matchesFilter(wi, f) {
			continue
		}
		out = append(out, wi)
	}
	return out, nil
}

// GetWorkItem fetches a single row by its bare bead id (the prefix
// is stripped via nativeID). Unknown ids return KindSessionNotFound
// so callers using errors.Is(err, core.ErrNotFound) keep working.
func (w *WorkPlane) GetWorkItem(ctx context.Context, id core.WorkItemID) (core.WorkItem, error) {
	native := nativeID(w.prefix, id)
	query := "SELECT " + listColumns + " FROM issues WHERE id = ? LIMIT 1"
	rows, err := w.db.QueryContext(ctx, query, native)
	if err != nil {
		return core.WorkItem{}, wrapQueryError(err, "issues")
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return core.WorkItem{}, wrapQueryError(err, "issues")
		}
		return core.WorkItem{}, core.NewAdaptorError(core.KindSessionNotFound,
			"dolt: bead %q not found", native)
	}
	r, err := scanIssueRow(rows)
	if err != nil {
		return core.WorkItem{}, err
	}
	// Drain so the conn returns to the pool cleanly.
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		return core.WorkItem{}, wrapQueryError(err, "issues")
	}
	if err := w.hydrate(ctx, []*issueRow{r}); err != nil {
		return core.WorkItem{}, err
	}
	return r.toWorkItem(w.prefix), nil
}

// CreateWorkItem inserts a bead through the Dolt SQL surface and returns
// the persisted row. It follows the same milestone, staged, parent, DoD,
// and agent-label conventions as the bd CLI adaptor.
func (w *WorkPlane) CreateWorkItem(ctx context.Context, wi core.WorkItem) (core.WorkItem, error) {
	if w.readOnly {
		return core.WorkItem{}, readOnlyError("CreateWorkItem")
	}
	if strings.TrimSpace(wi.Title) == "" {
		return core.WorkItem{}, core.NewAdaptorError(core.KindValidation,
			"dolt: CreateWorkItem requires title")
	}
	id, err := w.nextID(ctx)
	if err != nil {
		return core.WorkItem{}, err
	}
	status := wi.Status
	if status == "" {
		if wi.StateCategory != "" {
			var ok bool
			status, ok = doltStatusForCategory(wi.StateCategory)
			if !ok {
				return core.WorkItem{}, core.NewAdaptorError(core.KindValidation,
					"dolt: state_category %q has no beads status", wi.StateCategory)
			}
		} else {
			status = "open"
		}
	}
	kind := wi.Kind
	addMilestoneLabel := false
	if kind == core.KindMilestone {
		kind = "epic"
		addMilestoneLabel = true
	}
	if kind == "" {
		kind = "task"
	}
	priority := 2
	if wi.Priority != nil {
		priority = *wi.Priority
	}
	labels := stripDoltAgentLabels(wi.Labels)
	if wi.Assignee != nil {
		labels = append(labels, doltAgentLabels(wi.Assignee)...)
	}
	if addMilestoneLabel && !hasDoltLabel(labels, milestoneLabel) {
		labels = append(labels, milestoneLabel)
	}
	if wi.StateCategory == core.StateStaged && !hasDoltLabel(labels, stagedLabel) {
		labels = append(labels, stagedLabel)
	}
	assignee := sql.NullString{}
	if wi.Assignee != nil && wi.Assignee.ID != "" {
		assignee = sql.NullString{String: string(wi.Assignee.ID), Valid: true}
	}
	owner := sql.NullString{}
	if wi.Owner != nil && wi.Owner.ID != "" {
		owner = sql.NullString{String: string(wi.Owner.ID), Valid: true}
	}
	now := time.Now().UTC()
	actor := currentActor()
	desc := embedDoltDoD(wi.Description, wi.DoD)

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return core.WorkItem{}, wrapQueryError(err, "issues")
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO issues (id, title, description, design, acceptance_criteria, notes, status, priority, issue_type, assignee, owner, created_at, created_by, updated_at) VALUES (?, ?, ?, '', '', '', ?, ?, ?, ?, ?, ?, ?, ?)",
		id, wi.Title, desc, status, priority, string(kind), assignee, owner, now, actor, now,
	); err != nil {
		return core.WorkItem{}, wrapQueryError(err, "issues")
	}
	if err := replaceLabels(ctx, tx, id, labels); err != nil {
		return core.WorkItem{}, err
	}
	if parent := parentOnDoltCreate(wi.Relationships); parent != "" {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO dependencies (issue_id, depends_on_id, type, created_by) VALUES (?, ?, ?, ?)",
			id, nativeID(w.prefix, parent), "parent-child", actor,
		); err != nil {
			return core.WorkItem{}, wrapQueryError(err, "dependencies")
		}
	}
	if err := tx.Commit(); err != nil {
		return core.WorkItem{}, wrapQueryError(err, "issues")
	}
	out, err := w.GetWorkItem(ctx, buildWorkItemID(w.prefix, id))
	if err != nil {
		return core.WorkItem{}, err
	}
	w.emit(core.WorkItemEventCreated, out)
	return out, nil
}

// UpdateWorkItem applies a WorkItemPatch via SQL and re-reads the row
// so callers see persisted state.
func (w *WorkPlane) UpdateWorkItem(ctx context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
	if w.readOnly {
		return core.WorkItem{}, readOnlyError("UpdateWorkItem")
	}
	native := nativeID(w.prefix, id)
	var (
		sets []string
		args []any
	)
	labels := append([]string(nil), patch.Labels...)
	shouldReplaceLabels := len(labels) > 0
	addStagedLabel := false
	removeStagedLabel := false

	if patch.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *patch.Title)
	}
	if patch.Description != nil && patch.DoD != nil {
		sets = append(sets, "description = ?")
		args = append(args, embedDoltDoD(*patch.Description, patch.DoD))
	} else if patch.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *patch.Description)
	} else if patch.DoD != nil {
		current, err := w.GetWorkItem(ctx, id)
		if err != nil {
			return core.WorkItem{}, err
		}
		sets = append(sets, "description = ?")
		args = append(args, embedDoltDoD(current.Description, patch.DoD))
	}
	if patch.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *patch.Status)
	} else if patch.StateCategory != nil {
		status, ok := doltStatusForCategory(*patch.StateCategory)
		if !ok {
			return core.WorkItem{}, core.NewAdaptorError(core.KindValidation,
				"dolt: state_category %q has no beads status; send an explicit status instead",
				*patch.StateCategory)
		}
		sets = append(sets, "status = ?")
		args = append(args, status)
		switch *patch.StateCategory {
		case core.StateStaged:
			if shouldReplaceLabels {
				labels = setDoltStagedLabel(labels, true)
			} else {
				current, err := w.GetWorkItem(ctx, id)
				if err != nil {
					return core.WorkItem{}, err
				}
				addStagedLabel = !hasDoltLabel(current.Labels, stagedLabel)
			}
		default:
			if shouldReplaceLabels {
				labels = setDoltStagedLabel(labels, false)
			} else {
				current, err := w.GetWorkItem(ctx, id)
				if err != nil {
					return core.WorkItem{}, err
				}
				removeStagedLabel = hasDoltLabel(current.Labels, stagedLabel)
			}
		}
	}
	if patch.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *patch.Priority)
	}
	if patch.Owner != nil {
		sets = append(sets, "owner = ?")
		args = append(args, string(patch.Owner.ID))
	}
	if patch.Assignee != nil {
		sets = append(sets, "assignee = ?")
		args = append(args, string(patch.Assignee.ID))
	}
	agentExtra := doltAgentLabels(patch.Assignee)
	if shouldReplaceLabels {
		labels = stripDoltAgentLabels(labels)
		labels = append(labels, agentExtra...)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC())

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return core.WorkItem{}, wrapQueryError(err, "issues")
	}
	defer func() { _ = tx.Rollback() }()
	if len(sets) > 0 {
		args = append(args, native)
		res, err := tx.ExecContext(ctx,
			"UPDATE issues SET "+strings.Join(sets, ", ")+" WHERE id = ?",
			args...,
		)
		if err != nil {
			return core.WorkItem{}, wrapQueryError(err, "issues")
		}
		if rows, err := res.RowsAffected(); err == nil && rows == 0 {
			return core.WorkItem{}, core.NewAdaptorError(core.KindSessionNotFound,
				"dolt: bead %q not found", native)
		}
	}
	if shouldReplaceLabels {
		if err := replaceLabels(ctx, tx, native, labels); err != nil {
			return core.WorkItem{}, err
		}
	} else {
		for _, l := range agentExtra {
			if err := insertLabel(ctx, tx, native, l); err != nil {
				return core.WorkItem{}, err
			}
		}
		if addStagedLabel {
			if err := insertLabel(ctx, tx, native, stagedLabel); err != nil {
				return core.WorkItem{}, err
			}
		}
		if removeStagedLabel {
			if _, err := tx.ExecContext(ctx,
				"DELETE FROM labels WHERE issue_id = ? AND label = ?",
				native, stagedLabel,
			); err != nil {
				return core.WorkItem{}, wrapQueryError(err, "labels")
			}
		}
	}
	if patch.Parent != nil {
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM dependencies WHERE issue_id = ? AND type IN ('parent-child', 'parent_child')",
			native,
		); err != nil {
			return core.WorkItem{}, wrapQueryError(err, "dependencies")
		}
		if *patch.Parent != "" {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO dependencies (issue_id, depends_on_id, type, created_by) VALUES (?, ?, ?, ?)",
				native, nativeID(w.prefix, core.WorkItemID(*patch.Parent)), "parent-child", currentActor(),
			); err != nil {
				return core.WorkItem{}, wrapQueryError(err, "dependencies")
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return core.WorkItem{}, wrapQueryError(err, "issues")
	}
	out, err := w.GetWorkItem(ctx, id)
	if err != nil {
		return core.WorkItem{}, err
	}
	kind := core.WorkItemEventUpdated
	if out.StateCategory == core.StateCompleted || out.StateCategory == core.StateCanceled {
		kind = core.WorkItemEventClosed
	}
	w.emit(kind, out)
	return out, nil
}

// AttachEvidence remains a capability-denied operation: Beads evidence is
// synthesized from labels/transport artifacts, matching the bd adaptor.
func (w *WorkPlane) AttachEvidence(context.Context, core.WorkItemID, core.Evidence) error {
	if w.readOnly {
		return readOnlyError("AttachEvidence")
	}
	return core.EnforceCapability(doltManifest, core.OpAttachEvidence)
}

// DeleteWorkItem permanently deletes a bead and its edge/label rows.
func (w *WorkPlane) DeleteWorkItem(ctx context.Context, id core.WorkItemID) (core.WorkItem, error) {
	if w.readOnly {
		return core.WorkItem{}, readOnlyError("DeleteWorkItem")
	}
	native := nativeID(w.prefix, id)
	before, err := w.GetWorkItem(ctx, id)
	if err != nil {
		return core.WorkItem{}, err
	}
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return core.WorkItem{}, wrapQueryError(err, "issues")
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		"DELETE FROM labels WHERE issue_id = ?",
		"DELETE FROM dependencies WHERE issue_id = ? OR depends_on_id = ?",
		"DELETE FROM issues WHERE id = ?",
	} {
		var args []any
		if strings.Contains(stmt, "depends_on_id") {
			args = []any{native, native}
		} else {
			args = []any{native}
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return core.WorkItem{}, wrapQueryError(err, "issues")
		}
	}
	if err := tx.Commit(); err != nil {
		return core.WorkItem{}, wrapQueryError(err, "issues")
	}
	w.emit(core.WorkItemEventClosed, before)
	return before, nil
}

// ListSprints: beads has no native sprint concept, so this mirrors
// the bd adaptor and returns KindUnsupported. The manifest's
// SprintNative=false already hides the sprint chrome.
func (w *WorkPlane) ListSprints(context.Context) ([]core.Sprint, error) {
	return nil, core.EnforceCapability(doltManifest, core.OpListSprints)
}

// ReadBudgetRollup: same as ListSprints — capability_denied because
// SprintNative + TokenBudgetEnforced are both false.
func (w *WorkPlane) ReadBudgetRollup(context.Context, string) (core.BudgetRollup, error) {
	return core.BudgetRollup{}, core.EnforceCapability(doltManifest, core.OpReadBudgetRollup)
}

// Subscribe streams events for mutations performed through this adaptor
// instance. External SQL writers still require polling/refresh.
func (w *WorkPlane) Subscribe(ctx context.Context, f core.WorkPlaneSubscribeFilter) (<-chan core.WorkPlaneEvent, error) {
	return w.emitter.Subscribe(ctx, f), nil
}

// readOnlyError builds the KindReadOnly envelope the three write
// methods share.
func readOnlyError(op string) error {
	return core.NewAdaptorError(core.KindReadOnly,
		"dolt: %s is not available in --beads-read-only mode", op)
}

func (w *WorkPlane) nextID(ctx context.Context) (string, error) {
	for range 16 {
		var buf [3]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return "", core.WrapAdaptorError(core.KindProcessFailed, err,
				"dolt: generate bead id")
		}
		id := "gm-" + hex.EncodeToString(buf[:])
		var exists int
		if err := w.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM issues WHERE id = ?", id,
		).Scan(&exists); err != nil {
			return "", wrapQueryError(err, "issues")
		}
		if exists == 0 {
			return id, nil
		}
	}
	return "", core.NewAdaptorError(core.KindProcessFailed,
		"dolt: could not allocate a unique bead id")
}

func (w *WorkPlane) emit(kind string, wi core.WorkItem) {
	w.emitter.Publish(core.WorkPlaneEvent{
		ID:         fmt.Sprintf("%s:%s:%d", kind, wi.ID, time.Now().UnixNano()),
		Kind:       kind,
		At:         time.Now().UTC(),
		WorkItemID: wi.ID,
		Payload: map[string]any{
			"item": wi,
		},
	})
}

func replaceLabels(ctx context.Context, tx *sql.Tx, issueID string, labels []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM labels WHERE issue_id = ?", issueID); err != nil {
		return wrapQueryError(err, "labels")
	}
	for _, l := range labels {
		if err := insertLabel(ctx, tx, issueID, l); err != nil {
			return err
		}
	}
	return nil
}

func insertLabel(ctx context.Context, tx *sql.Tx, issueID, label string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT IGNORE INTO labels (issue_id, label) VALUES (?, ?)",
		issueID, label,
	); err != nil {
		return wrapQueryError(err, "labels")
	}
	return nil
}

func currentActor() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "gemba"
}

// parseDoltURL converts the user-facing mysql:// URL into the DSN
// go-sql-driver/mysql expects, plus the dbname portion for error
// messages. We accept either "mysql://" or plain "//" so an
// operator used to JDBC-style strings isn't surprised; the tcp()
// network form is implied (unix-socket support can come later).
func parseDoltURL(raw string) (dsn, dbName string, err error) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "mysql://") && !strings.HasPrefix(trimmed, "mysql+tcp://") {
		return "", "", core.NewAdaptorError(core.KindValidation,
			"dolt: --dolt-url must start with mysql://; got %q", redact(raw))
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", "", core.WrapAdaptorError(core.KindValidation, err,
			"dolt: parse --dolt-url %q", redact(raw))
	}
	if u.Host == "" {
		return "", "", core.NewAdaptorError(core.KindValidation,
			"dolt: --dolt-url missing host:port (got %q)", redact(raw))
	}
	dbName = strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return "", "", core.NewAdaptorError(core.KindValidation,
			"dolt: --dolt-url missing database name (expected mysql://host:port/dbname)")
	}
	user := "root"
	pass := ""
	hasPass := false
	if u.User != nil {
		if u.User.Username() != "" {
			user = u.User.Username()
		}
		pass, hasPass = u.User.Password()
	}
	// go-sql-driver/mysql DSN: user:pass@tcp(host)/dbname?params
	auth := user
	if hasPass {
		auth = user + ":" + pass
	}
	params := "parseTime=true&loc=UTC&readTimeout=30s&writeTimeout=30s"
	if q := u.RawQuery; q != "" {
		params = q + "&" + params
	}
	dsn = fmt.Sprintf("%s@tcp(%s)/%s?%s", auth, u.Host, dbName, params)
	return dsn, dbName, nil
}

// redact strips the password from a mysql://user:pass@host URL for
// log-safe error messages. Non-mysql URLs pass through unchanged;
// parse errors also pass through because a malformed URL already
// can't leak a structured password.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, ok := u.User.Password(); !ok {
		return raw
	}
	u.User = url.User(u.User.Username())
	return u.String()
}

// wrapPingError translates a connect-time failure into the most
// actionable error kind we can justify. Deadline exceeded and the
// MySQL driver's "connection refused" both land as AdaptorDegraded
// (retry after operator fixes the server). "Access denied" and
// "Unknown database" are fed back as Validation because the
// operator's URL is wrong and no retry will help.
func wrapPingError(err error, rawURL string) error {
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"dolt: ping timed out against %s (is the Dolt server running?)", redact(rawURL))
	case strings.Contains(lower, "access denied"):
		return core.WrapAdaptorError(core.KindValidation, err,
			"dolt: access denied for %s (check user/password)", redact(rawURL))
	case strings.Contains(lower, "unknown database"):
		return core.WrapAdaptorError(core.KindValidation, err,
			"dolt: unknown database in %s", redact(rawURL))
	case strings.Contains(lower, "connection refused"):
		return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"dolt: connection refused at %s", redact(rawURL))
	default:
		return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"dolt: ping %s failed", redact(rawURL))
	}
}

// wrapQueryError wraps a mid-request database failure as
// AdaptorDegraded — the most common cause is Dolt being restarted
// mid-session, which the banner surfaces verbatim.
func wrapQueryError(err error, table string) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "table") && strings.Contains(lower, "not") {
		return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"dolt: beads schema missing table %q (version skew?)", table)
	}
	return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
		"dolt: query %s failed", table)
}

// matchesFilter applies the predicates Dolt didn't execute
// server-side. Identical in spirit to the bd adaptor's
// in-process filter; kept here so the two packages are independent.
func matchesFilter(wi core.WorkItem, f core.WorkItemFilter) bool {
	if len(f.IDs) > 0 {
		hit := false
		for _, id := range f.IDs {
			if id == wi.ID {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(f.Kinds) > 0 {
		hit := false
		for _, k := range f.Kinds {
			if k == wi.Kind {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(f.Statuses) > 1 {
		hit := false
		for _, s := range f.Statuses {
			if s == wi.Status {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(f.StateCategory) > 0 {
		hit := false
		for _, c := range f.StateCategory {
			if c == wi.StateCategory {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(f.Labels) > 0 {
		have := map[string]struct{}{}
		for _, l := range wi.Labels {
			have[l] = struct{}{}
		}
		for _, want := range f.Labels {
			if _, ok := have[want]; !ok {
				return false
			}
		}
	}
	// gm-e12.22.1: workflow-template / wisp filter, mirrors bd adaptor.
	// Default-hide; opt-in via IncludeTemplates / IncludeWisps.
	if !f.IncludeTemplates && hasTemplateLabel(wi) {
		return false
	}
	if !f.IncludeWisps && strings.Contains(string(wi.ID), "wisp") {
		return false
	}
	return true
}

// hasTemplateLabel — duplicated from the bd adaptor on purpose. Both
// packages keep their own copy so the two stay independent (the
// shader package's isWisp() also lives standalone for the same
// reason). Dolt-direct paths share the convention. gm-e12.22.1.
func hasTemplateLabel(wi core.WorkItem) bool {
	for _, l := range wi.Labels {
		if l == "template" {
			return true
		}
	}
	return false
}
