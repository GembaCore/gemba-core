package persona

import (
	"sort"

	"github.com/MikeBengtson/gemba/internal/core"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
)

// Surface is the resolved set of filesystem paths a spawned session
// is allowed to read and/or write (gm-v8vr). The PreToolUse hook in
// gemba-bridge consults this when checking each tool call's path
// argument; the container backend translates it into volume mounts.
//
// Defense-in-depth design:
//
//   - Cwd and AdditionalWrites are the WRITE surface — anything else
//     is read-only at best, denied at worst.
//   - SiblingReads, WorkspaceMetadata, ToolingPaths, AdditionalReads
//     are the READ surface — read-only allowances on top of Writes.
//
// The fields are kept structured (not collapsed into a single slice)
// so consumers can present them differently: the SPA's "session
// surface" inspector groups them; the container backend uses just
// SiblingReads + ToolingPaths for `:ro` mounts; the PreToolUse hook
// short-circuits on Cwd before walking the others.
type Surface struct {
	// Cwd is the primary write target — the working directory the
	// session was spawned in. Resolved by [PersonaScope.ResolveWorkingDir]
	// (gm-k2jn). Always allowed for both read and write.
	Cwd string

	// AdditionalWrites lists glob patterns the bead's
	// [core.WorkItem.AdditionalWritePaths] declared. Very rare —
	// operator-justified.
	AdditionalWrites []string

	// SiblingReads lists every other [core.Repository.Path] in the
	// workspace's [core.RepositoryRegistry] (read-only). Lets a
	// polecat in repo-a read repo-b for cross-repo lookups
	// (e.g. API-contract verification) without granting writes there.
	SiblingReads []string

	// WorkspaceMetadata is the workspace's `.gemba/` directory
	// (read-only). Empty when no workspace dir is known. Sessions
	// need this for self-knowledge — persona registry, repository
	// registry, decisions log.
	WorkspaceMetadata string

	// ToolingPaths is the standard whitelist of tooling-config and
	// package-cache paths that real builds need (`~/.gitconfig`,
	// `~/go/pkg/mod`, etc.). Read-only. Concrete entries are picked
	// from [DefaultToolingPaths] at resolve time; operators can
	// extend via per-bead AdditionalReadPaths.
	ToolingPaths []string

	// AdditionalReads concatenates [core.WorkItem.AdditionalReadPaths]
	// and [corepersona.PersonaScope.AdditionalReadPaths]. Bead-level
	// entries come first (situational > categorical) but the resolver
	// de-duplicates so order doesn't change semantics.
	AdditionalReads []string
}

// AllowedReads returns the union of every read-allowed path in s,
// de-duplicated and stably ordered (ascending). The cwd is always
// readable; sibling repos, workspace metadata, tooling paths, and
// the per-bead/per-persona overrides are concatenated. Convenience
// accessor for the container backend's `:ro` mount list.
func (s Surface) AllowedReads() []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(s.Cwd)
	for _, p := range s.AdditionalWrites {
		add(p)
	}
	for _, p := range s.SiblingReads {
		add(p)
	}
	add(s.WorkspaceMetadata)
	for _, p := range s.ToolingPaths {
		add(p)
	}
	for _, p := range s.AdditionalReads {
		add(p)
	}
	sort.Strings(out)
	return out
}

// AllowedWrites returns the union of every write-allowed path in s,
// de-duplicated and stably ordered (ascending). Cwd plus any
// AdditionalWrites the bead granted. Convenience for the container
// backend's `:rw` mount list and for the PreToolUse hook's quick
// "is this a write-allowed prefix?" check.
func (s Surface) AllowedWrites() []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(s.Cwd)
	for _, p := range s.AdditionalWrites {
		add(p)
	}
	sort.Strings(out)
	return out
}

