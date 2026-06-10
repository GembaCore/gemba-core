// Backend-name-keyed factory registry for code-analysis
// providers (gm-1ak).
//
// Backends call [Register] from an init function (or wherever
// the binary wires its providers) so a config that names them by
// backend string can resolve to a live [Provider]. The package
// pre-registers a `"gitnexus"` slot whose factory returns
// [ErrNotImplemented]; gm-56z replaces that factory with the
// real adapter.

package codeanalysis

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// Factory builds a [Provider]. It returns the constructed
// provider (or nil) and an error. A factory that returns
// (nil, [ErrNotImplemented]) acts as a placeholder — the
// registry slot is reserved but the backend isn't wired yet.
//
// Factories are invoked lazily at [Resolve] time, so a binary
// that never resolves a backend never pays its construction
// cost. Factories SHOULD be cheap and idempotent — callers
// resolving the same name multiple times will invoke the
// factory each time unless they cache the result themselves.
type Factory func() (Provider, error)

// BackendGitNexus is the canonical registered name for the
// GitNexus reference backend. Other adapters claim their own
// names ("sourcegraph", "codeql", …) when they ship.
const BackendGitNexus = "gitnexus"

// defaultRegistry is the process-global registry. Tests MAY
// reset it via [resetRegistry]; production callers go through
// [Register] / [Resolve].
var (
	defaultRegistryMu sync.RWMutex
	defaultRegistry   = newRegistry()
)

// registry is the internal map type. Callers don't construct it
// directly; the package keeps a single global one.
type registry struct {
	factories map[string]Factory
}

func newRegistry() *registry {
	return &registry{factories: make(map[string]Factory)}
}

// Register installs a factory under name. Returns an error when
// name is empty, factory is nil, or another factory is already
// registered under name. Re-registering the SAME factory under
// the same name is a no-op so init paths can be defensive — we
// compare the underlying function pointer via reflect.
//
// Callers needing to swap a placeholder for a real backend
// (the canonical gm-56z replacement of the gitnexus stub)
// MUST call [Replace] instead.
func Register(name string, factory Factory) error {
	if name == "" {
		return errors.New("codeanalysis: Register: name is empty")
	}
	if factory == nil {
		return errors.New("codeanalysis: Register: factory is nil")
	}
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	if existing, ok := defaultRegistry.factories[name]; ok {
		if sameFactory(existing, factory) {
			return nil
		}
		return fmt.Errorf("codeanalysis: Register: backend %q already registered", name)
	}
	defaultRegistry.factories[name] = factory
	return nil
}

// Replace installs factory under name, overwriting any prior
// entry. Used by gm-56z to swap the gitnexus placeholder for
// the real adapter at binary-init time. Returns an error when
// name is empty or factory is nil.
//
// Replace is intentionally narrow — it does NOT compare against
// the prior factory. Callers SHOULD only use it when they know
// they're replacing a placeholder.
func Replace(name string, factory Factory) error {
	if name == "" {
		return errors.New("codeanalysis: Replace: name is empty")
	}
	if factory == nil {
		return errors.New("codeanalysis: Replace: factory is nil")
	}
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	defaultRegistry.factories[name] = factory
	return nil
}

// Resolve returns a freshly-constructed [Provider] for the named
// backend. Returns [ErrUnknownBackend] when no factory is
// registered under name. Returns whatever error the factory
// returns when name is registered but the factory fails — in
// particular, the pre-registered gitnexus slot returns
// [ErrNotImplemented] until gm-56z replaces it.
func Resolve(name string) (Provider, error) {
	defaultRegistryMu.RLock()
	factory, ok := defaultRegistry.factories[name]
	defaultRegistryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownBackend, name)
	}
	return factory()
}

// RegisteredBackends returns the list of registered backend
// names in ascending string order. Useful for diagnostics
// ("which backends does this binary know about?") and for
// validating a config's `default_backend` against the live
// registry.
func RegisteredBackends() []string {
	defaultRegistryMu.RLock()
	defer defaultRegistryMu.RUnlock()
	out := make([]string, 0, len(defaultRegistry.factories))
	for name := range defaultRegistry.factories {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// IsRegistered reports whether name has a factory installed.
// Cheap; safe for hot paths.
func IsRegistered(name string) bool {
	defaultRegistryMu.RLock()
	_, ok := defaultRegistry.factories[name]
	defaultRegistryMu.RUnlock()
	return ok
}

// resetRegistry wipes the process-global registry and re-
// installs the gitnexus placeholder. Tests use this to start
// from a known state; not exported.
func resetRegistry() {
	defaultRegistryMu.Lock()
	defaultRegistry = newRegistry()
	defaultRegistry.factories[BackendGitNexus] = gitnexusPlaceholder
	defaultRegistryMu.Unlock()
}

// gitnexusPlaceholder is the factory installed under
// [BackendGitNexus] at package init. It returns
// (nil, [ErrNotImplemented]) so callers see a clean signal that
// the slot is reserved but the adapter has not landed yet.
//
// gm-56z replaces this via [Replace] when the real adapter
// ships.
func gitnexusPlaceholder() (Provider, error) {
	return nil, fmt.Errorf("%w: %s adapter ships in gm-56z", ErrNotImplemented, BackendGitNexus)
}

// sameFactory compares two Factory function values by pointer
// identity. Go does not allow direct == on function values, so
// we go through reflect.Value.Pointer. This is best-effort —
// closures over different captured state but the same code
// pointer will compare equal — but the registry only uses it
// to detect the "registering the exact same factory twice"
// idempotency case, where the comparison is reliable.
func sameFactory(a, b Factory) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

func init() {
	// Reserve the gitnexus name so configs referring to it
	// resolve to a clean ErrNotImplemented rather than
	// ErrUnknownBackend. gm-56z replaces this via Replace.
	defaultRegistry.factories[BackendGitNexus] = gitnexusPlaceholder
}
