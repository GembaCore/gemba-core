//go:build linux

package firecracker

import (
	"os"
	"syscall"
)

// KVMAvailable reports whether /dev/kvm exists and the current
// process can open it. Used by tests to skip the real Firecracker
// path on Linux boxes that lack hardware virtualization (most CI
// runners). Returns false on permission failures even if the device
// node exists.
func KVMAvailable() bool {
	fi, err := os.Stat("/dev/kvm")
	if err != nil {
		return false
	}
	if fi.Mode()&os.ModeDevice == 0 {
		return false
	}
	// Try a non-destructive read-only open; an EACCES here means the
	// kernel module is loaded but the user isn't in the `kvm` group.
	// Treat as unavailable — Firecracker won't be able to ioctl().
	f, err := os.OpenFile("/dev/kvm", os.O_RDONLY, 0)
	if err != nil {
		// errors.Is(err, syscall.EACCES) is fine; we don't distinguish.
		_ = syscall.EACCES
		return false
	}
	_ = f.Close()
	return true
}
