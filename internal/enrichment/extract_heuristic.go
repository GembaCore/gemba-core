package enrichment

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// HeuristicExtractor mines bead text for the same shapes operators
// already write by hand:
//
//   - **Targets** come from path-shaped tokens. The detector matches
//     two families:
//       1. Backtick-fenced paths (`internal/auth/auth.go`,
//          `web/src/App.tsx`) — the most reliable signal because the
//          author has already committed to the path being literal.
//       2. Bare path tokens with a recognized prefix (`internal/`,
//          `cmd/`, `web/src/`, `docs/`, `testing/`) and either a
//          file extension or a trailing `/`. Looser, so prefixes
//          gate it.
//   - **Concepts** come from substring matches against the supplied
//     vocabulary. Word-boundary aware so "auth" doesn't match
//     "author"; case-insensitive so "Auth" / "AUTH" / "auth" all
//     resolve.
//
// The extractor is pure, network-free, and ships in the binary so
// the bead-create pipeline produces useful enrichment from day one
// — no API keys, no rate limits. An LLM-backed extractor can layer
// on top of (or replace) the heuristic via the same [Extractor]
// interface.
//
// Source on the returned [Enrichment] is [SourceLLM] so the
// retrospective grader (gm-s47n.8) treats heuristic + LLM
// extraction the same way; the operator can always override and
// the override stamps SourceOperator.
type HeuristicExtractor struct {
	// MaxTargets caps the number of target globs the extractor
	// emits. 0 → 8 (a sensible default; bigger sets are usually
	// noise from a body that lists every file in passing).
	MaxTargets int

	// MaxConcepts caps the number of concept tags. 0 → 8.
	MaxConcepts int

	// PathPrefixes overrides the recognized "looks like a path"
	// prefix set. nil → DefaultHeuristicPathPrefixes. Non-nil with
	// length 0 disables the prefix-gated half (only fenced paths
	// are extracted).
	PathPrefixes []string
}

// DefaultHeuristicPathPrefixes are the directory names the bare-
// path matcher accepts. Tuned for the gemba repo layout; operators
// with different conventions override via [HeuristicExtractor.PathPrefixes].
var DefaultHeuristicPathPrefixes = []string{
	"internal/",
	"cmd/",
	"web/src/",
	"web/",
	"docs/",
	"testing/",
	"scripts/",
	".github/",
}

// Compile-time interface check.
var _ Extractor = HeuristicExtractor{}

// fencedPathRE matches path-shaped tokens inside a backtick fence.
// The path body permits letters/digits/. /-_/ — enough for typical
// source paths without accidentally swallowing inline code.
var fencedPathRE = regexp.MustCompile("`([A-Za-z0-9_./-]+)`")

