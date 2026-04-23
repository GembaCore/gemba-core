package dolt_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/MikeBengtson/gemba/internal/adapter/dolt"
	"github.com/MikeBengtson/gemba/internal/core"
)

// issueColumns mirrors the listColumns order in workplane.go. Tests
// build rows with these names so a refactor of the column list
// fails here rather than silently misordering the Scan targets.
var issueColumns = []string{
	"id", "title", "description", "status", "priority", "issue_type",
	"assignee", "owner", "created_at", "created_by", "updated_at", "notes",
}

func newMock(t *testing.T) (*dolt.WorkPlane, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	wp := dolt.NewWorkPlaneFromDB(db, "gemba/gemba", "gemba")
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet mock expectations: %v", err)
		}
		_ = db.Close()
	}
	return wp, mock, cleanup
}

// oneRow returns a sqlmock.Rows with a single canned issue tuple,
// useful across the Get/List paths. Test helpers populate only the
// fields the assertions actually care about.
func oneRow(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(issueColumns).AddRow(
		"gm-0fd", "M1.2b: dolt-url", "a desc", "hooked", 0, "task",
		"gemba/polecats/quartz", "", now, "mike", now, "sample notes",
	)
}

func TestDescribe_ReadOnlyManifest(t *testing.T) {
	wp, _, cleanup := newMock(t)
	defer cleanup()

	manifest, err := wp.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if !manifest.ReadOnly {
		t.Error("manifest.ReadOnly must be true for the dolt adaptor")
	}
	if manifest.AdaptorName != "beads-dolt" {
		t.Errorf("AdaptorName: got %q want %q", manifest.AdaptorName, "beads-dolt")
	}
	if err := manifest.Validate(); err != nil {
		t.Errorf("manifest must be Validate()-clean: %v", err)
	}
	// Both beads adaptors must declare markdown by default so the SPA
	// renders descriptions through the markdown renderer end-to-end.
	if manifest.DescriptionFormat != core.DescriptionFormatMarkdown {
		t.Errorf("DescriptionFormat: got %q want %q",
			manifest.DescriptionFormat, core.DescriptionFormatMarkdown)
	}
}

func TestListWorkItems_HappyPath(t *testing.T) {
	wp, mock, cleanup := newMock(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Second)

	mock.ExpectQuery(`SELECT .* FROM issues`).
		WillReturnRows(oneRow(now))
	mock.ExpectQuery(`SELECT issue_id, label FROM labels`).
		WithArgs("gm-0fd").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "label"}).
			AddRow("gm-0fd", "milestone:m1").
			AddRow("gm-0fd", "risk:high"))
	mock.ExpectQuery(`SELECT issue_id, depends_on_id, type FROM dependencies WHERE issue_id`).
		WithArgs("gm-0fd").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id", "type"}).
			AddRow("gm-0fd", "gm-wisp-msr6", "blocks"))
	mock.ExpectQuery(`SELECT issue_id, depends_on_id, type FROM dependencies WHERE depends_on_id`).
		WithArgs("gm-0fd").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id", "type"}))

	items, err := wp.ListWorkItems(context.Background(), core.WorkItemFilter{})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	wi := items[0]
	if wi.ID != "gemba/gemba/gm-0fd" {
		t.Errorf("ID: %q", wi.ID)
	}
	if wi.Status != "hooked" || wi.StateCategory != core.StateStarted {
		t.Errorf("state: status=%q category=%q", wi.Status, wi.StateCategory)
	}
	if len(wi.Labels) != 2 {
		t.Errorf("labels: %v", wi.Labels)
	}
	// blocker "gm-wisp-msr6" → from, self → to
	if len(wi.Relationships) != 1 || wi.Relationships[0].Kind != core.RelBlocks {
		t.Errorf("relationships: %+v", wi.Relationships)
	}
	if wi.Assignee == nil || wi.Assignee.ID != "gemba/polecats/quartz" {
		t.Errorf("assignee: %+v", wi.Assignee)
	}
}

