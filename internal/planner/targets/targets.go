// Target-overlap algorithm. See doc.go for context.

package targets

import (
	"fmt"
	"strings"
)

// Pattern is a glob target. The supported grammar:
//
//   foo/bar.go         literal path
//   foo/*.go           single-segment wildcard
//   foo/?ar.go         single-character wildcard
//   foo/**             recursive prefix (matches foo, foo/x, foo/x/y/...)
//   foo/**/bar.go      recursive with literal suffix (gm-s47n.4.1 v1
//                      treats this as kindComplex — Maybe without FS)
//   **                 entire tree
//
// Character classes ([abc]) are NOT supported; if the bead authoring
// pipeline grows them, add a kindComplex fallthrough that recognises
// them so we don't silently mis-classify.
type Pattern string

// Result is the three-state output of Compare. Maybe means we'd need
// filesystem enumeration to decide; CompareWith resolves that.
type Result int

const (
	NoOverlap Result = iota
	Overlap
	Maybe
)

func (r Result) String() string {
	switch r {
	case NoOverlap:
		return "NoOverlap"
	case Overlap:
		return "Overlap"
	case Maybe:
		return "Maybe"
	default:
		return fmt.Sprintf("Result(%d)", int(r))
	}
}

// Compare reports the relationship between two patterns. Pure function;
// no I/O. For mid-segment wildcards on both sides, returns Maybe; use
// CompareWith if a deterministic answer is required.
func Compare(a, b Pattern) Result {
	return compareParsed(parse(a), parse(b))
}

// CompareSets returns Overlap if any pattern from a overlaps any from
// b, NoOverlap if every pair definitively does not overlap, Maybe
// otherwise. Short-circuits on Overlap to keep the worst case cheap
// for large bead sets.
func CompareSets(a, b []Pattern) Result {
	pa := parseAll(a)
	pb := parseAll(b)
	maybe := false
	for i := range pa {
		for j := range pb {
			switch compareParsed(pa[i], pb[j]) {
			case Overlap:
				return Overlap
			case Maybe:
				maybe = true
			}
		}
	}
	if maybe {
		return Maybe
	}
	return NoOverlap
}

// Filesystem is the safety-net enumeration interface. Implementations
// expand a pattern into a finite slice of concrete paths (typically
// by walking the working tree). Used by CompareWith to disambiguate
// Maybe results.
type Filesystem interface {
	// Glob returns the concrete paths matching pattern. The order is
	// not significant; CompareWith builds a set.
	Glob(pattern Pattern) ([]string, error)
}

// CompareWith is Compare with an FS-backed safety net. When the
// pure analysis returns Maybe, both patterns are enumerated through
// fs.Glob and the result becomes Overlap iff the intersection is
// non-empty. If fs is nil, CompareWith behaves identically to Compare.
func CompareWith(a, b Pattern, fs Filesystem) (Result, error) {
	r := Compare(a, b)
	if r != Maybe || fs == nil {
		return r, nil
	}
	left, err := fs.Glob(a)
	if err != nil {
		return Maybe, fmt.Errorf("targets.CompareWith: glob %q: %w", a, err)
	}
	right, err := fs.Glob(b)
	if err != nil {
		return Maybe, fmt.Errorf("targets.CompareWith: glob %q: %w", b, err)
	}
	leftSet := make(map[string]struct{}, len(left))
	for _, p := range left {
		leftSet[p] = struct{}{}
	}
	for _, p := range right {
		if _, ok := leftSet[p]; ok {
			return Overlap, nil
		}
	}
	return NoOverlap, nil
}

// CompareSetsWith is CompareSets with the FS-backed safety net.
// Routes each Maybe pair through CompareWith; bails on the first
// Overlap so a large set of patterns doesn't pay for every glob walk.
func CompareSetsWith(a, b []Pattern, fs Filesystem) (Result, error) {
	pa := parseAll(a)
	pb := parseAll(b)
	maybe := false
	for i := range pa {
		for j := range pb {
			r := compareParsed(pa[i], pb[j])
			switch r {
			case Overlap:
				return Overlap, nil
			case Maybe:
				if fs != nil {
					resolved, err := CompareWith(Pattern(pa[i].raw), Pattern(pb[j].raw), fs)
					if err != nil {
						return Maybe, err
					}
					if resolved == Overlap {
						return Overlap, nil
					}
					if resolved == Maybe {
						maybe = true
					}
				} else {
					maybe = true
				}
			}
		}
	}
	if maybe {
		return Maybe, nil
	}
	return NoOverlap, nil
}

// patternKind labels a pattern by structural shape so the comparison
// logic can dispatch on the cheaper-precision case first.
type patternKind int

const (
	// kindLiteral has no metacharacters: a concrete file path.
	kindLiteral patternKind = iota
	// kindPrefixGlob is "<literal>/**" or "**" — recursive subtree.
	kindPrefixGlob
	// kindComplex has wildcards mid-pattern (e.g. "src/*.go", "**/foo.go").
	kindComplex
)

type parsed struct {
	raw    string      // original pattern text after normalisation
	kind   patternKind // structural shape
	prefix string      // for kindPrefixGlob: literal prefix before /**
}

