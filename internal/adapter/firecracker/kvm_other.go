//go:build !linux

package firecracker

// KVMAvailable always returns false on non-Linux platforms. /dev/kvm
// is a Linux kernel construct; macOS/Windows hosts can never satisfy
// it, so the fallback subprocess shim is the only viable path.
func KVMAvailable() bool { return false }
