package persona

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/GembaCore/gemba-core/core"
	corepersona "github.com/GembaCore/gemba-core/internal/core/persona"
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

// AllowsRead reports whether p is reachable through any of s's
// read-allowed entries. The check is a glob/prefix match anchored at
// each entry — `Cwd` matches the cwd itself plus anything below it,
// `$HOME/.gitconfig` matches the file or any path that walks up from
// it. Empty input always denies (gm-eazw — Layer 2 enforcement).
//
// The match honors $HOME / $GOPATH / $GOROOT expansion via env: tooling
// paths shipped declarative (see DefaultToolingPaths) are normalized
// against env at decision time so the hook compares concrete paths,
// not envvar literals. Pass nil env to skip expansion (the resolver
// expanded already, or the call doesn't need it).
//
// Returns (allowed, reason) — reason is empty on allow, a human-
// readable explanation on deny that the bridge surfaces back to the
// model. Reason names the closest miss (sibling repo? tooling path?
// override?) so the model can reason about whether to ask for an
// override or revise the path.
func (s Surface) AllowsRead(p string, env func(string) string) (bool, string) {
	if p == "" {
		return false, "empty path"
	}
	target := normalize(p, env)
	// Reads are the union of writes (cwd + AdditionalWrites) plus the
	// pure-read entries. Walk in ascending specificity so the reason
	// names the most-specific match.
	if matchAny(target, s.Cwd, env) {
		return true, ""
	}
	for _, w := range s.AdditionalWrites {
		if matchAny(target, w, env) {
			return true, ""
		}
	}
	for _, sib := range s.SiblingReads {
		if matchAny(target, sib, env) {
			return true, ""
		}
	}
	if s.WorkspaceMetadata != "" && matchAny(target, s.WorkspaceMetadata, env) {
		return true, ""
	}
	for _, t := range s.ToolingPaths {
		if matchAny(target, t, env) {
			return true, ""
		}
	}
	for _, r := range s.AdditionalReads {
		if matchAny(target, r, env) {
			return true, ""
		}
	}
	return false, denyReason(target, "read", s)
}

// AllowsWrite reports whether p is writable. The write surface is a
// strict subset of reads: only Cwd and AdditionalWrites count. Cwd
// always wins; AdditionalWrites are operator-justified per-bead grants.
func (s Surface) AllowsWrite(p string, env func(string) string) (bool, string) {
	if p == "" {
		return false, "empty path"
	}
	target := normalize(p, env)
	if matchAny(target, s.Cwd, env) {
		return true, ""
	}
	for _, w := range s.AdditionalWrites {
		if matchAny(target, w, env) {
			return true, ""
		}
	}
	return false, denyReason(target, "write", s)
}

// matchAny reports whether target sits inside (or equals) the
// allow-pattern. Patterns are absolute-prefix-style ("$HOME/.cargo"
// matches "$HOME/.cargo" and "$HOME/.cargo/registry/index.crates.io").
// A trailing "/" on the pattern is implied — we never match a
// non-prefix-ending false positive ("/etc/foo" does NOT match
// "/etc/foobar").
func matchAny(target, pattern string, env func(string) string) bool {
	if pattern == "" {
		return false
	}
	pat := normalize(pattern, env)
	if pat == "" {
		return false
	}
	if target == pat {
		return true
	}
	// Treat pattern as a directory prefix.
	if !strings.HasSuffix(pat, "/") {
		pat += "/"
	}
	return strings.HasPrefix(target, pat)
}

// normalize cleans a path and expands $HOME / $GOPATH / $GOROOT (and
// any other envvar the caller provides via env). Returns the cleaned
// absolute-style string. env may be nil — patterns that already use
// concrete paths roundtrip unchanged.
func normalize(p string, env func(string) string) string {
	if p == "" {
		return ""
	}
	if env != nil {
		p = expandEnv(p, env)
	}
	return path.Clean(p)
}

func expandEnv(p string, env func(string) string) string {
	if !strings.Contains(p, "$") {
		return p
	}
	return os.Expand(p, env)
}

// denyReason builds the human-facing message returned to the model.
// Names the intent (read/write) and the configured allow surface so
// the operator can see at a glance whether the path is misspelled or
// the surface is too tight.
func denyReason(target, intent string, s Surface) string {
	var b strings.Builder
	fmt.Fprintf(&b, "path %q is outside this session's %s surface. ", target, intent)
	if intent == "write" {
		fmt.Fprintf(&b, "Allowed writes: %s", joinShort(s.AllowedWrites()))
	} else {
		fmt.Fprintf(&b, "Allowed reads: %s", joinShort(s.AllowedReads()))
	}
	b.WriteString(". To extend, add a `read:<glob>` or `write:<glob>` label to the bead, or ask the operator.")
	return b.String()
}

// joinShort renders an allow list compactly so the deny reason fits
// inside a single line. Drops past the first three entries with an
// ellipsis count.
func joinShort(in []string) string {
	if len(in) == 0 {
		return "(none)"
	}
	if len(in) <= 3 {
		return strings.Join(in, ", ")
	}
	return strings.Join(in[:3], ", ") + fmt.Sprintf(" + %d more", len(in)-3)
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