// barePathTokenRE picks up unfenced tokens shaped like paths. The
// extractor still gates the result on a prefix from PathPrefixes
// to avoid false positives (e.g. "v1.0.0" would match here but
// fails the prefix gate).
var barePathTokenRE = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_./-]*[A-Za-z0-9_/]`)

// Extract implements [Extractor].
func (h HeuristicExtractor) Extract(_ context.Context, in BeadInput) (Enrichment, error) {
	maxT := h.MaxTargets
	if maxT <= 0 {
		maxT = 8
	}
	maxC := h.MaxConcepts
	if maxC <= 0 {
		maxC = 8
	}
	prefixes := h.PathPrefixes
	if prefixes == nil {
		prefixes = DefaultHeuristicPathPrefixes
	}

	corpus := strings.Join([]string{in.Title, in.Body, in.Spec}, "\n")

	out := Enrichment{
		BeadID: in.BeadID,
		Source: SourceLLM, // heuristic = automated extraction; same downstream treatment
	}
	out.Targets = extractTargets(corpus, prefixes, maxT)
	out.Concepts = extractConcepts(corpus, in.Vocabulary, maxC)
	return out, nil
}

// extractTargets pulls path-shaped tokens out of corpus, deduped
// and stably sorted. The fenced matches always win; bare-token
// matches fill remaining capacity.
func extractTargets(corpus string, prefixes []string, max int) []string {
	seen := make(map[string]bool)
	out := make([]string, 0)

	add := func(p string) bool {
		p = normalizeTarget(p)
		if p == "" || seen[p] {
			return false
		}
		seen[p] = true
		out = append(out, p)
		return len(out) >= max
	}

	// Fenced first — operators wrap real paths in backticks.
	for _, m := range fencedPathRE.FindAllStringSubmatch(corpus, -1) {
		if len(m) < 2 {
			continue
		}
		if !looksLikePath(m[1]) {
			continue
		}
		if add(m[1]) {
			sort.Strings(out)
			return out
		}
	}

	// Then bare tokens, prefix-gated. We deliberately skip pure
	// version strings (no slash, dotted) so "v1.0.0" doesn't slip
	// through the prefix gate disabled case.
	for _, raw := range barePathTokenRE.FindAllString(corpus, -1) {
		if !strings.Contains(raw, "/") {
			continue
		}
		if !hasAnyPrefix(raw, prefixes) {
			continue
		}
		if add(raw) {
			sort.Strings(out)
			return out
		}
	}

	sort.Strings(out)
	return out
}

// extractConcepts picks vocabulary tags that appear in corpus as
// whole words. Match is case-insensitive against the canonical
// (lower-kebab) form of each vocabulary entry; underscores and
// dashes are treated as word boundaries.
func extractConcepts(corpus string, vocabulary []string, max int) []string {
	if len(vocabulary) == 0 {
		return nil
	}
	lc := strings.ToLower(corpus)

	type hit struct {
		term  string
		first int
	}
	hits := make([]hit, 0)
	seen := make(map[string]bool)

	for _, term := range vocabulary {
		canon := normalizeConcept(term)
		if canon == "" || seen[canon] {
			continue
		}
		seen[canon] = true

		// Try the canonical form, an underscore variant, and the
		// space-separated variant — operators write "react query"
		// or "react_query" or "react-query" interchangeably.
		variants := []string{canon}
		if strings.Contains(canon, "-") {
			variants = append(variants, strings.ReplaceAll(canon, "-", "_"))
			variants = append(variants, strings.ReplaceAll(canon, "-", " "))
		}
		first := -1
		for _, v := range variants {
			idx := indexWordBoundary(lc, v)
			if idx >= 0 && (first < 0 || idx < first) {
				first = idx
			}
		}
		if first >= 0 {
			hits = append(hits, hit{term: canon, first: first})
		}
	}

	// Stable order: by first occurrence in corpus, then by term —
	// preserves the author's order of mention for ties so the most
	// "obvious" tag (the one named earliest) lands first.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].first != hits[j].first {
			return hits[i].first < hits[j].first
		}
		return hits[i].term < hits[j].term
	})

	out := make([]string, 0, max)
	for _, h := range hits {
		if len(out) >= max {
			break
		}
		out = append(out, h.term)
	}
	sort.Strings(out)
	return out
}

// looksLikePath rejects fenced tokens that are clearly not source
// paths (single segments without an extension, version strings).
// Tuned to err on the side of accepting — false negatives just
// reduce the operator's free starter targets; false positives
// pollute the conflict scorer.
func looksLikePath(s string) bool {
	if s == "" {
		return false
	}
	if !strings.Contains(s, "/") {
		// Single segment must end in a real file extension — at
		// least one letter after the final dot. That admits
		// "auth.go", "Topbar.tsx", "config.yaml" while rejecting
		// "v1.2.3" (the post-dot suffix is digits-only).
		dot := strings.LastIndex(s, ".")
		if dot < 0 || dot == len(s)-1 {
			return false
		}
		ext := s[dot+1:]
		hasAlpha := false
		for _, r := range ext {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				hasAlpha = true
				break
			}
		}
		return hasAlpha
	}
	return true
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// indexWordBoundary returns the first index of needle in haystack
// where the surrounding characters are NOT letters / digits /
// underscores — i.e. a real word boundary. Returns -1 when the
// needle isn't present at a boundary.
func indexWordBoundary(haystack, needle string) int {
	if needle == "" {
		return -1
	}
	from := 0
	for from < len(haystack) {
		idx := strings.Index(haystack[from:], needle)
		if idx < 0 {
			return -1
		}
		abs := from + idx
		if isBoundary(haystack, abs-1) && isBoundary(haystack, abs+len(needle)) {
			return abs
		}
		from = abs + 1
	}
	return -1
}

func isBoundary(s string, idx int) bool {
	if idx < 0 || idx >= len(s) {
		return true
	}
	c := s[idx]
	if c >= 'a' && c <= 'z' {
		return false
	}
	if c >= '0' && c <= '9' {
		return false
	}
	if c == '_' {
		return false
	}
	return true
}
