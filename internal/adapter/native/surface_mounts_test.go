package native

import (
	"reflect"
	"testing"

	"github.com/GembaCore/gemba-core/internal/adapter/native/backend"
	"github.com/GembaCore/gemba-core/internal/persona"
)

// existsAll is the test-friendly path predicate that says yes to
// every path. Used when the test isn't exercising the missing-host-
// path branch.
func existsAll(string) bool { return true }

// existsOnly returns a predicate that admits only the named paths.
// Lets tests pin "host doesn't have $HOME/.cargo" without touching
// the real filesystem.
func existsOnly(paths ...string) func(string) bool {
	allowed := make(map[string]bool, len(paths))
	for _, p := range paths {
		allowed[p] = true
	}
	return func(p string) bool { return allowed[p] }
}

func TestSurfaceMounts_CwdIsRw(t *testing.T) {
	s := persona.Surface{Cwd: "/work/repo"}
	mounts, skipped := SurfaceMounts(s, existsAll)
	if len(skipped) != 0 {
		t.Fatalf("unexpected skips: %v", skipped)
	}
	want := []backend.Mount{{Src: "/work/repo", Dst: "/work/repo", Mode: "rw"}}
	if !reflect.DeepEqual(mounts, want) {
		t.Errorf("mounts = %+v, want %+v", mounts, want)
	}
}

func TestSurfaceMounts_SiblingsAndMetadataAndToolingAreRo(t *testing.T) {
	s := persona.Surface{
		Cwd:               "/work/repo",
		SiblingReads:      []string{"/work/sibling"},
		WorkspaceMetadata: "/work/.gemba",
		ToolingPaths:      []string{"/Users/op/.gitconfig"},
	}
	mounts, _ := SurfaceMounts(s, existsAll)

	// Sorted by Dst — /Users < /work/.gemba < /work/repo < /work/sibling.
	want := []backend.Mount{
		{Src: "/Users/op/.gitconfig", Dst: "/Users/op/.gitconfig", Mode: "ro"},
		{Src: "/work/.gemba", Dst: "/work/.gemba", Mode: "ro"},
		{Src: "/work/repo", Dst: "/work/repo", Mode: "rw"},
		{Src: "/work/sibling", Dst: "/work/sibling", Mode: "ro"},
	}
	if !reflect.DeepEqual(mounts, want) {
		t.Errorf("mounts = %+v\nwant     %+v", mounts, want)
	}
}

func TestSurfaceMounts_AdditionalReadsAndWrites(t *testing.T) {
	s := persona.Surface{
		Cwd:              "/cwd",
		AdditionalWrites: []string{"/extra/write"},
		AdditionalReads:  []string{"/extra/read"},
	}
	mounts, _ := SurfaceMounts(s, existsAll)
	want := []backend.Mount{
		{Src: "/cwd", Dst: "/cwd", Mode: "rw"},
		{Src: "/extra/read", Dst: "/extra/read", Mode: "ro"},
		{Src: "/extra/write", Dst: "/extra/write", Mode: "rw"},
	}
	if !reflect.DeepEqual(mounts, want) {
		t.Errorf("mounts = %+v\nwant    %+v", mounts, want)
	}
}

func TestSurfaceMounts_DupBetweenReadsAndWritesPrefersRw(t *testing.T) {
	// /shared lands in both AdditionalWrites and AdditionalReads.
	// AllowedReads() includes everything writes does, so the path
	// shows up twice in the surface accessors. The translator must
	// emit one :rw mount, not a :rw + :ro pair.
	s := persona.Surface{
		Cwd:              "/cwd",
		AdditionalWrites: []string{"/shared"},
		AdditionalReads:  []string{"/shared"},
	}
	mounts, _ := SurfaceMounts(s, existsAll)
	for _, m := range mounts {
		if m.Src == "/shared" && m.Mode != "rw" {
			t.Errorf("/shared mount mode = %q, want rw", m.Mode)
		}
	}
	count := 0
	for _, m := range mounts {
		if m.Src == "/shared" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected /shared to appear once, saw %d times", count)
	}
}