func parse(p Pattern) parsed {
	s := normalise(string(p))
	if s == "**" {
		return parsed{raw: s, kind: kindPrefixGlob, prefix: ""}
	}
	if !hasMeta(s) {
		return parsed{raw: s, kind: kindLiteral}
	}
	if pre, ok := strings.CutSuffix(s, "/**"); ok && !hasMeta(pre) {
		return parsed{raw: s, kind: kindPrefixGlob, prefix: pre}
	}
	return parsed{raw: s, kind: kindComplex}
}

func parseAll(ps []Pattern) []parsed {
	out := make([]parsed, len(ps))
	for i, p := range ps {
		out[i] = parse(p)
	}
	return out
}

// normalise strips a leading "./" or "/" and collapses "//" runs.
// We DON'T resolve "../" — the bead's targets are author-controlled
// and rendering them resolved would hide buggy patterns.
func normalise(s string) string {
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}

func hasMeta(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func compareParsed(a, b parsed) Result {
	// Canonical ordering: kindLiteral < kindPrefixGlob < kindComplex.
	// Swap so the lower-kind side is `a`. Lets the cases below skip the
	// mirror permutations.
	if a.kind > b.kind {
		a, b = b, a
	}

	switch a.kind {
	case kindLiteral:
		switch b.kind {
		case kindLiteral:
			if a.raw == b.raw {
				return Overlap
			}
			return NoOverlap
		case kindPrefixGlob:
			if isPathPrefix(b.prefix, a.raw) {
				return Overlap
			}
			return NoOverlap
		case kindComplex:
			// One side is concrete: the only path that could overlap
			// is a.raw. If b's pattern matches a.raw → overlap; else
			// no overlap (b might match other paths but a doesn't).
			if matchPattern(b.raw, a.raw) {
				return Overlap
			}
			return NoOverlap
		}
	case kindPrefixGlob:
		switch b.kind {
		case kindPrefixGlob:
			// Two recursive prefixes overlap iff one prefix path-
			// contains the other.
			if isPathPrefix(a.prefix, b.prefix) || isPathPrefix(b.prefix, a.prefix) {
				return Overlap
			}
			return NoOverlap
		case kindComplex:
			// Need FS enumeration to decide deterministically.
			// Conservative defaults: if b's first segment is a literal
			// AND that literal is not under a's prefix, definitively
			// NoOverlap. Otherwise Maybe.
			if firstSeg, _, hasMore := strings.Cut(b.raw, "/"); !hasMeta(firstSeg) && hasMore {
				if !isPathPrefix(a.prefix, firstSeg) && !isPathPrefix(firstSeg, a.prefix) {
					return NoOverlap
				}
			}
			return Maybe
		}
	case kindComplex:
		// Both sides have mid-segment wildcards. The pure analysis
		// can't intersect arbitrary glob languages cheaply; defer to
		// the FS safety net.
		return Maybe
	}
	return Maybe
}

// isPathPrefix reports whether `short` is a path-segment prefix of
// `long` — i.e. long == short, OR long starts with short + "/". An
// empty short is a prefix of every path (matches the "**" case).
func isPathPrefix(short, long string) bool {
	if short == "" {
		return true
	}
	if short == long {
		return true
	}
	return strings.HasPrefix(long, short+"/")
}

// Match reports whether the given concrete path matches the pattern.
// Path is repo-relative and forward-slashed; pattern uses the same
// glob grammar as Compare (literal / *, ?, **). Pure; no I/O.
//
// The retrospective (gm-s47n.8.1) uses this to ask "is this actual
// file covered by any declared pattern?" without going through the
// pair-wise pattern Compare.
func Match(pattern Pattern, path string) bool {
	return matchPattern(normalise(string(pattern)), normalise(path))
}

// matchPattern is a path-aware glob matcher with `**` support.
// `*` matches one path segment (no slashes), `?` matches one
// character within a segment, `**` matches any number of segments
// (including zero). Implementation: walk segments left-to-right,
// recursing on `**`.
func matchPattern(pattern, name string) bool {
	patSegs := strings.Split(pattern, "/")
	nameSegs := strings.Split(name, "/")
	return matchSegs(patSegs, nameSegs)
}

func matchSegs(pat, name []string) bool {
	for {
		if len(pat) == 0 {
			return len(name) == 0
		}
		if pat[0] == "**" {
			// Match zero name segments (consume the **) OR consume
			// one name segment and keep the **.
			rest := pat[1:]
			// "**" at end of pattern → matches any remaining tail.
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegs(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if !matchOneSegment(pat[0], name[0]) {
			return false
		}
		pat = pat[1:]
		name = name[1:]
	}
}

// matchOneSegment is path.Match semantics restricted to a single
// segment (no slashes anywhere). Implements * and ?; rejects [...]
// (we don't use character classes — see doc.go).
func matchOneSegment(pat, name string) bool {
	for {
		if pat == "" {
			return name == ""
		}
		if pat[0] == '*' {
			// Greedy *: try matching zero or more chars, no slashes.
			// Compress consecutive *'s.
			for len(pat) > 0 && pat[0] == '*' {
				pat = pat[1:]
			}
			if pat == "" {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchOneSegment(pat, name[i:]) {
					return true
				}
			}
			return false
		}
		if pat[0] == '?' {
			if name == "" {
				return false
			}
			pat = pat[1:]
			name = name[1:]
			continue
		}
		if pat[0] == '[' {
			// Character classes intentionally unsupported.
			return false
		}
		if name == "" || pat[0] != name[0] {
			return false
		}
		pat = pat[1:]
		name = name[1:]
	}
}
