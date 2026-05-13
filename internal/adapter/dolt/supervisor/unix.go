//go:build !windows

package supervisor

import (
	"bufio"
	"io"
	"log/slog"
	"os/exec"
	"syscall"
)

// newProcAttr returns SysProcAttr that puts the child into its own
// process group so a SIGTERM delivered to gemba via signal.NotifyContext
// is not implicitly forwarded to dolt — Stop() owns the dolt teardown.
func newProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// terminateProcess sends SIGTERM to the entire process group so any
// dolt children also receive it. Falls back to a plain process-level
// signal if the pgid signal isn't deliverable.
func terminateProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		// Negative pid = "deliver to the process group" in
		// the Unix signal API.
		if perr := syscall.Kill(-pgid, syscall.SIGTERM); perr == nil {
			return nil
		}
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}

// pipeToSlog drains a pipe line-by-line into the given logger. The
// caller is responsible for the goroutine's lifetime — the pipe
// closes when the subprocess exits, which terminates this loop.
func pipeToSlog(r io.ReadCloser, log *slog.Logger, level slog.Level, source string) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	// Bigger buffer in case dolt prints long lines on boot.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		log.Log(nil, level, "dolt subprocess output",
			"source", source, "line", scanner.Text())
	}
}
