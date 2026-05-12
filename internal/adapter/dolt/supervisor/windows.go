//go:build windows

package supervisor

import (
	"bufio"
	"io"
	"log/slog"
	"os/exec"
	"syscall"
)

// newProcAttr returns a zero SysProcAttr on Windows. Process-group
// isolation isn't available the same way — Stop relies on
// Process.Kill semantics instead.
func newProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func terminateProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Windows has no SIGTERM; Kill is the only portable option.
	return cmd.Process.Kill()
}

func pipeToSlog(r io.ReadCloser, log *slog.Logger, level slog.Level, source string) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		log.Log(nil, level, "dolt subprocess output",
			"source", source, "line", scanner.Text())
	}
}
