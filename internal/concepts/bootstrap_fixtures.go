package concepts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// FixtureTaxonomySource emits the top-level subdirectory names of
// testing/e2e/specs/. The e2e library has already validated that
// each tier names a real surface (smoke / chrome / drawers / grid /
// realtime / etc.); reusing that taxonomy gives the concept set a
// language operators are already fluent in.
type FixtureTaxonomySource struct{}

func (FixtureTaxonomySource) Name() string { return "fixture-taxonomy" }

func (FixtureTaxonomySource) Extract(ctx context.Context, root string) ([]Candidate, error) {
	specsRoot := filepath.Join(root, "testing", "e2e", "specs")
	entries, err := os.ReadDir(specsRoot)
	if err != nil {
		// No e2e library in this workspace; no-op cleanly.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	out := make([]Candidate, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		out = append(out, Candidate{
			Name:        strings.ToLower(name),
			Description: "e2e tier: testing/e2e/specs/" + name,
		})
	}
	return out, nil
}
