package dolt

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/GembaCore/gemba-core/core"
)

// TestVerifySchema_KnownVersion covers the happy path: when the
// schema_migrations tip is one of the pinned known-good versions,
// verifySchema returns nil.
func TestVerifySchema_KnownVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	known := knownSchemaVersionsSorted()
	if len(known) == 0 {
		t.Fatal("knownSchemaVersions must not be empty")
	}
	pick := known[len(known)-1]

	mock.ExpectQuery(`SELECT COALESCE\(MAX\(version\), 0\) FROM schema_migrations`).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(pick))

	if err := verifySchema(context.Background(), db); err != nil {
		t.Fatalf("verifySchema(known=%d): %v", pick, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// TestVerifySchema_UnknownVersion covers the incompatible-schema
// branch: a schema_migrations tip outside the pinned set must fail
// with KindAdaptorDegraded and attach the detected version in
// Detail so operators see what they're running against.
func TestVerifySchema_UnknownVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// Pick a version guaranteed to be outside the known set: one past
	// the highest pinned version. If someone adds that exact version
	// to knownSchemaVersions without updating this test, the test
	// fails loudly rather than silently passing for the wrong reason.
	known := knownSchemaVersionsSorted()
	unknown := known[len(known)-1] + 1000
	if _, ok := knownSchemaVersions[unknown]; ok {
		t.Fatalf("test picked %d but it is in knownSchemaVersions", unknown)
	}

	mock.ExpectQuery(`SELECT COALESCE\(MAX\(version\), 0\) FROM schema_migrations`).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(unknown))

	err = verifySchema(context.Background(), db)
	if err == nil {
		t.Fatal("verifySchema returned nil for unknown version")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected tagged AdaptorError, got %T: %v", err, err)
	}
	if ae.Kind != core.KindAdaptorDegraded {
		t.Errorf("Kind: got %s want %s", ae.Kind, core.KindAdaptorDegraded)
	}
	if ae.Detail == nil {
		t.Fatal("expected Detail to carry detected schema version")
	}
	if got := ae.Detail["schema_version"]; got != unknown {
		t.Errorf("Detail[schema_version]: got %v want %d", got, unknown)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// TestVerifySchema_MissingTable covers the non-beads-database case:
// if schema_migrations does not exist (or any query error surfaces),
// the adaptor must still fail with KindAdaptorDegraded so serve.go
// refuses to register the workplane.
func TestVerifySchema_MissingTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT COALESCE\(MAX\(version\), 0\) FROM schema_migrations`).
		WillReturnError(errors.New("Table 'gemba.schema_migrations' doesn't exist"))

	err = verifySchema(context.Background(), db)
	if err == nil {
		t.Fatal("verifySchema returned nil when schema_migrations missing")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected tagged AdaptorError, got %T: %v", err, err)
	}
	if ae.Kind != core.KindAdaptorDegraded {
		t.Errorf("Kind: got %s want %s", ae.Kind, core.KindAdaptorDegraded)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}
