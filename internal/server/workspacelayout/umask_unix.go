//go:build !windows

package workspacelayout

import "syscall"

// syscallUmask is the test-only shim that exposes the unix umask
// syscall. Build-tagged so the package still builds on Windows.
func syscallUmask(mask int) int { return syscall.Umask(mask) }
