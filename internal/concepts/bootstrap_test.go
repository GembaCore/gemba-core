package concepts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrap_UnionsSourcesAndDedupes(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "internal/concepts/doc.go"),
		"package concepts\n")
	mustWrite(t, filepath.Join(root, "internal/auth/auth.go"),
		"package auth\n")
	mustWrite(t, filepath.Join(root, "web/src/App.tsx"),
		`<Route path="/board" /> <Route path="/auth" />`)

	v, res, err := Bootstrap(context.Background(), root, DefaultSources(), DefaultBootstrapOpts())
	if err != nil {
		t.Fatal(err)
	}
	// concepts + auth + board (auth is in both sources but dedupes)
	wantNames := []string{"auth", "board", "concepts"}
	gotNames := termNames(v)
	if !equalSets(gotNames, wantNames) {
		t.Errorf("vocabulary names = %v, want %v", gotNames, wantNames)
	}
	if res.Total != len(wantNames) {
		t.Errorf("result.Total = %d, want %d", res.Total, len(wantNames))
	}
}

func TestBootstrap_RespectsMaxCap(t *testing.T) {
	root := t.TempDir()
	for _, pkg := range []string{"a", "b", "c", "d", "e"} {
		mustWrite(t, filepath.Join(root, "internal", pkg, "x.go"), "package "+pkg+"\n")
	}
	v, res, err := Bootstrap(context.Background(), root, []BootstrapSource{GoPackagesSource{}},
		BootstrapOpts{Max: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Terms) != 3 {
		t.Errorf("Max=3 should cap; got %d terms", len(v.Terms))
	}
	if res.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", res.Skipped)
	}
}

func TestBootstrap_FirstSourceWinsOnDuplicate(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "internal/auth/auth.go"), "package auth\n")
	mustWrite(t, filepath.Join(root, "web/src/App.tsx"), `<Route path="/auth" />`)
	// Pass go-packages first; route-prefixes second. The go-packages
	// label should win.
	v, _, err := Bootstrap(context.Background(), root,
		[]BootstrapSource{GoPackagesSource{}, RoutePrefixesSource{}},
		DefaultBootstrapOpts())
	if err != nil {
		t.Fatal(err)
	}
	t1, ok := v.Find("auth")
	if !ok {
		t.Fatal("missing auth term")
	}
	if t1.Source != "bootstrap:go-packages" {
		t.Errorf("first source should win: got %q", t1.Source)
	}
}

func TestBootstrap_NoSpaPathOK(t *testing.T) {
	// An SPA-less workspace should produce candidates from the
	// remaining sources without error.
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "internal/auth/auth.go"), "package auth\n")
	v, res, err := Bootstrap(context.Background(), root, DefaultSources(), DefaultBootstrapOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Terms) == 0 {
		t.Error("expected at least the go-packages source to fire")
	}
	if len(res.Errors) != 0 {
		t.Errorf("missing optional sources should not produce errors; got %v", res.Errors)
	}
}

func TestGoPackagesSource_SkipsMainAndTestPackages(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "cmd/foo/main.go"), "package main\n")
	mustWrite(t, filepath.Join(root, "internal/foo/foo_test.go"), "package foo_test\n")
	mustWrite(t, filepath.Join(root, "internal/keep/keep.go"), "package keep\n")
	cs, err := GoPackagesSource{}.Extract(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Name != "keep" {
		t.Errorf("expected only [keep], got %+v", cs)
	}
}

func TestRoutePrefixesSource_StripsWildcards(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "web/src/App.tsx"),
		`<Route path="/board" /> <Route path="/board/*" /> <Route path="/grid" />`)
	cs, err := RoutePrefixesSource{}.Extract(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := candidateNames(cs)
	want := []string{"board", "grid"}
	if !equalSets(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestFixtureTaxonomySource_DirectoryNames(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"smoke", "chrome", "drawers", "_skip", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, "testing/e2e/specs", dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cs, err := FixtureTaxonomySource{}.Extract(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := candidateNames(cs)
	want := []string{"chrome", "drawers", "smoke"}
	if !equalSets(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

// Helpers shared across concept tests.
func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func termNames(v *Vocabulary) []string {
	out := make([]string, 0, len(v.Terms))
	for _, t := range v.Terms {
		out = append(out, t.Name)
	}
	return out
}

func candidateNames(cs []Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func equalSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]bool, len(a))
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}
