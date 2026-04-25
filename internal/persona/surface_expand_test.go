package persona

import (
	"reflect"
	"testing"
)

func TestExpandPaths_ReplacesHomeGopathGoroot(t *testing.T) {
	env := map[string]string{
		"HOME":   "/Users/op",
		"GOPATH": "/Users/op/go",
		"GOROOT": "/usr/local/go",
	}
	in := Surface{
		Cwd:               "/Users/op/repo",
		WorkspaceMetadata: "/Users/op/.gemba",
		ToolingPaths: []string{
			"$HOME/.gitconfig",
			"$GOPATH/pkg/mod",
			"$GOROOT",
			"${HOME}/.cargo",
			"/etc/ssl/cert.pem",
		},
	}
	got := ExpandPaths(in, env)
	want := []string{
		"/Users/op/.gitconfig",
		"/Users/op/go/pkg/mod",
		"/usr/local/go",
		"/Users/op/.cargo",
		"/etc/ssl/cert.pem",
	}
	if !reflect.DeepEqual(got.ToolingPaths, want) {
		t.Errorf("ToolingPaths = %v, want %v", got.ToolingPaths, want)
	}
	// Cwd / WorkspaceMetadata round-trip when they have no $.
	if got.Cwd != "/Users/op/repo" {
		t.Errorf("Cwd = %q, want /Users/op/repo", got.Cwd)
	}
	if got.WorkspaceMetadata != "/Users/op/.gemba" {
		t.Errorf("WorkspaceMetadata = %q", got.WorkspaceMetadata)
	}
}

func TestExpandPaths_DropsPathsWithUnsetVar(t *testing.T) {
	// Only HOME is set — $GOPATH / $GOROOT references must drop the
	// whole path, not produce "/pkg/mod" or "" garbage that the
	// container backend would silently accept.
	env := map[string]string{"HOME": "/Users/op"}
	in := Surface{
		ToolingPaths: []string{
			"$HOME/.gitconfig",
			"$GOPATH/pkg/mod",
			"$GOROOT",
			"$HOME/go/pkg/mod",
		},
	}
	got := ExpandPaths(in, env)
	want := []string{
		"/Users/op/.gitconfig",
		"/Users/op/go/pkg/mod",
	}
	if !reflect.DeepEqual(got.ToolingPaths, want) {
		t.Errorf("ToolingPaths = %v, want %v", got.ToolingPaths, want)
	}
}

func TestExpandPaths_PreservesAdditionalReadsAndWrites(t *testing.T) {
	env := map[string]string{"HOME": "/h"}
	in := Surface{
		AdditionalWrites: []string{"$HOME/scratch", "/tmp/build"},
		AdditionalReads:  []string{"$HOME/.aws", "/data"},
	}
	got := ExpandPaths(in, env)
	if !reflect.DeepEqual(got.AdditionalWrites, []string{"/h/scratch", "/tmp/build"}) {
		t.Errorf("AdditionalWrites = %v", got.AdditionalWrites)
	}
	if !reflect.DeepEqual(got.AdditionalReads, []string{"/h/.aws", "/data"}) {
		t.Errorf("AdditionalReads = %v", got.AdditionalReads)
	}
}

func TestExpandPaths_EmptyVarValueDropsPath(t *testing.T) {
	// An env entry that exists but is empty is treated as unset —
	// GOROOT="" on a system where Go is missing should not produce
	// a literal "" path that the backend would emit as a relative
	// mount source.
	env := map[string]string{"HOME": "/h", "GOROOT": ""}
	in := Surface{ToolingPaths: []string{"$GOROOT", "$HOME/x"}}
	got := ExpandPaths(in, env)
	if !reflect.DeepEqual(got.ToolingPaths, []string{"/h/x"}) {
		t.Errorf("ToolingPaths = %v, want [/h/x]", got.ToolingPaths)
	}
}

func TestExpandPaths_LiteralDollar(t *testing.T) {
	// A bare $ at end-of-string or followed by a non-identifier
	// character is treated as a literal $. Path stays intact.
	env := map[string]string{"HOME": "/h"}
	in := Surface{ToolingPaths: []string{"/literal/$", "$HOME/data", "/odd$/path"}}
	got := ExpandPaths(in, env)
	want := []string{"/literal/$", "/h/data", "/odd$/path"}
	if !reflect.DeepEqual(got.ToolingPaths, want) {
		t.Errorf("ToolingPaths = %v, want %v", got.ToolingPaths, want)
	}
}

func TestExpandPaths_UnterminatedBraceIsLiteral(t *testing.T) {
	env := map[string]string{"HOME": "/h"}
	in := Surface{ToolingPaths: []string{"/before/${HOMEunterminated"}}
	got := ExpandPaths(in, env)
	if got.ToolingPaths[0] != "/before/${HOMEunterminated" {
		t.Errorf("ToolingPaths[0] = %q, want literal passthrough", got.ToolingPaths[0])
	}
}

func TestEnvFromOS_PicksTrackedVars(t *testing.T) {
	env := map[string]string{
		"HOME":   "/h",
		"GOPATH": "/g",
		"USER":   "op",
		// GOROOT deliberately empty — must drop, not pass through.
		"GOROOT":   "",
		"OTHERVAR": "ignored",
	}
	got := EnvFromOS(func(k string) string { return env[k] })
	if got["HOME"] != "/h" || got["GOPATH"] != "/g" || got["USER"] != "op" {
		t.Errorf("EnvFromOS = %v", got)
	}
	if _, present := got["GOROOT"]; present {
		t.Errorf("EnvFromOS should drop empty GOROOT, got %v", got)
	}
	if _, present := got["OTHERVAR"]; present {
		t.Errorf("EnvFromOS leaked untracked var OTHERVAR: %v", got)
	}
}