// SurfaceRequest carries the inputs to [ResolveSurface]. The
// dispatcher builds it once per consult; the polecat spawn driver
// builds it per polecat (with Persona = nil).
type SurfaceRequest struct {
	// Cwd is the spawn working directory (already resolved by the
	// dispatcher or polecat scheduler). Required.
	Cwd string

	// WorkspaceDir is the directory containing `.gemba/`. Optional;
	// when empty, [Surface.WorkspaceMetadata] stays empty.
	WorkspaceDir string

	// Repositories is the workspace's repository registry. Optional;
	// when nil, [Surface.SiblingReads] stays empty (the spawn falls
	// back to single-repo behavior).
	Repositories *core.RepositoryRegistry

	// Bead is the work item this spawn is associated with. Optional;
	// nil when the spawn is a persona consult that has no bead
	// context (e.g. an ad-hoc PM consult that the operator initiated
	// from /insights).
	Bead *core.WorkItem

	// Persona is the persona being consulted, if any. Optional; nil
	// for polecat spawns. When set, [PersonaScope.AdditionalReadPaths]
	// is honored.
	Persona *corepersona.Persona

	// ToolingPaths overrides [DefaultToolingPaths]. Empty means
	// "use the default whitelist." Tests inject a deterministic set;
	// production normally uses the default.
	ToolingPaths []string
}

// ResolveSurface materializes a [Surface] from req. Pure — no
// filesystem probing, no git invocations. Callers that need to
// validate the materialized paths exist on disk do so in the spawn
// driver (right place to surface "checkout missing" errors).
func ResolveSurface(req SurfaceRequest) Surface {
	s := Surface{Cwd: req.Cwd}

	if req.Bead != nil {
		s.AdditionalWrites = appendUnique(s.AdditionalWrites, req.Bead.AdditionalWritePaths...)
		s.AdditionalReads = appendUnique(s.AdditionalReads, req.Bead.AdditionalReadPaths...)
	}
	if req.Persona != nil {
		s.AdditionalReads = appendUnique(s.AdditionalReads, req.Persona.Scope.AdditionalReadPaths...)
	}

	if req.Repositories != nil {
		for _, id := range req.Repositories.List() {
			repo, ok := req.Repositories.Get(id)
			if !ok || repo.Path == req.Cwd {
				// Skip the spawned repo's own path — that's already
				// the cwd / write surface, not a sibling-read path.
				continue
			}
			s.SiblingReads = appendUnique(s.SiblingReads, repo.Path)
		}
	}

	if req.WorkspaceDir != "" {
		s.WorkspaceMetadata = req.WorkspaceDir + "/.gemba"
	}

	if len(req.ToolingPaths) > 0 {
		s.ToolingPaths = appendUnique(nil, req.ToolingPaths...)
	} else {
		s.ToolingPaths = appendUnique(nil, DefaultToolingPaths...)
	}

	return s
}

// DefaultToolingPaths is the standard read-only whitelist of tooling
// configuration and package-cache paths real builds need (gm-v8vr).
// Operators can extend via per-bead [core.WorkItem.AdditionalReadPaths]
// or per-persona [PersonaScope.AdditionalReadPaths]; the design
// favors keeping this default tight and making operators justify
// extensions.
//
// Listed as glob-friendly absolute-prefix patterns. The PreToolUse
// hook normalizes against the user's actual $HOME at runtime — no
// hardcoded `/Users/<name>` paths here.
var DefaultToolingPaths = []string{
	// Git config + GPG (signed commits) + SSH known_hosts
	"$HOME/.gitconfig",
	"$HOME/.gitignore_global",
	"$HOME/.gnupg",
	"$HOME/.ssh/known_hosts",
	// JS/TS package caches
	"$HOME/.npm",
	"$HOME/.yarn",
	"$HOME/.pnpm-store",
	// Go
	"$HOME/go/pkg/mod",
	"$GOPATH/pkg/mod",
	"$GOROOT",
	// Rust
	"$HOME/.cargo",
	"$HOME/.rustup",
	// JVM
	"$HOME/.gradle",
	"$HOME/.m2",
	// System certs + DNS (read-only)
	"/etc/ssl/cert.pem",
	"/private/etc/hosts",
	"/etc/hosts",
}

// appendUnique appends every non-empty element of in to out that
// isn't already present. Preserves input order on first occurrence.
func appendUnique(out []string, in ...string) []string {
	seen := make(map[string]bool, len(out))
	for _, v := range out {
		seen[v] = true
	}
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
