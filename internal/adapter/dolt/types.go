package dolt

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// issueRow is the projection of the beads `issues` table we scan
// into. Field order MUST match the listColumns const in
// workplane.go — the scanIssueRow helper pairs the two.
type issueRow struct {
	ID           string
	Title        string
	Description  sql.NullString
	Status       string
	Priority     int
	IssueType    string
	Assignee     sql.NullString
	Owner        sql.NullString
	CreatedAt    time.Time
	CreatedBy    sql.NullString
	UpdatedAt    time.Time
	Notes        sql.NullString
	Labels       []string
	Dependencies []edgeRow
	Dependents   []edgeRow
}

// edgeRow is one row out of the `dependencies` table, projected
// onto the direction its row represents (upstream vs downstream).
// The bd adaptor carries richer BeadEdge metadata; this adaptor
// keeps only what toWorkItem needs and what the SPA extension can
// render without another round trip.
type edgeRow struct {
	Linked string
	Type   string
}

// scanIssueRow scans a single row out of a SELECT that ordered its
// columns per listColumns. Returns a KindAdaptorDegraded error on
// scan failure — scan failures at this level usually mean a schema
// drift or a broken connection, both operator-visible problems.
func scanIssueRow(rows *sql.Rows) (*issueRow, error) {
	var r issueRow
	if err := rows.Scan(
		&r.ID, &r.Title, &r.Description, &r.Status, &r.Priority,
		&r.IssueType, &r.Assignee, &r.Owner, &r.CreatedAt, &r.CreatedBy,
		&r.UpdatedAt, &r.Notes,
	); err != nil {
		return nil, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"dolt: scan issues row")
	}
	return &r, nil
}

// hydrate fills in Labels and Dependencies/Dependents for each row
// in rs with two bulk queries (one per join table). Zero-row input
// is a nil-op so the call site does not need to guard against it.
//
// The bulk pattern matters when listing many beads: a per-row query
// would make ListWorkItems quadratic; two WHERE issue_id IN (...)
// queries keep the round-trip count at O(1) regardless of list
// size.
func (w *WorkPlane) hydrate(ctx context.Context, rs []*issueRow) error {
	if len(rs) == 0 {
		return nil
	}
	byID := make(map[string]*issueRow, len(rs))
	placeholders := make([]string, 0, len(rs))
	args := make([]any, 0, len(rs))
	for _, r := range rs {
		byID[r.ID] = r
		placeholders = append(placeholders, "?")
		args = append(args, r.ID)
	}
	inClause := strings.Join(placeholders, ",")

	if err := w.hydrateLabels(ctx, byID, inClause, args); err != nil {
		return err
	}
	if err := w.hydrateDependencies(ctx, byID, inClause, args); err != nil {
		return err
	}
	return nil
}

func (w *WorkPlane) hydrateLabels(
	ctx context.Context, byID map[string]*issueRow, inClause string, args []any,
) error {
	query := "SELECT issue_id, label FROM labels WHERE issue_id IN (" + inClause + ")"
	rows, err := w.db.QueryContext(ctx, query, args...)
	if err != nil {
		return wrapQueryError(err, "labels")
	}
	defer rows.Close()
	for rows.Next() {
		var issueID, label string
		if err := rows.Scan(&issueID, &label); err != nil {
			return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
				"dolt: scan labels row")
		}
		if r, ok := byID[issueID]; ok {
			r.Labels = append(r.Labels, label)
		}
	}
	return rows.Err()
}

