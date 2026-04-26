//go:build !windows

package bridge

import (
	"os"
	"syscall"
)

// inodeOf returns the POSIX inode number for a FileInfo, used to
// detect log rotation (inode change) vs truncate (inode stable,
// size shrinks). The Windows build of this package returns 0 from
// inode_windows.go and the tailer falls back to size-only detection.
func inodeOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
