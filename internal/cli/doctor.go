package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that gemba prerequisites are satisfied",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

type check struct {
	name string
	run  func() error
}

func runDoctor() error {
	checks := []check{
		{"gt on PATH (or gc for Gas City alpha)", checkGtOrGc},
		{"bd on PATH", func() error {
			_, err := exec.LookPath("bd")
			return err
		}},
		{"inside a Gas Town HQ (or Gas City workspace)", checkWorkspace},
		{"default port 7666 available", func() error {
			return checkPortFree(7666)
		}},
	}

	var failed int
	for _, c := range checks {
		if err := c.run(); err != nil {
			fmt.Printf("  ✗ %s: %v\n", c.name, err)
			failed++
		} else {
			fmt.Printf("  ✓ %s\n", c.name)
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	fmt.Println("\nall checks passed")
	return nil
}

// checkGtOrGc: Gas Town v1.0 is the stable runtime Gemba ships against
// today; Gas City is the alpha follow-on. `gc` is also shipped by graphviz,
// so we probe the binary to confirm it's actually Gas City before trusting it.
func checkGtOrGc() error {
	if _, err := exec.LookPath("gt"); err == nil {
		return nil
	}
	if path, err := exec.LookPath("gc"); err == nil {
		if looksLikeGasCity(path) {
			return nil
		}
	}
	return errors.New("neither Gas Town (gt) nor Gas City (gc) found on " +
		"PATH; install Gas Town 1.0 (stable, recommended) or Gas City " +
		"(alpha) first")
}

func looksLikeGasCity(path string) bool {
	cmd := exec.Command(path, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	s := string(out)
	return containsFold(s, "city.toml") ||
		containsFold(s, "gascity") ||
		containsFold(s, "Gas City") ||
		(containsFold(s, "controller") && containsFold(s, "pack"))
}

func containsFold(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	hl := []byte(haystack)
	nl := []byte(needle)
	for i := 0; i <= len(hl)-len(nl); i++ {
		match := true
		for j := 0; j < len(nl); j++ {
			a := hl[i+j]
			b := nl[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func checkWorkspace() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	for dir := cwd; ; {
		if isDir(filepath.Join(dir, ".gt")) ||
			isDir(filepath.Join(dir, "rigs")) {
			return nil
		}
		if isDir(filepath.Join(dir, ".gc")) ||
			isFile(filepath.Join(dir, "city.toml")) {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return errors.New("no .gt/, rigs/, .gc/, or city.toml found in " +
				"cwd or any ancestor; cd to your Gas Town HQ (typically ~/gt) " +
				"or Gas City workspace (typically ~/my-city) first")
		}
		dir = parent
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func checkPortFree(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d in use: %w", port, err)
	}
	_ = l.Close()
	return nil
}
