package concepts

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// RoutePrefixesSource extracts the top-level UI route names the SPA
// already exposes — every `<Route path="/foo" ...>` literal in
// web/src/App.tsx becomes a candidate. Routes are user-facing
// surfaces the operator already named, which makes them excellent
// concept seeds.
type RoutePrefixesSource struct{}

func (RoutePrefixesSource) Name() string { return "route-prefixes" }

// routePathRE matches `path="/segment"` (single or double quote).
// Wildcard suffixes and nested segments are stripped at extraction
// time so /board/* and /board both become "board".
var routePathRE = regexp.MustCompile(`path=["']\/([a-zA-Z][a-zA-Z0-9-]*)`)

func (RoutePrefixesSource) Extract(ctx context.Context, root string) ([]Candidate, error) {
	app := filepath.Join(root, "web", "src", "App.tsx")
	data, err := os.ReadFile(app)
	if err != nil {
		// SPA-less workspace: source no-ops cleanly.
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

	matches := routePathRE.FindAllStringSubmatch(string(data), -1)
	seen := make(map[string]bool)
	out := []Candidate{}
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		name := strings.ToLower(m[1])
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Candidate{
			Name:        name,
			Description: "SPA route /" + name,
		})
	}
	return out, nil
}
