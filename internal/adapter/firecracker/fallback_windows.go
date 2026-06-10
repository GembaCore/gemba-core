//go:build windows

package firecracker

import (
	"os/exec"
	"syscall"
)

// newFallbackProcAttr returns a zero SysProcAttr on Windows — process
// groups don't translate cleanly to the Win32 job-object model and
// Stop relies on Process.Kill (via ctx cancel) for force-termination.
func newFallbackProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// terminateFallback on Windows has no SIGTERM equivalent for child
// processes spawned via exec.CommandContext. The Stop path falls
// through to ctx.cancel() which delivers a Kill. We return nil here
// so the timeout branch in Stop is the only termination path.
func terminateFallback(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return nil
}
