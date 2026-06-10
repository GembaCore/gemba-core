//go:build windows

package bridge

import "os"

// inodeOf returns 0 on Windows. NTFS exposes a "file index" via
// GetFileInformationByHandle, but it's only stable within a single
// open handle, can change after defrag, and the tailer's rotation-
// detection works fine with size-only signals on Windows hosts that
// rotate logs (rotation atomically replaces the file, so the file
// the tailer is reading goes EOF; the next stat picks up the new
// one). gm-lyo8.
func inodeOf(_ os.FileInfo) uint64 {
	return 0
}
