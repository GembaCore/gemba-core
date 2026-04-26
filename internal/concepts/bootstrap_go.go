package concepts

import (
	"context"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// GoPackagesSource walks internal/ + cmd/ under root and emits a
// candidate per unique Go package name. Internal package names are
// the most stable signal of "what a contributor calls a thing" — a
// directory whose package is named `concepts` is observably about
// concepts whether or not the operator remembered to label it.
type GoPackagesSource struct{}

func (GoPackagesSource) Name() string { return "go-packages" }

func (GoPackagesSource) Extract(ctx context.Context, root string) ([]Candidate, error) {
	var roots []string
	for _, sub := range []string{"internal", "cmd"} {
		p := filepath.Join(root, sub)
		if info, err := stat(p); err == nil && info.IsDir() {
			roots = append(roots, p)
		}
	}
	if len(roots) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	out := []Candidate{}
	for _, r := range roots {
		err := filepath.WalkDir(r, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				// Permissions / vanished file — skip the entry, keep
				// walking the rest. A failed source is better than a
				// half-empty bootstrap.
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if d.IsDir() {
				name := d.Name()
				// Skip vendored / build / hidden / test fixtures.
				if name == "vendor" || name == "node_modules" || name == "testdata" ||
					strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
			if err != nil {
				// Malformed file — skip without escalating; bootstrap
				// is best-effort.
				return nil
			}
			pkg := file.Name.Name
			// Drop the conventional main + test packages — they don't
			// describe a concept.
			if pkg == "" || pkg == "main" || strings.HasSuffix(pkg, "_test") {
				return nil
			}
			canon := Normalize(pkg)
			if canon == "" || seen[canon] {
				return nil
			}
			seen[canon] = true
			out = append(out, Candidate{
				Name:        canon,
				Description: "Go package " + pkg + " (" + filepath.Dir(rel(root, path)) + ")",
			})
			return nil
		})
		if err != nil && err != context.Canceled {
			return out, err
		}
	}
	return out, nil
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}
