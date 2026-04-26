package concepts

import (
	"sort"
	"strings"
	"time"
	"unicode"
)

// Term is one entry in the controlled vocabulary. Names are
// normalized lower-kebab-case so a bead carrying "React-Query"
// matches a vocabulary term "react-query".
type Term struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Description string `json:"description,omitempty"`
	// Aliases are names that merged into this term. Kept on the
	// surviving term so lookups for the retired name still resolve
	// without walking the suggestions log.
	Aliases   []string  `json:"aliases,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Retired terms stay in the vocabulary so historical rewrites
	// can find them; lookups for active terms filter via [Vocabulary.Active].
	Retired   bool       `json:"retired,omitempty"`
	RetiredAt *time.Time `json:"retired_at,omitempty"`
}

// Vocabulary is the closed set of terms, stably ordered by Name. The
// in-memory shape mirrors the on-disk vocabulary.json so the file
// stays diff-friendly for review in beads dolt commits.
type Vocabulary struct {
	Terms []Term `json:"terms"`
}

// Find returns the term with the given canonical name (or any
// alias), and a bool reporting whether it was found. Includes
// retired terms — historical rewrites need them.
func (v *Vocabulary) Find(name string) (*Term, bool) {
	canon := Normalize(name)
	for i := range v.Terms {
		if v.Terms[i].Name == canon {
			return &v.Terms[i], true
		}
		for _, a := range v.Terms[i].Aliases {
			if a == canon {
				return &v.Terms[i], true
			}
		}
	}
	return nil, false
}

// Active returns just the non-retired terms, copied so callers can't
// mutate vocabulary state.
func (v *Vocabulary) Active() []Term {
	out := make([]Term, 0, len(v.Terms))
	for _, t := range v.Terms {
		if !t.Retired {
			out = append(out, t)
		}
	}
	return out
}

// Sort sorts the vocabulary's terms by name in place. Storage layer
// calls this before serializing so the on-disk order is stable.
func (v *Vocabulary) Sort() {
	sort.SliceStable(v.Terms, func(i, j int) bool {
		return v.Terms[i].Name < v.Terms[j].Name
	})
}

// Add inserts a term, returning the inserted term and a bool that
// reports whether it was new. Re-adding an existing name is a no-op
// that returns the existing term — bootstrap sources can run
// multiple times without piling up duplicates.
func (v *Vocabulary) Add(t Term) (*Term, bool) {
	t.Name = Normalize(t.Name)
	if existing, ok := v.Find(t.Name); ok {
		return existing, false
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	v.Terms = append(v.Terms, t)
	return &v.Terms[len(v.Terms)-1], true
}

// Retire marks the named term as retired and stamps RetiredAt. No-op
// when the term is already retired; returns false when the name
// matches no term at all.
func (v *Vocabulary) Retire(name string) bool {
	t, ok := v.Find(name)
	if !ok {
		return false
	}
	if t.Retired {
		return true
	}
	now := time.Now().UTC()
	t.Retired = true
	t.RetiredAt = &now
	t.UpdatedAt = now
	return true
}

// Merge folds the `from` term into `to`: from's name is added as an
// alias on to, from is retired. Both terms must already exist in the
// vocabulary. Returns the surviving term and an error when either
// name is missing.
//
// Merge is the vocabulary-level half of the rewrite pipeline; the
// historical bead rewrite is [ApplyMerge] over a [BeadConceptStore].
func (v *Vocabulary) Merge(from, to string) (*Term, error) {
	canonFrom := Normalize(from)
	canonTo := Normalize(to)
	if canonFrom == canonTo {
		return nil, &VocabularyError{Reason: "merge from and to are the same term", Term: canonFrom}
	}
	fromT, ok := v.Find(canonFrom)
	if !ok {
		return nil, &VocabularyError{Reason: "from term not in vocabulary", Term: canonFrom}
	}
	toT, ok := v.Find(canonTo)
	if !ok {
		return nil, &VocabularyError{Reason: "to term not in vocabulary", Term: canonTo}
	}
	// Carry the retiring term's aliases over so a chain of merges
	// (a → b → c) leaves c knowing about a as well.
	for _, a := range fromT.Aliases {
		toT.Aliases = appendUnique(toT.Aliases, a)
	}
	toT.Aliases = appendUnique(toT.Aliases, fromT.Name)
	now := time.Now().UTC()
	toT.UpdatedAt = now
	fromT.Retired = true
	fromT.RetiredAt = &now
	fromT.UpdatedAt = now
	return toT, nil
}

// Rename swaps a term's canonical name in place. The old name is
// preserved as an alias so beads carrying the old name still resolve.
func (v *Vocabulary) Rename(from, to string) (*Term, error) {
	canonFrom := Normalize(from)
	canonTo := Normalize(to)
	if canonFrom == canonTo {
		return nil, &VocabularyError{Reason: "rename from and to are the same term", Term: canonFrom}
	}
	fromT, ok := v.Find(canonFrom)
	if !ok {
		return nil, &VocabularyError{Reason: "from term not in vocabulary", Term: canonFrom}
	}
	if _, exists := v.Find(canonTo); exists {
		return nil, &VocabularyError{Reason: "to term already exists; use Merge instead", Term: canonTo}
	}
	fromT.Aliases = appendUnique(fromT.Aliases, fromT.Name)
	fromT.Name = canonTo
	fromT.UpdatedAt = time.Now().UTC()
	return fromT, nil
}

// Normalize collapses a candidate name into the canonical
// lower-kebab-case form. Whitespace, underscores, slashes, and dots
// all become hyphens; runs of separators collapse to one; trailing
// separators are trimmed.
func Normalize(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(name))
	prevSep := true
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevSep = false
		case r == '-' || r == '_' || r == ' ' || r == '/' || r == '.' || r == ':':
			if !prevSep {
				b.WriteByte('-')
				prevSep = true
			}
		default:
			// Drop everything else (parens, brackets, punctuation).
		}
	}
	out := b.String()
	out = strings.TrimRight(out, "-")
	return out
}

// VocabularyError is the typed error returned by mutator methods so
// callers can branch on Reason without string parsing.
type VocabularyError struct {
	Reason string
	Term   string
}

func (e *VocabularyError) Error() string {
	if e.Term == "" {
		return e.Reason
	}
	return e.Reason + ": " + e.Term
}

func appendUnique(in []string, vs ...string) []string {
	seen := make(map[string]bool, len(in))
	for _, v := range in {
		seen[v] = true
	}
	for _, v := range vs {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		in = append(in, v)
	}
	return in
}
