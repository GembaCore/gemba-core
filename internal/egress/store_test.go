package egress

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMemStore_CRUD_RoundTrip(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	r := Rule{
		ID:          "r1",
		WorkspaceID: "ws1",
		Action:      ActionAllow,
		Proto:       ProtoTCP,
		HostPattern: "internal.corp",
		PortStart:   443,
		PortEnd:     443,
		Priority:    10,
	}
	if err := s.Put(ctx, r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.List(ctx, "ws1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("List = %+v, want one rule with id r1", got)
	}
	if got[0].CreatedAt.IsZero() {
		t.Error("CreatedAt should be auto-stamped")
	}
	if err := s.Delete(ctx, "ws1", "r1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete(ctx, "ws1", "r1"); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("Delete second time = %v, want ErrRuleNotFound", err)
	}
	got, _ = s.List(ctx, "ws1")
	if len(got) != 0 {
		t.Fatalf("List after delete = %+v, want empty", got)
	}
}

func TestMemStore_DefaultsImmutable(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()
	def := Defaults()[0]
	bad := Rule{ID: def.ID, WorkspaceID: "ws1", Action: ActionDeny, Proto: ProtoTCP, HostPattern: "x", Priority: 1}
	if err := s.Put(ctx, bad); !errors.Is(err, ErrDefaultImmutable) {
		t.Fatalf("Put with default id = %v, want ErrDefaultImmutable", err)
	}
	if err := s.Delete(ctx, "ws1", def.ID); !errors.Is(err, ErrDefaultImmutable) {
		t.Fatalf("Delete default = %v, want ErrDefaultImmutable", err)
	}
}

func TestEffective_MergesDefaultsAndWorkspace(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()
	must(t, s.Put(ctx, Rule{ID: "ws-allow-internal", WorkspaceID: "ws1",
		Action: ActionAllow, Proto: ProtoTCP, HostPattern: "internal.corp",
		PortStart: 443, PortEnd: 443, Priority: 100}))

	eff, err := s.Effective(ctx, "ws1")
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if len(eff) != len(Defaults())+1 {
		t.Fatalf("Effective len = %d, want %d", len(eff), len(Defaults())+1)
	}
	// Sorted by priority descending.
	for i := 1; i < len(eff); i++ {
		if eff[i-1].Priority < eff[i].Priority {
			t.Fatalf("Effective not sorted desc at %d: %d < %d", i, eff[i-1].Priority, eff[i].Priority)
		}
	}
	// Top rule should be the workspace rule (Priority 100 > 0 baseline).
	if eff[0].ID != "ws-allow-internal" {
		t.Errorf("top rule = %s, want ws-allow-internal", eff[0].ID)
	}
	// Terminal rule is default-deny-all at the bottom.
	if last := eff[len(eff)-1]; last.ID != "default-deny-all" || last.Action != ActionDeny {
		t.Errorf("last rule = %+v, want default-deny-all", last)
	}
}

func TestEffective_WorkspaceCanOverrideDefault(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()
	// Workspace denies anthropic, overriding the default allow.
	must(t, s.Put(ctx, Rule{
		ID: "ws-block-anthropic", WorkspaceID: "ws1",
		Action: ActionDeny, Proto: ProtoTCP, HostPattern: "api.anthropic.com",
		PortStart: 443, PortEnd: 443, Priority: 50,
	}))
	eff, err := s.Effective(ctx, "ws1")
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	// First matching rule for (tcp, api.anthropic.com, 443) must be the deny.
	var first *Rule
	for i := range eff {
		if MatchRule(eff[i], ProtoTCP, "api.anthropic.com", 443) {
			first = &eff[i]
			break
		}
	}
	if first == nil {
		t.Fatal("no rule matched api.anthropic.com:443 — expected workspace override + default")
	}
	if first.Action != ActionDeny || first.ID != "ws-block-anthropic" {
		t.Fatalf("first match = %+v, want ws-block-anthropic deny", first)
	}
}

func TestSQLStore_MigrateIsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	// Two consecutive Migrates -> two ExecContext on the same DDL.
	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS egress_rules")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS egress_rules")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	s := NewSQLStore(db)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate #1: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate #2: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSQLStore_PutListDelete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	s := NewSQLStore(db)

	now := time.Now().UTC().Truncate(time.Second)
	r := Rule{ID: "r1", WorkspaceID: "ws1", Action: ActionAllow, Proto: ProtoTCP,
		HostPattern: "x.example", PortStart: 443, PortEnd: 443, Priority: 5, CreatedAt: now}

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO egress_rules")).
		WithArgs("r1", "ws1", "allow", "tcp", "x.example", 443, 443, 5, now, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := s.Put(context.Background(), r); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rows := sqlmock.NewRows([]string{
		"id", "workspace_id", "action", "proto", "host_pattern",
		"port_start", "port_end", "priority", "created_at", "created_by",
	}).AddRow("r1", "ws1", "allow", "tcp", "x.example", 443, 443, 5, now, nil)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, workspace_id, action, proto, host_pattern, port_start, port_end, priority, created_at, created_by")).
		WithArgs("ws1").WillReturnRows(rows)
	got, err := s.List(context.Background(), "ws1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("List = %+v", got)
	}

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM egress_rules")).
		WithArgs("ws1", "r1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.Delete(context.Background(), "ws1", "r1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestDefaults_BaselinePresent(t *testing.T) {
	defs := Defaults()
	if len(defs) < 11 {
		t.Fatalf("Defaults len = %d, want at least 11", len(defs))
	}
	wantHosts := []string{
		"api.anthropic.com", "api.openai.com", "github.com", "api.github.com",
		"ghcr.io", "registry.npmjs.org", "pypi.org", "files.pythonhosted.org",
		"deb.debian.org", "proxy.golang.org", "sum.golang.org",
	}
	have := map[string]bool{}
	for _, d := range defs {
		have[d.HostPattern] = true
	}
	for _, h := range wantHosts {
		if !have[h] {
			t.Errorf("Defaults missing host %q", h)
		}
	}
	// Terminal must be deny **.
	last := defs[len(defs)-1]
	if last.Action != ActionDeny || last.HostPattern != "**" {
		t.Errorf("terminal default = %+v, want deny **", last)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
}
