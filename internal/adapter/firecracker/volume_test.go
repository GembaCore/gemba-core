package firecracker

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestSpec_VolumeMountValidation covers gm-o9t8.3.2.4 — Spec.validate()
// must reject missing HostPath, relative GuestPath, and HostPath that
// doesn't exist on disk. A well-formed mount must validate cleanly.
func TestSpec_VolumeMountValidation(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "data")
	if err := os.WriteFile(good, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name    string
		mount   VolumeMount
		wantErr string
	}{
		{"missing host", VolumeMount{GuestPath: "/workspace"}, "HostPath is required"},
		{"missing guest", VolumeMount{HostPath: good}, "GuestPath is required"},
		{"relative guest", VolumeMount{HostPath: good, GuestPath: "workspace"}, "must be absolute"},
		{"missing on disk", VolumeMount{HostPath: filepath.Join(tmp, "nope"), GuestPath: "/workspace"}, "HostPath"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := Spec{WorkspaceID: "ws", VolumeMounts: []VolumeMount{tc.mount}}
			err := spec.validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}

	t.Run("happy path", func(t *testing.T) {
		spec := Spec{
			WorkspaceID: "ws",
			VolumeMounts: []VolumeMount{
				{HostPath: good, GuestPath: "/workspace"},
			},
		}
		if err := spec.validate(); err != nil {
			t.Fatalf("good spec returned %v", err)
		}
	})
}

// TestFallback_VolumeMountReadable launches a subprocess that asserts
// it can read the file inside the mounted volume via $GEMBA_VOL_0_PATH.
// gm-o9t8.3.2.4 contract: the subprocess sees the host data through
// the symlink staged into the per-VM tmpdir.
func TestFallback_VolumeMountReadable(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback-only test")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	tmp := t.TempDir()
	data := filepath.Join(tmp, "payload")
	if err := os.WriteFile(data, []byte("hello-from-volume"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sup := NewSupervisor(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := Spec{
		WorkspaceID: "vol-ws",
		VolumeMounts: []VolumeMount{
			{HostPath: data, GuestPath: "/workspace/payload"},
		},
		// Exit 0 iff the subprocess can read the payload through the
		// symlink and the env hint matches.
		FallbackCommand: `test "$GEMBA_VOL_0_GUEST" = "/workspace/payload" && test "$(cat "$GEMBA_VOL_0_PATH")" = "hello-from-volume"`,
	}
	vm, err := sup.Start(ctx, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case waitErr, ok := <-sup.Wait(ctx, vm):
		if ok && waitErr != nil {
			t.Fatalf("subprocess could not read volume mount: %v", waitErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Wait did not fire")
	}
	if err := sup.Stop(ctx, vm, 1*time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
