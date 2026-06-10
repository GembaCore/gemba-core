package egress

import (
	"strings"
	"testing"
)

func TestBuiltinTemplates_GitHubOnly(t *testing.T) {
	tpl := NewBuiltinTemplates().Lookup("github-only")
	if tpl.Name != "github-only" {
		t.Fatalf("name: got %q", tpl.Name)
	}
	if tpl.NetworkDefault != NetworkDeny {
		t.Fatalf("expected deny default, got %q", tpl.NetworkDefault)
	}
	if !contains(tpl.AllowedFQDNs, "github.com") {
		t.Fatalf("github.com missing from bundle: %+v", tpl.AllowedFQDNs)
	}
	if contains(tpl.AllowedFQDNs, "pypi.org") {
		t.Fatal("github-only must not include pypi.org")
	}
}

func TestBuiltinTemplates_PyPINPM(t *testing.T) {
	tpl := NewBuiltinTemplates().Lookup("pypi+npm")
	for _, host := range []string{"github.com", "pypi.org", "registry.npmjs.org"} {
		if !contains(tpl.AllowedFQDNs, host) {
			t.Fatalf("pypi+npm missing %s", host)
		}
	}
	if tpl.NetworkDefault != NetworkDeny {
		t.Fatalf("expected deny default")
	}
}

func TestBuiltinTemplates_WideOpen(t *testing.T) {
	tpl := NewBuiltinTemplates().Lookup("wide-open")
	if tpl.NetworkDefault != NetworkAllow {
		t.Fatalf("wide-open must default to allow, got %q", tpl.NetworkDefault)
	}
	if len(tpl.AllowedFQDNs) != 0 {
		t.Fatalf("wide-open should not carry per-host rules, got %v", tpl.AllowedFQDNs)
	}
}

func TestBuiltinTemplates_DefaultIsGithubOnly(t *testing.T) {
	def := NewBuiltinTemplates().Lookup("default")
	if def.Name != "github-only" {
		t.Fatalf("default should alias github-only, got %q", def.Name)
	}
}

func TestBuiltinTemplates_UnknownFallsBackToDefault(t *testing.T) {
	tpl := NewBuiltinTemplates().Lookup("no-such-bundle")
	if tpl.Name != "github-only" {
		t.Fatalf("unknown bundle should fall back to github-only, got %q", tpl.Name)
	}
}

func TestBuiltinTemplates_EmptyFallsBackToDefault(t *testing.T) {
	tpl := NewBuiltinTemplates().Lookup("")
	if tpl.Name != "github-only" {
		t.Fatalf("empty should fall back to github-only, got %q", tpl.Name)
	}
}

func TestBuiltinTemplates_CaseInsensitive(t *testing.T) {
	if NewBuiltinTemplates().Lookup("WIDE-OPEN").Name != "wide-open" {
		t.Fatal("lookup must be case-insensitive")
	}
}

func TestTemplateNames(t *testing.T) {
	names := TemplateNames()
	for _, want := range []string{"github-only", "pypi+npm", "wide-open", "default"} {
		found := false
		for _, n := range names {
			if strings.EqualFold(n, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("TemplateNames missing %q", want)
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
