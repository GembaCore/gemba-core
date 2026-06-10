package workspaces

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/tenant"
)

// registryFactory builds a fresh Registry for one subtest. Both
// MemStore and SQLStore feed into runConformance through a factory so
// the test cases never reach for impl-specific seams.
type registryFactory func(t *testing.T) Registry

// runConformance drives the same scenario list against every Registry
// implementation. Centralising the cases keeps MemStore and SQLStore
// from drifting — every behavior change is enforced in one place.
func runConformance(t *testing.T, name string, newReg registryFactory) {
	t.Helper()
	t.Run(name+"/CreateAndResolve", func(t *testing.T) {
		reg := newReg(t)
		ctx := context.Background()
		w, err := reg.Create(ctx, CreateInput{
			TenantID:    string(tenant.DefaultTenant),
			Slug:        "alpha",
			ProjectPath: "/tmp/alpha",
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if w.ID != tenant.DefaultTenant.Prefix()+":alpha" {
			t.Errorf("ID = %q, want %q", w.ID, tenant.DefaultTenant.Prefix()+":alpha")
		}
		// Resolve by canonical form.
		got, err := reg.Resolve(ctx, w.ID)
		if err != nil {
			t.Fatalf("Resolve canonical: %v", err)
		}
		if got.ProjectPath != "/tmp/alpha" {
			t.Errorf("ProjectPath = %q", got.ProjectPath)
		}
		// Resolve by bare-slug (M1 backwards compat).
		got, err = reg.Resolve(ctx, "alpha")
		if err != nil {
			t.Fatalf("Resolve bare: %v", err)
		}
		if got.TenantID != string(tenant.DefaultTenant) {
			t.Errorf("bare-slug TenantID = %q, want %q", got.TenantID, tenant.DefaultTenant)
		}
	})

	t.Run(name+"/ResolveMissing", func(t *testing.T) {
		reg := newReg(t)
		_, err := reg.Resolve(context.Background(), "no-such-thing")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run(name+"/CreateDuplicate", func(t *testing.T) {
		reg := newReg(t)
		ctx := context.Background()
		in := CreateInput{
			TenantID:    string(tenant.DefaultTenant),
			Slug:        "dup",
			ProjectPath: "/tmp/dup",
		}
		if _, err := reg.Create(ctx, in); err != nil {
			t.Fatalf("first Create: %v", err)
		}
		if _, err := reg.Create(ctx, in); !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("second Create err = %v, want ErrAlreadyExists", err)
		}
	})

	t.Run(name+"/CreateInvalid", func(t *testing.T) {
		reg := newReg(t)
		ctx := context.Background()
		cases := []CreateInput{
			{},
			{TenantID: string(tenant.DefaultTenant)},
			{TenantID: string(tenant.DefaultTenant), Slug: "x"},
			{Slug: "x", ProjectPath: "/tmp/x"}, // missing tenant
			{TenantID: "not-a-tenant", Slug: "x", ProjectPath: "/tmp/x"},
		}
		for i, in := range cases {
			if _, err := reg.Create(ctx, in); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("case %d: err = %v, want ErrInvalidInput", i, err)
			}
		}
	})

	t.Run(name+"/Delete", func(t *testing.T) {
		reg := newReg(t)
		ctx := context.Background()
		if _, err := reg.Create(ctx, CreateInput{
			TenantID:    string(tenant.DefaultTenant),
			Slug:        "to-delete",
			ProjectPath: "/tmp/del",
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := reg.Delete(ctx, "to-delete"); err != nil {
			t.Fatalf("Delete bare: %v", err)
		}
		if _, err := reg.Resolve(ctx, "to-delete"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("post-Delete Resolve err = %v, want ErrNotFound", err)
		}
		if err := reg.Delete(ctx, "to-delete"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("repeat Delete err = %v, want ErrNotFound", err)
		}
	})

	t.Run(name+"/CrossTenantCollision", func(t *testing.T) {
		reg := newReg(t)
		ctx := context.Background()
		// Two tenants, same slug. Both must resolve independently.
		tidA := "t-aaaaaaaa"
		tidB := "t-bbbbbbbb"
		wA, err := reg.Create(ctx, CreateInput{TenantID: tidA, Slug: "foo", ProjectPath: "/tmp/a/foo"})
		if err != nil {
			t.Fatalf("Create A: %v", err)
		}
		wB, err := reg.Create(ctx, CreateInput{TenantID: tidB, Slug: "foo", ProjectPath: "/tmp/b/foo"})
		if err != nil {
			t.Fatalf("Create B: %v", err)
		}
		if wA.ID == wB.ID {
			t.Fatalf("expected distinct wsids; both = %q", wA.ID)
		}
		gotA, err := reg.Resolve(ctx, wA.ID)
		if err != nil {
			t.Fatalf("Resolve A: %v", err)
		}
		if gotA.TenantID != tidA || gotA.ProjectPath != "/tmp/a/foo" {
			t.Errorf("A resolved to %+v", gotA)
		}
		gotB, err := reg.Resolve(ctx, wB.ID)
		if err != nil {
			t.Fatalf("Resolve B: %v", err)
		}
		if gotB.TenantID != tidB || gotB.ProjectPath != "/tmp/b/foo" {
			t.Errorf("B resolved to %+v", gotB)
		}
	})

	t.Run(name+"/List", func(t *testing.T) {
		reg := newReg(t)
		ctx := context.Background()
		for _, slug := range []string{"a", "b", "c"} {
			if _, err := reg.Create(ctx, CreateInput{
				TenantID:    string(tenant.DefaultTenant),
				Slug:        slug,
				ProjectPath: "/tmp/" + slug,
			}); err != nil {
				t.Fatalf("Create %s: %v", slug, err)
			}
		}
		ws, err := reg.List(ctx, ListOpts{TenantID: string(tenant.DefaultTenant)})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(ws) != 3 {
			t.Fatalf("len(List) = %d, want 3", len(ws))
		}
		// Confirm tenant filter excludes other tenants.
		if _, err := reg.Create(ctx, CreateInput{
			TenantID:    "t-cccccccc",
			Slug:        "other",
			ProjectPath: "/tmp/other",
		}); err != nil {
			t.Fatalf("Create other-tenant: %v", err)
		}
		ws, err = reg.List(ctx, ListOpts{TenantID: string(tenant.DefaultTenant)})
		if err != nil {
			t.Fatalf("List filtered: %v", err)
		}
		for _, w := range ws {
			if w.TenantID != string(tenant.DefaultTenant) {
				t.Errorf("tenant filter leaked %s", w.TenantID)
			}
		}
	})
}

func TestMemStore_Conformance(t *testing.T) {
	runConformance(t, "MemStore", func(t *testing.T) Registry {
		t.Helper()
		s := NewMemStore()
		s.SetClock(func() time.Time { return time.Unix(1_700_000_000, 0).UTC() })
		return s
	})
}

func TestBootstrapFromDir(t *testing.T) {
	tmp := t.TempDir()
	// Two project dirs + a hidden dir we must skip.
	for _, d := range []string{"proj-one", "proj-two"} {
		if err := os.MkdirAll(filepath.Join(tmp, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(tmp, ".cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := NewMemStore()
	ctx := context.Background()
	n, err := BootstrapFromDir(ctx, reg, tmp)
	if err != nil {
		t.Fatalf("BootstrapFromDir: %v", err)
	}
	if n != 2 {
		t.Errorf("inserted = %d, want 2", n)
	}
	// Idempotent re-run.
	n, err = BootstrapFromDir(ctx, reg, tmp)
	if err != nil {
		t.Fatalf("BootstrapFromDir rerun: %v", err)
	}
	if n != 0 {
		t.Errorf("rerun inserted = %d, want 0", n)
	}
	// Verify rows resolve under the default tenant.
	w, err := reg.Resolve(ctx, "proj-one")
	if err != nil {
		t.Fatalf("Resolve proj-one: %v", err)
	}
	if w.TenantID != string(tenant.DefaultTenant) {
		t.Errorf("TenantID = %q", w.TenantID)
	}
	if w.ProjectPath != filepath.Join(tmp, "proj-one") {
		t.Errorf("ProjectPath = %q", w.ProjectPath)
	}
}

func TestBootstrapFromDir_MissingRoot(t *testing.T) {
	reg := NewMemStore()
	n, err := BootstrapFromDir(context.Background(), reg, "/no/such/path/here")
	if err != nil {
		t.Fatalf("BootstrapFromDir missing: %v", err)
	}
	if n != 0 {
		t.Errorf("inserted = %d, want 0", n)
	}
}
