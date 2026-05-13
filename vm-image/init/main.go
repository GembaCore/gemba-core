//go:build linux && init
// +build linux,init

// gemba-vm-init is PID 1 inside a Gemba microVM. It runs the
// boot-time bring-up that bridges the Firecracker host and the
// dispatched agent process.
//
// Lifecycle:
//
//  1. Bring up loopback + eth0 (DHCP via the busybox `udhcpc` client
//     installed by Buildroot).
//  2. Fetch MMDS payload from 169.254.169.254 — JSON containing
//     workspace context, secrets, and the agent command to dispatch.
//     The host puts this in MMDS via the Firecracker API before boot
//     (gm-o9t8.3.2.5 wiring).
//  3. Mount /workspace — virtio-fs at tag "workspace" via the FUSE
//     `virtiofsd` client; if that fails (older kernel / no virtio-fs)
//     fall back to mounting /dev/vdb (a virtio-blk volume the host
//     prepared).
//  4. Export secrets into the child process env. We deliberately do
//     NOT write them to /etc/environment or any persistent file —
//     they live in this process's memory until exec.
//  5. Exec the dispatched agent command (e.g. `claude-code` with the
//     bead context). The agent inherits PID 1, so on exit we resume
//     control, write the run summary back into MMDS, and trigger an
//     orderly shutdown (reboot syscall with LINUX_REBOOT_CMD_POWER_OFF)
//     so the host sees a clean stop.
//
// This is a sketch implementation. Real production hardening
// (timeouts, retries on MMDS fetch, signal forwarding, proper PID-1
// reaping of orphans, oom_score_adj tuning) is tracked in
// gm-o9t8.3.2.6 and is intentionally NOT done here.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// version is injected by -ldflags at build time (see the Buildroot
// package). Surfaced via MMDS summary so the host can audit which
// init revision actually ran.
var version = "dev"

// mmdsURL is Firecracker's fixed MMDS endpoint.
const mmdsURL = "http://169.254.169.254/"

// bootSpec is the contract between the host's pre-boot MMDS publish
// step and this init binary. Keep in sync with internal/dispatch/
// (consumer side); the JSON tags are the wire format.
type bootSpec struct {
	WorkspaceID string            `json:"workspace_id"`
	BeadID      string            `json:"bead_id"`
	AgentCmd    []string          `json:"agent_cmd"`
	Env         map[string]string `json:"env"`
	Secrets     map[string]string `json:"secrets"`
	WorkspaceFS string            `json:"workspace_fs"` // "virtiofs" | "virtioblk"
	VirtiofsTag string            `json:"virtiofs_tag"`
}