func TestSurfaceMounts_SkipsMissingHostPaths(t *testing.T) {
	s := persona.Surface{
		Cwd:          "/work/repo",
		ToolingPaths: []string{"/Users/op/.cargo", "/etc/ssl/cert.pem"},
	}
	// Only the cwd + cert exist on the host — .cargo is missing.
	mounts, skipped := SurfaceMounts(s, existsOnly("/work/repo", "/etc/ssl/cert.pem"))

	// Skipped contains the missing path so the spawn driver can log
	// it (decision deferred to caller).
	if !reflect.DeepEqual(skipped, []string{"/Users/op/.cargo"}) {
		t.Errorf("skipped = %v, want [/Users/op/.cargo]", skipped)
	}
	for _, m := range mounts {
		if m.Src == "/Users/op/.cargo" {
			t.Errorf("missing path should not be mounted: %+v", m)
		}
	}
}

func TestSurfaceMounts_DefaultPredicateUsedWhenNil(t *testing.T) {
	// nil predicate must default to DefaultPathExists (os.Stat).
	// Pass a bogus path that won't exist to confirm: it should be
	// skipped instead of panicking on a nil-call.
	s := persona.Surface{Cwd: "/this/path/should/not/exist/anywhere/12345"}
	mounts, skipped := SurfaceMounts(s, nil)
	if len(mounts) != 0 {
		t.Errorf("missing path should produce no mounts, got %+v", mounts)
	}
	if len(skipped) != 1 || skipped[0] != s.Cwd {
		t.Errorf("skipped = %v, want [%q]", skipped, s.Cwd)
	}
}

func TestMergeSurfaceMounts_AdditiveLayersOperatorOnSurface(t *testing.T) {
	operator := []backend.Mount{
		{Src: "/host/scratch", Dst: "/scratch", Mode: "rw"},
	}
	surface := []backend.Mount{
		{Src: "/work/repo", Dst: "/work/repo", Mode: "rw"},
	}
	got, dropped := MergeSurfaceMounts(operator, surface, SurfaceModeAdditive)
	want := []backend.Mount{
		{Src: "/host/scratch", Dst: "/scratch", Mode: "rw"},
		{Src: "/work/repo", Dst: "/work/repo", Mode: "rw"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged = %+v\nwant   %+v", got, want)
	}
	if len(dropped) != 0 {
		t.Errorf("nothing should be dropped in additive mode, got %+v", dropped)
	}
}

func TestMergeSurfaceMounts_AdditiveDropsConflictingOperatorEntries(t *testing.T) {
	// Operator declared a :ro mount on a path the surface owns :rw.
	// The surface entry MUST win — operator can't demote a write.
	operator := []backend.Mount{
		{Src: "/host/repo", Dst: "/work/repo", Mode: "ro"},
	}
	surface := []backend.Mount{
		{Src: "/work/repo", Dst: "/work/repo", Mode: "rw"},
	}
	got, dropped := MergeSurfaceMounts(operator, surface, SurfaceModeAdditive)
	if len(got) != 1 || got[0].Mode != "rw" {
		t.Errorf("merged = %+v, want single rw mount", got)
	}
	if len(dropped) != 1 || dropped[0].Src != "/host/repo" {
		t.Errorf("dropped = %+v, want operator entry", dropped)
	}
}

func TestMergeSurfaceMounts_ExclusiveDropsOperator(t *testing.T) {
	operator := []backend.Mount{
		{Src: "/host/scratch", Dst: "/scratch", Mode: "rw"},
		{Src: "/host/cache", Dst: "/cache", Mode: "ro"},
	}
	surface := []backend.Mount{
		{Src: "/work/repo", Dst: "/work/repo", Mode: "rw"},
	}
	got, dropped := MergeSurfaceMounts(operator, surface, SurfaceModeExclusive)
	if !reflect.DeepEqual(got, surface) {
		t.Errorf("exclusive merged = %+v, want surface only %+v", got, surface)
	}
	if !reflect.DeepEqual(dropped, operator) {
		t.Errorf("dropped = %+v, want %+v", dropped, operator)
	}
}

func TestParseSurfaceMode(t *testing.T) {
	cases := []struct {
		in   string
		want SurfaceMode
		ok   bool
	}{
		{"", SurfaceModeAdditive, true},
		{"additive", SurfaceModeAdditive, true},
		{"exclusive", SurfaceModeExclusive, true},
		{"weird", SurfaceModeAdditive, false},
	}
	for _, tc := range cases {
		got, ok := ParseSurfaceMode(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseSurfaceMode(%q) = (%q, %v), want (%q, %v)",
				tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
