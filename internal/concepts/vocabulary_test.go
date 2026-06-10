package concepts

import (
	"strings"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	cases := []struct{ in, out string }{
		{"react-query", "react-query"},
		{"React-Query", "react-query"},
		{"react query", "react-query"},
		{"react/query", "react-query"},
		{"react.query", "react-query"},
		{"react:query", "react-query"},
		{"react__query", "react-query"},
		{"  React Query  ", "react-query"},
		{"react  query", "react-query"},
		{"foo (bar)", "foo-bar"},
		{"trailing-", "trailing"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.out {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestVocabulary_AddIsIdempotent(t *testing.T) {
	v := &Vocabulary{}
	if _, fresh := v.Add(Term{Name: "auth", Source: "bootstrap:packages"}); !fresh {
		t.Errorf("first Add should report fresh=true")
	}
	if _, fresh := v.Add(Term{Name: "auth", Source: "operator"}); fresh {
		t.Errorf("re-adding same name should report fresh=false")
	}
	if got := len(v.Terms); got != 1 {
		t.Errorf("expected 1 term after dup add, got %d", got)
	}
	if v.Terms[0].Source != "bootstrap:packages" {
		t.Errorf("re-add should not overwrite source: got %q", v.Terms[0].Source)
	}
}

func TestVocabulary_AddNormalizesName(t *testing.T) {
	v := &Vocabulary{}
	v.Add(Term{Name: "React-Query"})
	if v.Terms[0].Name != "react-query" {
		t.Errorf("name not normalized: %q", v.Terms[0].Name)
	}
	if _, ok := v.Find("react query"); !ok {
		t.Errorf("Find should normalize lookups")
	}
}

func TestVocabulary_FindResolvesAliases(t *testing.T) {
	v := &Vocabulary{}
	v.Add(Term{Name: "react-query", Aliases: []string{"rq"}})
	t1, ok := v.Find("rq")
	if !ok {
		t.Fatalf("alias lookup failed")
	}
	if t1.Name != "react-query" {
		t.Errorf("alias resolved to wrong term: %q", t1.Name)
	}
}

func TestVocabulary_RetireMarksAndStamps(t *testing.T) {
	v := &Vocabulary{}
	v.Add(Term{Name: "foo"})
	if !v.Retire("foo") {
		t.Fatalf("Retire should report true")
	}
	if !v.Terms[0].Retired || v.Terms[0].RetiredAt == nil {
		t.Errorf("term not marked retired: %+v", v.Terms[0])
	}
	if got := len(v.Active()); got != 0 {
		t.Errorf("Active should hide retired, got %d", got)
	}
	if _, ok := v.Find("foo"); !ok {
		t.Errorf("Find must still resolve retired terms (history rewrites need them)")
	}
}

func TestVocabulary_MergeFoldsAliasesAndRetiresFrom(t *testing.T) {
	v := &Vocabulary{}
	v.Add(Term{Name: "rq"})
	v.Add(Term{Name: "react-query"})
	to, err := v.Merge("rq", "react-query")
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if to.Name != "react-query" {
		t.Errorf("Merge returned wrong surviving term: %q", to.Name)
	}
	if !contains(to.Aliases, "rq") {
		t.Errorf("Merge should add from-name as alias on to: %+v", to.Aliases)
	}
	from, _ := v.Find("rq")
	if !from.Retired {
		t.Errorf("from term should be retired after merge")
	}
}

func TestVocabulary_MergeChainsAliasesForward(t *testing.T) {
	// a → b → c: c should know about a and b.
	v := &Vocabulary{}
	v.Add(Term{Name: "a"})
	v.Add(Term{Name: "b"})
	v.Add(Term{Name: "c"})
	if _, err := v.Merge("a", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Merge("b", "c"); err != nil {
		t.Fatal(err)
	}
	cT, _ := v.Find("c")
	if !contains(cT.Aliases, "a") || !contains(cT.Aliases, "b") {
		t.Errorf("c should carry both a and b as aliases: %+v", cT.Aliases)
	}
	// Lookups for a still resolve to c via the alias chain (b's
	// alias-on-c resolves transitively because b carried a).
}

func TestVocabulary_MergeRejectsSameTerm(t *testing.T) {
	v := &Vocabulary{}
	v.Add(Term{Name: "foo"})
	if _, err := v.Merge("foo", "foo"); err == nil {
		t.Error("merging a term with itself must be an error")
	}
}

func TestVocabulary_MergeRejectsMissingTerms(t *testing.T) {
	v := &Vocabulary{}
	v.Add(Term{Name: "foo"})
	if _, err := v.Merge("foo", "missing"); err == nil {
		t.Error("merge to missing term must be an error")
	}
	if _, err := v.Merge("missing", "foo"); err == nil {
		t.Error("merge from missing term must be an error")
	}
}

func TestVocabulary_RenameKeepsOldNameAsAlias(t *testing.T) {
	v := &Vocabulary{}
	v.Add(Term{Name: "old-name"})
	if _, err := v.Rename("old-name", "new-name"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	t1, ok := v.Find("new-name")
	if !ok {
		t.Fatalf("Find new name failed")
	}
	if !contains(t1.Aliases, "old-name") {
		t.Errorf("rename should leave old name as alias: %+v", t1.Aliases)
	}
	// Lookups for the old name still resolve.
	if _, ok := v.Find("old-name"); !ok {
		t.Errorf("old name should still resolve after rename")
	}
}

func TestVocabulary_RenameRejectsCollision(t *testing.T) {
	v := &Vocabulary{}
	v.Add(Term{Name: "a"})
	v.Add(Term{Name: "b"})
	_, err := v.Rename("a", "b")
	if err == nil {
		t.Fatal("rename onto existing term must be an error")
	}
	if !strings.Contains(err.Error(), "use Merge") {
		t.Errorf("error should hint at Merge: %v", err)
	}
}

func TestVocabulary_SortStable(t *testing.T) {
	v := &Vocabulary{
		Terms: []Term{
			{Name: "zeta"},
			{Name: "alpha"},
			{Name: "mu"},
		},
	}
	v.Sort()
	if v.Terms[0].Name != "alpha" || v.Terms[2].Name != "zeta" {
		t.Errorf("Sort failed: %+v", v.Terms)
	}
}

func TestTermFieldsStampedOnFreshAdd(t *testing.T) {
	v := &Vocabulary{}
	before := time.Now().UTC()
	v.Add(Term{Name: "auth"})
	t1 := v.Terms[0]
	if t1.CreatedAt.Before(before) {
		t.Errorf("CreatedAt not stamped: %v", t1.CreatedAt)
	}
	if t1.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt not stamped")
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
