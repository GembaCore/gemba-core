package firecracker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeImageFixture stages a kernel, rootfs, and matching manifest in a
// temp dir and returns their paths. Callers can then mutate one of them
// before invoking VerifyImage to assert mismatch detection.
func writeImageFixture(t *testing.T) (dir, kernel, rootfs, manifest string) {
	t.Helper()
	dir = t.TempDir()
	kernel = filepath.Join(dir, "vmlinux")
	rootfs = filepath.Join(dir, "rootfs.ext4")
	manifest = filepath.Join(dir, "manifest.json")

	kData := []byte("fake-kernel-bytes-for-test\n")
	rData := []byte("fake-rootfs-bytes-for-test-padding\n")
	if err := os.WriteFile(kernel, kData, 0o644); err != nil {
		t.Fatalf("write kernel: %v", err)
	}
	if err := os.WriteFile(rootfs, rData, 0o644); err != nil {
		t.Fatalf("write rootfs: %v", err)
	}

	kSum := sha256.Sum256(kData)
	rSum := sha256.Sum256(rData)
	m := Manifest{
		KernelSHA256:    hex.EncodeToString(kSum[:]),
		RootfsSHA256:    hex.EncodeToString(rSum[:]),
		BuiltAt:         "2026-01-01T00:00:00Z",
		Builder:         "test",
		KernelVersion:   "6.1.0-test",
		RootfsSizeBytes: int64(len(rData)),
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(manifest, raw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return
}

func TestImageVerifyOK(t *testing.T) {
	_, k, r, m := writeImageFixture(t)
	if err := VerifyImage(k, r, m); err != nil {
		t.Fatalf("VerifyImage on a clean fixture should succeed; got %v", err)
	}
}

func TestImageVerifyDetectsRootfsCorruption(t *testing.T) {
	_, k, r, m := writeImageFixture(t)
	// Flip exactly one byte in the rootfs — the sha256 must change.
	data, err := os.ReadFile(r)
	if err != nil {
		t.Fatalf("read rootfs: %v", err)
	}
	data[0] ^= 0xFF
	if err := os.WriteFile(r, data, 0o644); err != nil {
		t.Fatalf("rewrite rootfs: %v", err)
	}
	if err := VerifyImage(k, r, m); err == nil {
		t.Fatalf("VerifyImage should fail on a 1-byte corrupted rootfs, got nil")
	}
}

func TestImageVerifyDetectsKernelCorruption(t *testing.T) {
	_, k, r, m := writeImageFixture(t)
	data, err := os.ReadFile(k)
	if err != nil {
		t.Fatalf("read kernel: %v", err)
	}
	data[len(data)-1] ^= 0x01
	if err := os.WriteFile(k, data, 0o644); err != nil {
		t.Fatalf("rewrite kernel: %v", err)
	}
	if err := VerifyImage(k, r, m); err == nil {
		t.Fatalf("VerifyImage should fail on a corrupted kernel, got nil")
	}
}

func TestImageLoadManifestRejectsBadHex(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(bad, []byte(`{"kernel_sha256":"nope","rootfs_sha256":"nope","built_at":"x","builder":"x","kernel_version":"x","rootfs_size_bytes":0}`), 0o644); err != nil {
		t.Fatalf("write bad manifest: %v", err)
	}
	if _, err := LoadManifest(bad); err == nil {
		t.Fatalf("LoadManifest should reject non-hex digest, got nil")
	}
}

func TestImageDefaultImagePathsHonorsEnv(t *testing.T) {
	dir, _, _, _ := writeImageFixture(t)
	t.Setenv("GEMBA_VM_DIR", dir)
	k, r, m, ok := DefaultImagePaths()
	if !ok {
		t.Fatalf("DefaultImagePaths should succeed when GEMBA_VM_DIR points at a populated dir")
	}
	if filepath.Dir(k) != dir || filepath.Dir(r) != dir || filepath.Dir(m) != dir {
		t.Fatalf("DefaultImagePaths returned wrong dir: %s %s %s (want %s)", k, r, m, dir)
	}
}

func TestImageDefaultImagePathsMissing(t *testing.T) {
	// Point env at an empty dir; clear home so the fallback can't
	// accidentally find a real install on the dev box.
	t.Setenv("GEMBA_VM_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if _, _, _, ok := DefaultImagePaths(); ok {
		t.Fatalf("DefaultImagePaths should fail when no candidate dir is populated")
	}
}
