package bd

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

var embeddedLockRetryDelays = []time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
}

func runBdCommand(ctx context.Context, path string, dir string, args ...string) ([]byte, error) {
	var lastOut []byte
	var lastErr error
	for attempt := 0; attempt <= len(embeddedLockRetryDelays); attempt++ {
		cmd := exec.CommandContext(ctx, path, args...)
		if dir != "" {
			cmd.Dir = dir
		}
		out, err := cmd.Output()
		if err == nil {
			return out, nil
		}
		lastOut = outputWithStderr(out, err)
		lastErr = err
		if !isEmbeddedDoltLockError(err) || attempt == len(embeddedLockRetryDelays) {
			return lastOut, err
		}
		timer := time.NewTimer(embeddedLockRetryDelays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return lastOut, ctx.Err()
		case <-timer.C:
		}
	}
	return lastOut, lastErr
}

func outputWithStderr(out []byte, err error) []byte {
	if len(out) > 0 {
		return out
	}
	if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
		return exitErr.Stderr
	}
	return out
}

func isEmbeddedDoltLockError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if exitErr, ok := err.(*exec.ExitError); ok {
		msg += "\n" + string(exitErr.Stderr)
	}
	return strings.Contains(msg, "another process holds the exclusive lock") ||
		strings.Contains(msg, "embedded backend supports only one writer at a time")
}
