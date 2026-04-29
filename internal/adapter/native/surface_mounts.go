package native

import (
	"os"
	"sort"

	"github.com/GembaCore/gemba-core/internal/adapter/native/backend"
	"github.com/GembaCore/gemba-core/internal/persona"
)

// SurfaceMode controls how operator-declared mounts in agents.toml
// compose with the surface-derived mount set (gm-utik). The default
// is additive — operator extras layer on top of the policy-enforced
// surface; "exclusive" drops them and logs a warning.
type SurfaceMode string

const (
	// SurfaceModeAdditive is the default: operator-declared mounts
	// extend the surface-derived set. Conflicts (same Dst) prefer
	// the surface mount because the surface is the trust boundary.
	SurfaceModeAdditive SurfaceMode = "additive"

	// SurfaceModeExclusive drops every operator-declared mount and
	// emits only the surface-derived set. Right for high-assurance
	// polecats where operators MUST NOT extend the filesystem trust
	// boundary.
	SurfaceModeExclusive SurfaceMode = "exclusive"
)

// ParseSurfaceMode normalizes a stringly-typed surface_mode value
// from agents.toml. Empty string → additive (the default operators
// get when they don't opt in). Unknown values → additive plus the
// returned ok=false so callers can warn.
func ParseSurfaceMode(s string) (SurfaceMode, bool) {
	switch s {
	case "", string(SurfaceModeAdditive):
		return SurfaceModeAdditive, true
	case string(SurfaceModeExclusive):
		return SurfaceModeExclusive, true
	default:
		return SurfaceModeAdditive, false
	}
}

// SurfaceMounts translates a [persona.Surface] into the mount list a
// container backend can consume directly (gm-utik). The Src and Dst
// of each mount are the same path — the container sees the host
// layout one-to-one so an agent's path arguments stay portable across
// backends.
//
// Rules:
//
//   - Every entry in s.AllowedWrites becomes a "rw" bind-mount.
//   - Every entry in s.AllowedReads that isn't already a write
//     becomes a "ro" bind-mount (writes win — the dedup check is
//     keyed on Src so the broader permission survives).
//   - Paths whose Src does not exist on the host are skipped. We
//     surface them through the returned `skipped` slice so the
//     spawn driver can decide whether to log; this keeps the
//     decision out of the pure path layer.
//   - Output is sorted by destination path so the docker argv stays
//     deterministic — easier diffing in tests, easier review of
//     spawn logs.
//
// pathExists is injected so tests can pin behavior without touching
// the filesystem. Production callers pass [os.Stat]-backed
// [DefaultPathExists].
func SurfaceMounts(s persona.Surface, pathExists func(string) bool) (mounts []backend.Mount, skipped []string) {
	if pathExists == nil {
		pathExists = DefaultPathExists
	}

	// seen tracks every path we've already emitted OR skipped, so a
	// path that appears in both Allowed{Reads,Writes} (Cwd does, by
	// construction) doesn't get reported twice in the skip list.
	seen := make(map[string]bool)
	writeSet := make(map[string]bool)
	for _, p := range s.AllowedWrites() {
		if seen[p] {
			continue
		}
		seen[p] = true
		if !pathExists(p) {
			skipped = append(skipped, p)
			continue
		}
		writeSet[p] = true
		mounts = append(mounts, backend.Mount{Src: p, Dst: p, Mode: "rw"})
	}

	for _, p := range s.AllowedReads() {
		if writeSet[p] || seen[p] {
			continue
		}
		seen[p] = true
		if !pathExists(p) {
			skipped = append(skipped, p)
			continue
		}
		mounts = append(mounts, backend.Mount{Src: p, Dst: p, Mode: "ro"})
	}

	sort.SliceStable(mounts, func(i, j int) bool { return mounts[i].Dst < mounts[j].Dst })
	return mounts, skipped
}

// MergeSurfaceMounts composes the surface-derived mount set with
// operator-declared mounts from agents.toml per the requested
// SurfaceMode. The result is the slice the spawn-spec ships to the
// backend; ordering is operator-mounts-first (additive only) so the
// operator's intent is visible, then the surface-derived set.
//
// Dedup rule: when the same Dst appears in both sets, the surface
// mount wins because the surface is the trust boundary. The dropped
// operator entries are returned for caller-side logging — the
// decision to surface them as warnings stays in the spawn driver.
func MergeSurfaceMounts(operator []backend.Mount, surface []backend.Mount, mode SurfaceMode) (mounts []backend.Mount, dropped []backend.Mount) {
	// Exclusive: drop everything the operator declared. Their slice
	// is reported as "dropped" so the spawn driver can warn.
	if mode == SurfaceModeExclusive {
		dropped = append(dropped, operator...)
		mounts = append(mounts, surface...)
		return mounts, dropped
	}

	// Additive: operator first, surface second. Where Dst collides,
	// the surface entry takes priority (writes are the broadest
	// permission; we don't want an operator-declared :ro to demote
	// a surface-declared :rw on the same path).
	surfaceDst := make(map[string]bool, len(surface))
	for _, m := range surface {
		surfaceDst[m.Dst] = true
	}
	for _, m := range operator {
		if surfaceDst[m.Dst] {
			dropped = append(dropped, m)
			continue
		}
		mounts = append(mounts, m)
	}
	mounts = append(mounts, surface...)
	return mounts, dropped
}

// DefaultPathExists is the production [os.Stat]-backed predicate.
// Symlinks are followed (Stat, not Lstat) so a workspace's
// vendored-symlink-to-cargo-cache resolves cleanly. Errors other
// than "not exist" still report false because we'd rather skip a
// mount we can't stat than spawn a container that fails on `docker
// run`.
func DefaultPathExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}
