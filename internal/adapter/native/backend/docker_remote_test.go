package backend

import "testing"

func TestIsRemoteDockerHost(t *testing.T) {
	cases := map[string]bool{
		"":                         false,
		"unix:///var/run/docker":   false,
		"fd://":                    false,
		"ssh://op@vps":             true,
		"SSH://Op@VPS":             true, // case-insensitive
		"tcp://10.0.0.1:2376":      true,
		"http://localhost:2375":    true,
		"https://docker.local:443": true,
		"pigeon://carrier":         false, // unknown scheme is safer as "local"
	}
	for in, want := range cases {
		if got := IsRemoteDockerHost(in); got != want {
			t.Errorf("IsRemoteDockerHost(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLocalMountWarning(t *testing.T) {
	type tc struct {
		src     string
		remote  bool
		wantHit string // substring expected in warning, empty = no warning
	}
	cases := []tc{
		{"/var/lib/gemba/ws", true, ""},       // portable absolute path
		{"/var/lib/gemba/ws", false, ""},      // local mode skips warnings
		{"~/work", true, "~"},                 // home dir leaks
		{"$HOME/work", true, "env var"},       // env var leaks
		{"./relative", true, "relative path"}, // relative leaks
		{"~/work", false, ""},                 // local mode — no warning
		{"", true, ""},                        // empty src — nothing to warn about
	}
	for _, c := range cases {
		got := LocalMountWarning(c.src, c.remote)
		if c.wantHit == "" && got != "" {
			t.Errorf("LocalMountWarning(%q, %v) = %q, want empty", c.src, c.remote, got)
		}
		if c.wantHit != "" && got == "" {
			t.Errorf("LocalMountWarning(%q, %v) = empty, want containing %q", c.src, c.remote, c.wantHit)
		}
	}
}

func TestRemoteMode_UsesEnv(t *testing.T) {
	t.Setenv("DOCKER_HOST", "ssh://user@vps")
	d := &Docker{binary: "/fake/docker"}
	if !d.RemoteMode() {
		t.Error("RemoteMode should pick up DOCKER_HOST=ssh://")
	}
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	if d.RemoteMode() {
		t.Error("RemoteMode should not flag unix socket as remote")
	}
}
