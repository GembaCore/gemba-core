package projectshape

import (
	"errors"
	"testing"
)

// expectedSeedNames lists the 5 archetypes gm-4uso ships, in the
// order SeedShapes is documented to return. Tests assert all five
// land in the default registry; additions to the seed set MUST
// update this list intentionally.
var expectedSeedNames = []string{
	"gemba",
	"two-tier-spa",
	"two-tier-ssr",
	"three-tier-with-edge",
	"mobile-with-backend",
}

func TestSeedShapes_HasAllFiveArchetypes(t *testing.T) {
	got := SeedShapes()
	if len(got) != len(expectedSeedNames) {
		t.Fatalf("SeedShapes len = %d, want %d", len(got), len(expectedSeedNames))
	}
	for i, want := range expectedSeedNames {
		if got[i].Name != want {
			t.Errorf("SeedShapes[%d].Name = %q, want %q", i, got[i].Name, want)
		}
	}
}

func TestSeedShapes_AllNonZero(t *testing.T) {
	for _, s := range SeedShapes() {
		if s.IsZero() {
			t.Errorf("seed %q is the zero Shape — fill in fields", s.Name)
		}
		if s.Render() == "" {
			t.Errorf("seed %q renders empty — Render() contract broken", s.Name)
		}
	}
}

func TestSeedShapes_ReturnsFreshSliceEachCall(t *testing.T) {
	a := SeedShapes()
	a[0].Name = "MUTATED"
	b := SeedShapes()
	if b[0].Name == "MUTATED" {
		t.Fatal("SeedShapes returned shared backing slice; mutations leak")
	}
}

func TestDefaultRegistry_RegistersAllSeeds(t *testing.T) {
	reg := DefaultRegistry()
	for _, name := range expectedSeedNames {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("DefaultRegistry missing seed %q", name)
		}
	}
	names := reg.Names()
	if len(names) != len(expectedSeedNames) {
		t.Errorf("Names len = %d, want %d (got %v)", len(names), len(expectedSeedNames), names)
	}
}

func TestRegistry_Lookup_Found(t *testing.T) {
	reg := DefaultRegistry()
	s, ok := reg.Lookup("two-tier-spa")
	if !ok {
		t.Fatal("Lookup(two-tier-spa) ok=false; want true")
	}
	if s.Name != "two-tier-spa" {
		t.Errorf("Lookup returned name %q, want two-tier-spa", s.Name)
	}
}

func TestRegistry_Lookup_Unknown(t *testing.T) {
	reg := DefaultRegistry()
	_, ok := reg.Lookup("does-not-exist")
	if ok {
		t.Fatal("Lookup(does-not-exist) ok=true; want false")
	}
}

func TestRegistry_Resolve_TypedErrorOnUnknown(t *testing.T) {
	reg := DefaultRegistry()
	_, err := reg.Resolve("does-not-exist")
	if err == nil {
		t.Fatal("Resolve(unknown) returned nil error")
	}
	if !errors.Is(err, ErrUnknownShape) {
		t.Errorf("Resolve error not Is(ErrUnknownShape): %v", err)
	}
}

func TestRegistry_LookupOrGeneric_FallbackOnEmpty(t *testing.T) {
	reg := DefaultRegistry()
	s := reg.LookupOrGeneric("")
	if !s.IsZero() {
		t.Errorf("LookupOrGeneric(\"\") returned %+v, want zero", s)
	}
}

func TestRegistry_LookupOrGeneric_FallbackOnUnknown(t *testing.T) {
	// gm-4uso fallback contract: an unknown name MUST yield the
	// generic Shape{} so the dispatcher never breaks on bad input.
	// Typed-error path (Resolve) is for the loader; this method is
	// for the dispatcher's hot path.
	reg := DefaultRegistry()
	s := reg.LookupOrGeneric("does-not-exist")
	if !s.IsZero() {
		t.Errorf("LookupOrGeneric(unknown) returned %+v, want zero", s)
	}
}

func TestRegistry_LookupOrGeneric_Hit(t *testing.T) {
	reg := DefaultRegistry()
	s := reg.LookupOrGeneric("gemba")
	if s.Name != "gemba" {
		t.Errorf("LookupOrGeneric(gemba) name = %q, want gemba", s.Name)
	}
}

func TestRegistry_Register_DuplicateRejected(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Shape{Name: "x"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := reg.Register(Shape{Name: "x"}); err == nil {
		t.Fatal("second Register(x): nil error; want duplicate rejection")
	}
}

func TestRegistry_Register_EmptyNameRejected(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Shape{}); err == nil {
		t.Fatal("Register(zero) returned nil; want empty-name rejection")
	}
}