func (w *WorkPlane) hydrateDependencies(
	ctx context.Context, byID map[string]*issueRow, inClause string, args []any,
) error {
	// Upstream (this bead depends on something) is a row with
	// issue_id = self. Downstream (something depends on this bead)
	// is a row with depends_on_id = self. One table, two queries —
	// cheaper than a UNION + CASE and keeps the Scan targets
	// uniform.
	upstream := "SELECT issue_id, depends_on_id, type FROM dependencies " +
		"WHERE issue_id IN (" + inClause + ")"
	rows, err := w.db.QueryContext(ctx, upstream, args...)
	if err != nil {
		return wrapQueryError(err, "dependencies")
	}
	defer rows.Close()
	for rows.Next() {
		var self, other, edgeType string
		if err := rows.Scan(&self, &other, &edgeType); err != nil {
			return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
				"dolt: scan dependencies row")
		}
		if r, ok := byID[self]; ok {
			r.Dependencies = append(r.Dependencies, edgeRow{Linked: other, Type: edgeType})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	downstream := "SELECT issue_id, depends_on_id, type FROM dependencies " +
		"WHERE depends_on_id IN (" + inClause + ")"
	drows, err := w.db.QueryContext(ctx, downstream, args...)
	if err != nil {
		return wrapQueryError(err, "dependencies")
	}
	defer drows.Close()
	for drows.Next() {
		var other, self, edgeType string
		if err := drows.Scan(&other, &self, &edgeType); err != nil {
			return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
				"dolt: scan dependencies row")
		}
		if r, ok := byID[self]; ok {
			r.Dependents = append(r.Dependents, edgeRow{Linked: other, Type: edgeType})
		}
	}
	return drows.Err()
}

// toWorkItem projects an issueRow onto the adaptor-agnostic
// core.WorkItem shape, matching what the bd adaptor emits for the
// same row. Fields the SQL path cannot cheaply reconstruct
// (beads:started_at, per-dependency metadata beyond type) are
// omitted rather than faked; the bd CLI path remains the richer
// view and the SPA already gates on FieldExtensions.
func (r *issueRow) toWorkItem(prefix string) core.WorkItem {
	category, ok := beadsStateMap[r.Status]
	if !ok {
		category = core.StateBacklog
	}
	if category == core.StateUnstarted && hasDoltLabel(r.Labels, stagedLabel) {
		category = core.StateStaged
	}
	priority := r.Priority
	custom := map[string]any{}
	if r.IssueType != "" {
		custom["beads:issue_type"] = r.IssueType
	}
	if r.Notes.Valid && r.Notes.String != "" {
		custom["beads:notes"] = r.Notes.String
	}
	if r.CreatedBy.Valid && r.CreatedBy.String != "" {
		custom["beads:created_by"] = r.CreatedBy.String
	}
	if len(r.Dependencies) > 0 {
		custom["beads:dependencies"] = edgeRowsToJSON(r.Dependencies, "issue_id", r.ID)
	}
	if len(r.Dependents) > 0 {
		custom["beads:dependents"] = edgeRowsToJSON(r.Dependents, "depends_on_id", r.ID)
	}
	id := buildWorkItemID(prefix, r.ID)
	descClean, dod := extractDoltDoD(nullString(r.Description))
	kind := r.IssueType
	if hasDoltLabel(r.Labels, milestoneLabel) {
		kind = core.KindMilestone
	}
	wi := core.WorkItem{
		ID:            id,
		Kind:          kind,
		Title:         r.Title,
		Description:   descClean,
		Status:        r.Status,
		StateCategory: category,
		Priority:      &priority,
		Labels:        append([]string(nil), r.Labels...),
		Relationships: relationshipsFromRows(r, id, prefix),
		DoD:           dod,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
		Custom:        custom,
	}
	if r.Owner.Valid && r.Owner.String != "" {
		wi.Owner = &core.AgentRef{
			ID:   core.AgentID(r.Owner.String),
			Name: r.Owner.String,
			Kind: core.AgentKindHuman,
		}
	}
	if r.Assignee.Valid && r.Assignee.String != "" {
		wi.Assignee = &core.AgentRef{
			ID:   core.AgentID(r.Assignee.String),
			Name: r.Assignee.String,
			Kind: core.AgentKindAgent,
		}
		for _, l := range r.Labels {
			switch {
			case strings.HasPrefix(l, labelAgentRolePrefix):
				wi.Assignee.Role = strings.TrimPrefix(l, labelAgentRolePrefix)
			case strings.HasPrefix(l, labelAgentParentPrefix):
				pid := core.AgentID(strings.TrimPrefix(l, labelAgentParentPrefix))
				if pid != "" {
					wi.Assignee.ParentID = &pid
				}
			}
		}
	}
	return wi
}