func TestListWorkItems_PushesStatusAssigneeLimit(t *testing.T) {
	wp, mock, cleanup := newMock(t)
	defer cleanup()
	now := time.Now().UTC()

	// Status + assignee go into WHERE; limit appends LIMIT ?. We
	// assert argument order matches the clauses so a refactor that
	// reorders predicates fails here.
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT id, title, description, status, priority, issue_type, assignee, "+
			"owner, created_at, created_by, updated_at, notes FROM issues "+
			"WHERE status = ? AND assignee = ? ORDER BY updated_at DESC LIMIT ?")).
		WithArgs("hooked", "gemba/polecats/quartz", 5).
		WillReturnRows(oneRow(now))
	// Hydrations still run on the single-row result set.
	mock.ExpectQuery(`SELECT issue_id, label FROM labels`).
		WithArgs("gm-0fd").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "label"}))
	mock.ExpectQuery(`SELECT issue_id, depends_on_id, type FROM dependencies WHERE issue_id`).
		WithArgs("gm-0fd").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id", "type"}))
	mock.ExpectQuery(`SELECT issue_id, depends_on_id, type FROM dependencies WHERE depends_on_id`).
		WithArgs("gm-0fd").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id", "type"}))

	assignee := core.AgentID("gemba/polecats/quartz")
	_, err := wp.ListWorkItems(context.Background(), core.WorkItemFilter{
		Statuses:   []string{"hooked"},
		AssigneeID: &assignee,
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
}

func TestGetWorkItem_NotFound(t *testing.T) {
	wp, mock, cleanup := newMock(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT .* FROM issues WHERE id = \?`).
		WithArgs("gm-missing").
		WillReturnRows(sqlmock.NewRows(issueColumns))

	_, err := wp.GetWorkItem(context.Background(), "gemba/gemba/gm-missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, core.ErrNotFound) {
		t.Errorf("want ErrNotFound-compatible, got %v", err)
	}
	ae := core.AsAdaptorError(err)
	if ae == nil || ae.Kind != core.KindSessionNotFound {
		t.Errorf("want KindSessionNotFound, got %+v", ae)
	}
}

func TestGetWorkItem_HappyPath(t *testing.T) {
	wp, mock, cleanup := newMock(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Second)

	mock.ExpectQuery(`SELECT .* FROM issues WHERE id = \?`).
		WithArgs("gm-0fd").
		WillReturnRows(oneRow(now))
	mock.ExpectQuery(`SELECT issue_id, label FROM labels`).
		WithArgs("gm-0fd").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "label"}))
	mock.ExpectQuery(`SELECT issue_id, depends_on_id, type FROM dependencies WHERE issue_id`).
		WithArgs("gm-0fd").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id", "type"}))
	mock.ExpectQuery(`SELECT issue_id, depends_on_id, type FROM dependencies WHERE depends_on_id`).
		WithArgs("gm-0fd").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id", "type"}))

	wi, err := wp.GetWorkItem(context.Background(), "gemba/gemba/gm-0fd")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.ID != "gemba/gemba/gm-0fd" {
		t.Errorf("ID: %q", wi.ID)
	}
	if wi.Custom["beads:issue_type"] != "task" {
		t.Errorf("issue_type custom: %v", wi.Custom["beads:issue_type"])
	}
}

// The three write methods share the same contract: fail fast with
// KindReadOnly. Table-driving keeps the repetition short.
func TestWrites_AllReturnReadOnly(t *testing.T) {
	wp, _, cleanup := newMock(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("CreateWorkItem", func(t *testing.T) {
		_, err := wp.CreateWorkItem(ctx, core.WorkItem{Title: "x"})
		assertReadOnly(t, err)
	})
	t.Run("UpdateWorkItem", func(t *testing.T) {
		_, err := wp.UpdateWorkItem(ctx, "gemba/gemba/gm-0fd", core.WorkItemPatch{})
		assertReadOnly(t, err)
	})
	t.Run("AttachEvidence", func(t *testing.T) {
		err := wp.AttachEvidence(ctx, "gemba/gemba/gm-0fd", core.Evidence{ID: "e1"})
		assertReadOnly(t, err)
	})
}

func assertReadOnly(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected tagged AdaptorError; got %v", err)
	}
	if ae.Kind != core.KindReadOnly {
		t.Errorf("want KindReadOnly, got %s", ae.Kind)
	}
	if ae.Retryable {
		t.Error("KindReadOnly must not be retryable")
	}
}

// Parse surface: malformed --dolt-url must fail at construction
// with KindValidation rather than deferring to a useless ping
// error. Covers the three shapes we care about: wrong scheme,
// missing host, missing database.
func TestNewWorkPlane_URLValidation(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"wrong-scheme", "postgres://root@localhost:3307/gemba"},
		{"missing-host", "mysql:///gemba"},
		{"missing-db", "mysql://root@127.0.0.1:3307"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := dolt.NewWorkPlane(dolt.Config{URL: tc.url})
			if err == nil {
				t.Fatalf("want error for %q", tc.url)
			}
			ae := core.AsAdaptorError(err)
			if ae == nil || ae.Kind != core.KindValidation {
				t.Errorf("want KindValidation, got %+v", ae)
			}
		})
	}
}