// runSummary is posted back into MMDS at exit so the host can drain
// the result without scraping stdout.
type runSummary struct {
	Version    string `json:"init_version"`
	ExitCode   int    `json:"exit_code"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Error      string `json:"error,omitempty"`
}

func main() {
	started := time.Now().UTC().Format(time.RFC3339)
	exitCode, err := run()
	finished := time.Now().UTC().Format(time.RFC3339)

	summary := runSummary{
		Version:    version,
		ExitCode:   exitCode,
		StartedAt:  started,
		FinishedAt: finished,
	}
	if err != nil {
		summary.Error = err.Error()
		fmt.Fprintf(os.Stderr, "gemba-vm-init: %v\n", err)
	}
	_ = postSummary(summary)
	powerOff()
}

// run executes the bring-up sequence and returns the agent's exit
// code. A non-nil error means we failed before/while launching the
// agent; that's surfaced through MMDS but still triggers shutdown.
func run() (int, error) {
	if err := bringUpNetwork(); err != nil {
		return 1, fmt.Errorf("network: %w", err)
	}
	spec, err := fetchBootSpec()
	if err != nil {
		return 1, fmt.Errorf("mmds: %w", err)
	}
	if err := mountWorkspace(spec); err != nil {
		return 1, fmt.Errorf("workspace mount: %w", err)
	}
	return execAgent(spec)
}

// bringUpNetwork is a thin wrapper around busybox `ip` + `udhcpc`.
// We accept failures non-fatally on the loopback bring-up because
// many Firecracker configs disable DHCP and inject a static IP via
// kernel cmdline; in that case eth0 is already up by the time we
// run.
func bringUpNetwork() error {
	_ = exec.Command("/sbin/ip", "link", "set", "lo", "up").Run()
	_ = exec.Command("/sbin/ip", "link", "set", "eth0", "up").Run()
	// Best-effort DHCP; ignore error.
	_ = exec.Command("/sbin/udhcpc", "-i", "eth0", "-n", "-q").Run()
	return nil
}

// fetchBootSpec retrieves and decodes the MMDS payload. Retries for
// up to 5 seconds — MMDS is occasionally not ready on the very first
// boot tick.
func fetchBootSpec() (*bootSpec, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(mmdsURL)
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		var spec bootSpec
		if err := json.Unmarshal(body, &spec); err != nil {
			return nil, fmt.Errorf("parse mmds: %w", err)
		}
		return &spec, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout")
	}
	return nil, lastErr
}

// mountWorkspace honors spec.WorkspaceFS. virtiofs uses the kernel's
// "virtiofs" fs type (no userspace daemon required on the guest when
// the host runs virtiofsd). virtioblk treats /dev/vdb as an ext4
// volume and mounts it directly.
func mountWorkspace(spec *bootSpec) error {
	if err := os.MkdirAll("/workspace", 0o755); err != nil {
		return err
	}
	switch spec.WorkspaceFS {
	case "", "virtiofs":
		tag := spec.VirtiofsTag
		if tag == "" {
			tag = "workspace"
		}
		// virtiofs uses the tag as the source.
		if err := syscall.Mount(tag, "/workspace", "virtiofs", 0, ""); err != nil {
			// Fall back to virtio-blk if virtiofs isn't available
			// (older kernels, or the host opted out).
			return syscall.Mount("/dev/vdb", "/workspace", "ext4", 0, "")
		}
		return nil
	case "virtioblk":
		return syscall.Mount("/dev/vdb", "/workspace", "ext4", 0, "")
	default:
		return fmt.Errorf("unknown workspace_fs %q", spec.WorkspaceFS)
	}
}

// execAgent forks/execs the agent command and returns its exit code.
// Secrets are passed via env only; they never touch disk. We chdir
// into /workspace so the agent sees the mounted workspace as its CWD
// — this is what every supported agent expects.
func execAgent(spec *bootSpec) (int, error) {
	if len(spec.AgentCmd) == 0 {
		return 1, fmt.Errorf("empty agent_cmd in MMDS spec")
	}
	cmd := exec.Command(spec.AgentCmd[0], spec.AgentCmd[1:]...)
	cmd.Dir = "/workspace"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = nil

	env := os.Environ()
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	for k, v := range spec.Secrets {
		env = append(env, k+"="+v)
	}
	env = append(env, "GEMBA_WORKSPACE_ID="+spec.WorkspaceID, "GEMBA_BEAD_ID="+spec.BeadID)
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

// postSummary writes the run summary back to MMDS. Firecracker MMDS
// accepts PUT/PATCH on the same endpoint; we use PATCH so we don't
// clobber the host's boot spec (the host reads our /summary subtree
// after the VM exits).
func postSummary(s runSummary) error {
	body, err := json.Marshal(map[string]any{"summary": s})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PATCH", mmdsURL, ioReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ioReader wraps a byte slice as an io.Reader without importing
// bytes; keeps the binary small. (stdlib bytes is tiny, but the
// pattern documents what's wire-format vs in-memory.)
type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func ioReader(b []byte) io.Reader { return &byteReader{b: b} }

// powerOff issues the Linux reboot syscall with the power-off command.
// Firecracker treats guest power-off as a clean shutdown signal and
// closes the VMM cleanly.
func powerOff() {
	// LINUX_REBOOT_CMD_POWER_OFF = 0x4321FEDC
	_ = syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF)
	// If reboot returned (shouldn't), spin so PID 1 never exits —
	// kernel panic on PID 1 exit is worse than a hang.
	for {
		time.Sleep(time.Hour)
	}
}