// nullString unwraps a sql.NullString into the zero value when the
// backend reported NULL. Description and similar text columns are
// defined NOT NULL in the beads schema but may be empty; the
// sql.NullString tolerant path keeps us resilient if a future
// migration relaxes the constraint.
func nullString(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}

// edgeRowsToJSON materializes the edges into the same map shape
// the bd adaptor puts on Custom["beads:dependencies"]. The SPA's
// beads/ extension consumes this shape verbatim; rendering logic
// belongs to the extension, not this adaptor.
func edgeRowsToJSON(rows []edgeRow, selfKey, selfID string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		entry := map[string]any{
			selfKey: selfID,
			"type":  e.Type,
		}
		if selfKey == "issue_id" {
			entry["depends_on_id"] = e.Linked
		} else {
			entry["issue_id"] = e.Linked
		}
		out = append(out, entry)
	}
	return out
}

// relationshipsFromRows projects the core-compatible subset of the
// edge rows onto core.Relationship, matching the bd adaptor's
// mapping exactly. Unknown edge types are dropped (the SPA sees
// them via the extension path); normalized vocabulary is
// underscore form per core.RelationshipKind.
func relationshipsFromRows(r *issueRow, self core.WorkItemID, prefix string) []core.Relationship {
	if len(r.Dependencies) == 0 && len(r.Dependents) == 0 {
		return nil
	}
	rels := make([]core.Relationship, 0, len(r.Dependencies)+len(r.Dependents))
	for _, e := range r.Dependencies {
		kind, ok := coreEdgeFor(e.Type)
		if !ok {
			continue
		}
		other := buildWorkItemID(prefix, e.Linked)
		if other == "" {
			continue
		}
		rels = append(rels, core.Relationship{Kind: kind, From: other, To: self})
	}
	for _, e := range r.Dependents {
		kind, ok := coreEdgeFor(e.Type)
		if !ok {
			continue
		}
		other := buildWorkItemID(prefix, e.Linked)
		if other == "" {
			continue
		}
		rels = append(rels, core.Relationship{Kind: kind, From: self, To: other})
	}
	if len(rels) == 0 {
		return nil
	}
	return rels
}

// coreEdgeFor normalizes a bd edge token (hyphen or underscore
// variant) onto a core RelationshipKind. Empty in → blocks, per bd
// CLI's default. Unknown tokens return false so callers can drop
// them rather than mis-classify.
func coreEdgeFor(t string) (core.RelationshipKind, bool) {
	n := strings.ReplaceAll(strings.TrimSpace(strings.ToLower(t)), "-", "_")
	switch n {
	case "":
		return core.RelBlocks, true
	case "blocks":
		return core.RelBlocks, true
	case "parent_child":
		return core.RelParentChild, true
	case "relates_to", "related":
		return core.RelRelatesTo, true
	}
	return "", false
}

// buildWorkItemID composes a workspace-qualified id from a prefix
// and a bare bead id. Empty prefix yields the bare id so callers
// driving the adaptor directly with bd ids keep working.
func buildWorkItemID(prefix, native string) core.WorkItemID {
	if prefix == "" {
		return core.WorkItemID(native)
	}
	return core.WorkItemID(prefix + "/" + native)
}

// nativeID strips the adaptor's prefix from a WorkItemID to yield
// the bare bead id the `issues` table stores. Accepts the
// canonical prefix OR any last-segment fallback so conformance ids
// of the form "gemba/gemba/gm-..." resolve cleanly.
func nativeID(prefix string, id core.WorkItemID) string {
	s := string(id)
	if prefix != "" && strings.HasPrefix(s, prefix+"/") {
		return strings.TrimPrefix(s, prefix+"/")
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
