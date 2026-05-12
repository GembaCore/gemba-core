//go:build windows

package workspacelayout

// syscallUmask is a no-op on Windows; the test that uses it skips on
// Windows so this stub only exists to keep the build green.
func syscallUmask(mask int) int { return 0 }
