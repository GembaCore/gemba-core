package projectshape

import (
	"strings"
	"testing"
)

func TestShape_IsZero(t *testing.T) {
	if !(Shape{}).IsZero() {
		t.Fatal("zero-value Shape should report IsZero() == true")
	}
	s := Shape{Name: "x"}
	if s.IsZero() {
		t.Fatal("Shape with Name should not be IsZero")
	}
}

func TestShape_RenderZeroIsEmpty(t *testing.T) {
	// Critical contract: a dispatcher with no configured shape must
	// be able to call Render() and get the empty string so the
	// project-values layer simply omits the contribution.
	if got := (Shape{}).Render(); got != "" {
		t.Errorf("zero Shape.Render() = %q, want empty", got)
	}
}

func TestShape_RenderIncludesAllFields(t *testing.T) {
	s := Shape{
		Name:              "two-tier-spa",
		Description:       "Browser-side SPA plus backend API.",
		Tiers:             []string{"spa frontend", "http api backend"},
		RequestPath:       "browser → https → api → db",
		Tooling:           []string{"vite", "go"},
		DeploymentSurface: "single VPS",
	}
	got := s.Render()
	wants := []string{
		"two-tier-spa",
		"Browser-side SPA plus backend API.",
		"spa frontend, http api backend",
		"browser → https → api → db",
		"vite, go",
		"single VPS",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("Render() missing %q\nfull output:\n%s", w, got)
		}
	}
}

func TestShape_RenderOmitsEmptyFields(t *testing.T) {
	// Render skips fields that are empty so a sparsely-populated
	// shape doesn't litter the prompt with "Tooling: " followed by
	// nothing.
	s := Shape{Name: "minimal"}
	got := s.Render()
	if !strings.Contains(got, "Project shape: minimal") {
		t.Errorf("Render() must include the name header, got:\n%s", got)
	}
	for _, banned := range []string{"Tiers:", "Request path:", "Tooling:", "Deployment:"} {
		if strings.Contains(got, banned) {
			t.Errorf("Render() unexpectedly emitted %q for sparse shape:\n%s", banned, got)
		}
	}
}
