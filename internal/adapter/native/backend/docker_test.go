package backend

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// stubDocker returns a Docker with an injected runner. `calls` captures
// every invocation so tests can assert argv shape.
func stubDocker(t *testing.T, outputs map[string][]byte) (*Docker, *[][]string) {
	t.Helper()
	var calls [][]string
	return &Docker{
		binary: "/fake/docker",
		runner: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string(nil), args...))
			return outputs[args[0]], nil
		},
	}, &calls
}

func TestBuildRunArgs_SecurityDefaults(t *testing.T) {
	d := &Docker{binary: "/fake/docker"}
	args := d.buildRunArgs(SpawnSpec{
		Image:          "test:latest",
		Cwd:            "/work",
		ReadOnlyRootfs: true,
	})
	joined := strings.Join(args, " ")

	// Every default that gm-root.15.7 codifies has to land on the
	// wire unless the caller opted out. This test is the contract.
	mustContain := []string{
		"--read-only",
		"--tmpfs /tmp:size=128m,mode=1777",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--network none",
		"--pids-limit 512",
		"--workdir /work",
	}
	for _, s := range mustContain {
		if !strings.Contains(joined, s) {
			t.Errorf("missing default %q in args: %s", s, joined)
		}
	}
	// Image must be the last positional arg before any command.
	if args[len(args)-1] != "test:latest" {
		t.Errorf("image not last: %v", args)
	}
}

func TestBuildRunArgs_NetworkOverride(t *testing.T) {
	d := &Docker{binary: "/fake/docker"}
	args := d.buildRunArgs(SpawnSpec{
		Image:   "img",
		Network: NetworkPolicy{Mode: "bridge:work"},
	})
	if !containsPair(args, "--network", "bridge:work") {
		t.Errorf("network override not applied: %v", args)
	}
	// Must not also contain --network none.
	if strings.Count(strings.Join(args, " "), "--network") != 1 {
		t.Errorf("duplicate --network: %v", args)
	}
}

func TestBuildRunArgs_LimitsAndUserNS(t *testing.T) {
	d := &Docker{binary: "/fake/docker"}
	args := d.buildRunArgs(SpawnSpec{
		Image:  "img",
		UserNS: "1000:1000",
		Limits: Limits{CPUs: 2.5, Memory: 4 << 30, PidsLimit: 256},
	})
	if !containsPair(args, "--cpus", "2.5") {
		t.Errorf("--cpus not applied: %v", args)
	}
	if !containsPair(args, "--user", "1000:1000") {
		t.Errorf("--user not applied: %v", args)
	}
	if !containsPair(args, "--pids-limit", "256") {
		t.Errorf("--pids-limit override not applied: %v", args)
	}
}

func TestBuildRunArgs_MountsAndSecrets(t *testing.T) {
	d := &Docker{binary: "/fake/docker"}
	args := d.buildRunArgs(SpawnSpec{
		Image: "img",
		Mounts: []Mount{
			{Src: "/host/work", Dst: "/work", Mode: "rw"},
			{Src: "/host/bridge", Dst: "/opt/bridge", Mode: "ro"},
			{Dst: "/scratch", Mode: "tmpfs", Size: "64m"},
		},
		Secrets: []string{"api_key"},
	})
	joined := strings.Join(args, " ")
	mustContain := []string{
		"type=bind,src=/host/work,dst=/work",
		"type=bind,src=/host/bridge,dst=/opt/bridge,readonly",
		"--tmpfs /scratch:size=64m",
		"--tmpfs /run/secrets:mode=0400,size=1m",
	}
	for _, s := range mustContain {
		if !strings.Contains(joined, s) {
			t.Errorf("missing %q in args: %s", s, joined)
		}
	}
}

func TestBuildRunArgs_LabelsSorted(t *testing.T) {
	// Sorted argv keeps logs + tests diff-friendly. The backend
	// stamps gemba.session/gemba.agent upstream; here we just assert
	// ordering.
	d := &Docker{binary: "/fake/docker"}
	args := d.buildRunArgs(SpawnSpec{
		Image: "img",
		Labels: map[string]string{
			"gemba.session": "sess-1",
			"gemba.agent":   "claude-sandboxed",
		},
	})
	joined := strings.Join(args, " ")
	idxAgent := strings.Index(joined, "gemba.agent=claude-sandboxed")
	idxSess := strings.Index(joined, "gemba.session=sess-1")
	if idxAgent < 0 || idxSess < 0 || idxAgent > idxSess {
		t.Errorf("labels not sorted alphabetically: %s", joined)
	}
}

func TestParseDockerPs(t *testing.T) {
	sample := "abc123\timg:1\t\"bash\"\tsess-1\tgemba.session=sess-1,gemba.agent=claude\n" +
		"def456\timg:2\t\"claude\"\tsess-2\tgemba.session=sess-2\n"
	got, err := parseDockerPs(sample)
	if err != nil {
		t.Fatalf("parseDockerPs: %v", err)
	}
	want := []Pane{
		{Kind: KindContainer, ID: "abc123", Image: "img:1", Command: "bash", Title: "sess-1",
			Labels: map[string]string{"gemba.session": "sess-1", "gemba.agent": "claude"}},
		{Kind: KindContainer, ID: "def456", Image: "img:2", Command: "claude", Title: "sess-2",
			Labels: map[string]string{"gemba.session": "sess-2"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseDockerPs mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestSpawnPane_RequiresImage(t *testing.T) {
	d, _ := stubDocker(t, nil)
	_, err := d.SpawnPane(context.Background(), SpawnSpec{})
	if err == nil || !strings.Contains(err.Error(), "Image") {
		t.Errorf("want Image-required error, got %v", err)
	}
}

func TestKillAndPause(t *testing.T) {
	d, calls := stubDocker(t, nil)
	ctx := context.Background()
	if err := d.Kill(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := d.Pause(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := d.Unpause(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	wantVerbs := []string{"rm", "pause", "unpause"}
	if len(*calls) != len(wantVerbs) {
		t.Fatalf("want %d calls, got %d", len(wantVerbs), len(*calls))
	}
	for i, w := range wantVerbs {
		if (*calls)[i][0] != w {
			t.Errorf("call %d: want verb %q, got %v", i, w, (*calls)[i])
		}
	}
}

func TestCapturePane_DefaultsTailTo200(t *testing.T) {
	d, calls := stubDocker(t, nil)
	if _, err := d.CapturePane(context.Background(), "abc", 0); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(*calls))
	}
	if !containsPair((*calls)[0], "--tail", "200") {
		t.Errorf("default tail not 200: %v", (*calls)[0])
	}
}

func TestPausableInterface(t *testing.T) {
	// Compile-time assertion: Docker satisfies Pausable. If this
	// breaks, the OrchestrationPlane's PauseSession path silently
	// falls back to KindUnsupported — caught here instead.
	var _ Pausable = (*Docker)(nil)
}

// containsPair asserts a flag/value appears adjacent in args.
func containsPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
