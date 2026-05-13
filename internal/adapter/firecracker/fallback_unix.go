//go:build !linux && !windows

package firecracker

import (
	"os/exec"
	"syscall"
)

// newFallbackProcAttr puts the bash -c child into its own process
// group on Unix so a Ctrl-C delivered to the parent does not
// implicitly tear down the VM stand-in. Stop() owns teardown.
func newFallbackProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// terminateFallback delivers SIGTERM to the entire process group so
// any descendants of the bash -c command also receive it. Falls back
// to a plain process-level signal if pgid lookup fails.
func terminateFallback(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		if perr := syscall.Kill(-pgid, syscall.SIGTERM); perr == nil {
			return nil
		}
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}
