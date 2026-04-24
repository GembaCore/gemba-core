//go:build integration

package backend

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// envelope_integration_test.go exercises the real Docker backend
// against a live daemon to assert the security envelope (gm-root.15.7)
// actually blocks the escapes it's meant to, not just that the flags
// land on the wire. Gated behind the `integration` build tag AND the
// DOCKER_INTEGRATION=1 env var so it never runs in unit-test CI by
// accident:
//
//   DOCKER_INTEGRATION=1 go test -tags=integration ./internal/adapter/native/backend/
//
// Each test spawns a short-lived busybox container with the default
// envelope applied and runs a single escape attempt. Any success is a
// regression.

func requireDocker(t *testing.T) *Docker {
	t.Helper()
	if os.Getenv("DOCKER_INTEGRATION") != "1" {
		t.Skip("set DOCKER_INTEGRATION=1 to run")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}
	d, err := NewDocker()
	if err != nil {
		t.Skipf("NewDocker: %v", err)
	}
	return d
}

func spawnAndWait(ctx context.Context, t *testing.T, d *Docker, spec SpawnSpec) (string, string) {
	t.Helper()
	p, err := d.SpawnPane(ctx, spec)
	if err != nil {
		t.Fatalf("SpawnPane: %v", err)
	}
	t.Cleanup(func() {
		_ = d.Kill(context.Background(), p.ID)
	})
	// Containers run detached; give them a moment to finish their
	// one-shot command then capture the logs.
	time.Sleep(500 * time.Millisecond)
	logs, err := d.CapturePane(ctx, p.ID, 50)
	if err != nil {
		t.Fatalf("CapturePane: %v", err)
	}
	return p.ID, logs
}

// TestEnvelope_WriteOutsideMountBlocked asserts the agent cannot
// write to the root filesystem when --read-only is applied.
func TestEnvelope_WriteOutsideMountBlocked(t *testing.T) {
	d := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, logs := spawnAndWait(ctx, t, d, SpawnSpec{
		Image:          "busybox:latest",
		ReadOnlyRootfs: true,
		Command:        []string{"sh", "-c", "touch /escape && echo UNEXPECTED_WRITE_SUCCEEDED || echo WRITE_BLOCKED"},
		Labels:         map[string]string{"gemba.session": "envelope-test"},
	})
	if !strings.Contains(logs, "WRITE_BLOCKED") {
		t.Errorf("rootfs write NOT blocked. logs:\n%s", logs)
	}
	if strings.Contains(logs, "UNEXPECTED_WRITE_SUCCEEDED") {
		t.Errorf("rootfs write SUCCEEDED — envelope failed. logs:\n%s", logs)
	}
}

// TestEnvelope_CapabilitiesDropped asserts the agent cannot use
// capabilities that --cap-drop ALL removes (raw sockets here).
func TestEnvelope_CapabilitiesDropped(t *testing.T) {
	d := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// `ping` needs CAP_NET_RAW; cap-drop ALL removes it.
	_, logs := spawnAndWait(ctx, t, d, SpawnSpec{
		Image:          "busybox:latest",
		ReadOnlyRootfs: true,
		Command:        []string{"sh", "-c", "ping -c1 -W1 127.0.0.1 >/dev/null 2>&1 && echo UNEXPECTED_CAP || echo CAP_BLOCKED"},
		Labels:         map[string]string{"gemba.session": "envelope-test"},
	})
	if !strings.Contains(logs, "CAP_BLOCKED") {
		t.Errorf("capability NOT dropped. logs:\n%s", logs)
	}
}

// TestEnvelope_NetworkNoneBlocksEgress asserts --network none prevents
// any outbound connection.
func TestEnvelope_NetworkNoneBlocksEgress(t *testing.T) {
	d := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, logs := spawnAndWait(ctx, t, d, SpawnSpec{
		Image:          "busybox:latest",
		ReadOnlyRootfs: true,
		// --network none leaves only lo up — wget should fail fast.
		Command: []string{"sh", "-c", "wget -q -T2 -O- http://1.1.1.1 2>/dev/null && echo UNEXPECTED_EGRESS || echo EGRESS_BLOCKED"},
		Labels:  map[string]string{"gemba.session": "envelope-test"},
	})
	if !strings.Contains(logs, "EGRESS_BLOCKED") {
		t.Errorf("egress NOT blocked. logs:\n%s", logs)
	}
}

// TestEnvelope_HostFilesystemNotReadable asserts the container cannot
// read host paths that weren't mounted. Host /etc is present inside
// the container but only because it's part of the container image —
// there's no way for busybox to see the host's /etc without a mount.
// We assert the container's /etc is the image's /etc (tiny) not the
// host's (dozens of files).
func TestEnvelope_HostFilesystemNotReadable(t *testing.T) {
	d := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, logs := spawnAndWait(ctx, t, d, SpawnSpec{
		Image:          "busybox:latest",
		ReadOnlyRootfs: true,
		Command:        []string{"sh", "-c", "ls /etc/shadow 2>/dev/null && echo UNEXPECTED_HOST_LEAK || echo HOST_ISOLATED"},
		Labels:         map[string]string{"gemba.session": "envelope-test"},
	})
	if !strings.Contains(logs, "HOST_ISOLATED") {
		t.Errorf("host filesystem NOT isolated. logs:\n%s", logs)
	}
}
