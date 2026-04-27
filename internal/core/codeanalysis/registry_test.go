package codeanalysis

import (
	"errors"
	"testing"
)

// withFreshRegistry runs fn against a freshly-reset registry
// (gitnexus placeholder reinstalled, all other entries cleared).
// Tests that mutate the global state MUST run sequentially —
// callers SHOULD NOT t.Parallel() under this helper.
func withFreshRegistry(t *testing.T, fn func(t *testing.T)) {
	t.Helper()
	resetRegistry()
	t.Cleanup(resetRegistry)
	fn(t)
}

func TestRegistry_GitNexusPlaceholder(t *testing.T) {
	withFreshRegistry(t, func(t *testing.T) {
		if !IsRegistered(BackendGitNexus) {
			t.Fatal("expected gitnexus to be pre-registered")
		}
		p, err := Resolve(BackendGitNexus)
		if p != nil {
			t.Errorf("expected nil provider from placeholder, got %#v", p)
		}
		if !errors.Is(err, ErrNotImplemented) {
			t.Errorf("expected ErrNotImplemented, got %v", err)
		}
	})
}

func TestRegistry_RegisterAndResolve(t *testing.T) {
	withFreshRegistry(t, func(t *testing.T) {
		factory := func() (Provider, error) { return stubProvider{}, nil }
		if err := Register("stub", factory); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if !IsRegistered("stub") {
			t.Error("expected stub to be registered")
		}
		p, err := Resolve("stub")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if _, ok := p.(stubProvider); !ok {
			t.Fatalf("expected stubProvider, got %T", p)
		}
	})
}

func TestRegistry_ResolveUnknown(t *testing.T) {
	withFreshRegistry(t, func(t *testing.T) {
		_, err := Resolve("does-not-exist")
		if !errors.Is(err, ErrUnknownBackend) {
			t.Fatalf("expected ErrUnknownBackend, got %v", err)
		}
	})
}

func TestRegistry_RegisterIdempotent(t *testing.T) {
	withFreshRegistry(t, func(t *testing.T) {
		factory := func() (Provider, error) { return stubProvider{}, nil }
		if err := Register("stub", factory); err != nil {
			t.Fatalf("first Register: %v", err)
		}
		// Re-registering the same factory under the same name
		// is a no-op (same function pointer).
		if err := Register("stub", factory); err != nil {
			t.Fatalf("idempotent re-register: %v", err)
		}
	})
}

func TestRegistry_RegisterCollision(t *testing.T) {
	withFreshRegistry(t, func(t *testing.T) {
		f1 := func() (Provider, error) { return stubProvider{}, nil }
		f2 := func() (Provider, error) { return stubProvider{}, nil }
		if err := Register("stub", f1); err != nil {
			t.Fatalf("first Register: %v", err)
		}
		err := Register("stub", f2)
		if err == nil {
			t.Fatal("expected collision error registering different factory under same name")
		}
	})
}

func TestRegistry_RegisterInvalid(t *testing.T) {
	withFreshRegistry(t, func(t *testing.T) {
		if err := Register("", func() (Provider, error) { return nil, nil }); err == nil {
			t.Error("expected empty-name error")
		}
		if err := Register("ok", nil); err == nil {
			t.Error("expected nil-factory error")
		}
	})
}

func TestRegistry_Replace(t *testing.T) {
	withFreshRegistry(t, func(t *testing.T) {
		// Replace the gitnexus placeholder with a real-ish stub
		// — this is what gm-56z will do at init.
		real := func() (Provider, error) { return stubProvider{}, nil }
		if err := Replace(BackendGitNexus, real); err != nil {
			t.Fatalf("Replace: %v", err)
		}
		p, err := Resolve(BackendGitNexus)
		if err != nil {
			t.Fatalf("Resolve after Replace: %v", err)
		}
		if _, ok := p.(stubProvider); !ok {
			t.Fatalf("expected stubProvider after Replace, got %T", p)
		}
	})
}

func TestRegistry_RegisteredBackends(t *testing.T) {
	withFreshRegistry(t, func(t *testing.T) {
		factory := func() (Provider, error) { return stubProvider{}, nil }
		if err := Register("alpha", factory); err != nil {
			t.Fatalf("Register alpha: %v", err)
		}
		if err := Register("zeta", factory); err != nil {
			t.Fatalf("Register zeta: %v", err)
		}
		got := RegisteredBackends()
		want := []string{"alpha", BackendGitNexus, "zeta"}
		if len(got) != len(want) {
			t.Fatalf("RegisteredBackends() = %v, want %v", got, want)
		}
		for i, w := range want {
			if got[i] != w {
				t.Errorf("RegisteredBackends()[%d] = %q, want %q", i, got[i], w)
			}
		}
	})
}
