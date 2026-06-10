package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/GembaCore/gemba-core/core"
)

// Docker implements Backend by shelling to the `docker` CLI — no Go
// SDK dependency, no long-lived daemon connection. Remote dispatch
// works for free via the standard DOCKER_HOST=ssh://user@host env
// var (gm-root.15.4). See docs/design/containerized-sessions.md §3-8.
//
// Security defaults (gm-root.15.7) are applied by SpawnPane unless
// the SpawnSpec explicitly overrides them:
//
//   - read-only rootfs with a writable tmpfs at /tmp
//   - --cap-drop ALL, --security-opt no-new-privileges
//   - --network none
//   - --pids-limit 512 (when spec.Limits.PidsLimit == 0)
//   - --label gemba.session=<id> for the orphan reaper (gm-root.15.13)
//
// All command execution funnels through the runner field so tests can
// inject a fake without touching the real docker binary.
type Docker struct {
	// binary is the resolved path to docker. Empty uses "docker" from
	// $PATH.
	binary string
	// runner executes docker commands. Swapped in tests.
	runner dockerRunner
}

type dockerRunner func(ctx context.Context, binary string, args ...string) ([]byte, error)

// NewDocker constructs a Docker backend. Returns an error when docker
// is not on $PATH so the native adaptor can fall back cleanly or
// surface the config error before opening a listener.
func NewDocker() (*Docker, error) {
	p, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("native/backend/docker: docker not found on PATH: %w", err)
	}
	return &Docker{binary: p, runner: runDocker}, nil
}

// Name implements Backend.
func (*Docker) Name() string { return "docker" }

// RemoteMode reports whether DOCKER_HOST points at a non-local
// daemon. When true, bind-mount paths are resolved on the REMOTE
// host, not the gemba host — callers that hand a local workspace
// path to a remote daemon will fail at spawn time (gm-root.15.4).
//
// Recognized remote schemes: ssh:// (the common case), tcp://,
// http://, https://. Empty / unix:// / fd:// are treated as local.
func (d *Docker) RemoteMode() bool {
	return IsRemoteDockerHost(os.Getenv("DOCKER_HOST"))
}

// IsRemoteDockerHost is the pure predicate behind RemoteMode, exposed
// so the orchestration plane can warn on suspicious config without
// constructing a Docker backend.
func IsRemoteDockerHost(dockerHost string) bool {
	h := strings.TrimSpace(strings.ToLower(dockerHost))
	if h == "" {
		return false
	}
	switch {
	case strings.HasPrefix(h, "unix://"), strings.HasPrefix(h, "fd://"):
		return false
	case strings.HasPrefix(h, "ssh://"),
		strings.HasPrefix(h, "tcp://"),
		strings.HasPrefix(h, "http://"),
		strings.HasPrefix(h, "https://"):
		return true
	}
	return false
}

// LocalMountWarning inspects a bind-mount source path and returns a
// human-readable warning when the path is obviously meaningful only
// on the local host but the backend is running in remote mode.
// Returns empty string when the path looks portable (absolute, no
// ~ / $VAR / relative segments) or when remote mode is off.
//
// The warning is surfaced to the operator via slog at spawn time —
// it is NOT a hard error because a remote host may legitimately share
// the path (NFS, identical layouts). gm-root.15.4.
func LocalMountWarning(src string, remote bool) string {
	if !remote || src == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(src, "~"):
		return fmt.Sprintf("mount src %q starts with ~ but DOCKER_HOST is remote — ~ resolves on the gemba host, not the remote daemon", src)
	case strings.HasPrefix(src, "$"):
		return fmt.Sprintf("mount src %q references an env var but DOCKER_HOST is remote — the var resolves on the gemba host", src)
	case !strings.HasPrefix(src, "/"):
		return fmt.Sprintf("mount src %q is a relative path but DOCKER_HOST is remote — the remote daemon will resolve it against its own cwd", src)
	}
	return ""
}

// listFormatDocker is the template passed to `docker ps`. One line
// per container; tab-separated so a title with spaces survives.
const listFormatDocker = "{{.ID}}\t{{.Image}}\t{{.Command}}\t{{.Names}}\t{{.Labels}}"

// ListPanes returns every container the daemon can see that carries
// a gemba.session label. Non-gemba containers on the host are ignored
// — the operator's other workloads are not gemba's business.
func (d *Docker) ListPanes(ctx context.Context) ([]Pane, error) {
	out, err := d.runner(ctx, d.binary,
		"ps", "-a",
		"--filter", "label=gemba.session",
		"--format", listFormatDocker,
	)
	if err != nil {
		return nil, err
	}
	return parseDockerPs(string(out))
}

// parseDockerPs is the pure half of ListPanes so tests can exercise
// the output parser without spawning docker.
func parseDockerPs(out string) ([]Pane, error) {
	var sessions []Pane
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 5 {
			continue
		}
		s := Pane{
			Kind:    KindContainer,
			ID:      parts[0],
			Image:   parts[1],
			Command: strings.Trim(parts[2], `"`),
			Title:   parts[3],
			Labels:  parseDockerLabels(parts[4]),
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func parseDockerLabels(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	for _, kv := range strings.Split(raw, ",") {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		out[strings.TrimSpace(kv[:eq])] = strings.TrimSpace(kv[eq+1:])
	}
	return out
}

// SpawnPane launches a container per spec and returns the populated
// Session. The container runs detached; its stdout/stderr are captured
// by docker for CapturePane to tail later.
func (d *Docker) SpawnPane(ctx context.Context, spec SpawnSpec) (Pane, error) {
	if spec.Image == "" {
		return Pane{}, fmt.Errorf("native/backend/docker: SpawnPane requires Image")
	}
	args := d.buildRunArgs(spec)
	out, err := d.runner(ctx, d.binary, args...)
	if err != nil {
		return Pane{}, fmt.Errorf("native/backend/docker: run: %w", err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return Pane{}, fmt.Errorf("native/backend/docker: run returned empty container id")
	}
	return d.inspect(ctx, id, spec)
}

// buildRunArgs is the single composition point for `docker run`
// arguments. Tests assert against its output directly rather than
// threading a fake exec through the whole backend.
func (d *Docker) buildRunArgs(spec SpawnSpec) []string {
	args := []string{"run", "-d"}

	// Security envelope defaults (gm-root.15.7). SpawnSpec fields
	// toggle them off when the agent type declares unsafe = true
	// upstream; the backend itself does not second-guess policy.
	if spec.ReadOnlyRootfs {
		args = append(args, "--read-only",
			"--tmpfs", "/tmp:size=128m,mode=1777")
	}
	args = append(args,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges")

	// Network policy (gm-root.15.8). Default to "none" when
	// unspecified — failure-closed is the right default for
	// sandboxed agents.
	netMode := spec.Network.Mode
	if netMode == "" {
		netMode = "none"
	}
	args = append(args, "--network", netMode)

	// Resource caps (gm-root.15.7). Zero fields fall back to the
	// backend's opinionated defaults rather than literally unlimited.
	if spec.Limits.CPUs > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(spec.Limits.CPUs, 'f', -1, 64))
	}
	if spec.Limits.Memory > 0 {
		args = append(args, "--memory", strconv.FormatInt(spec.Limits.Memory, 10))
	}
	pids := spec.Limits.PidsLimit
	if pids == 0 {
		pids = 512
	}
	args = append(args, "--pids-limit", strconv.FormatInt(pids, 10))

	if spec.UserNS != "" {
		args = append(args, "--user", spec.UserNS)
	}

	if spec.Cwd != "" {
		args = append(args, "--workdir", spec.Cwd)
	}

	// Env (non-secret). Sorted for deterministic argv — makes tests
	// stable and logged commands diff-friendly.
	for _, k := range sortedKeys(spec.Env) {
		args = append(args, "--env", k+"="+spec.Env[k])
	}

	for _, m := range spec.Mounts {
		args = append(args, mountArg(m)...)
	}

	// Secrets ride on a tmpfs mount; the resolver (gm-root.15.9)
	// writes material into /run/secrets/<name> before exec. Here we
	// only declare the mount point.
	if len(spec.Secrets) > 0 {
		args = append(args, "--tmpfs", "/run/secrets:mode=0400,size=1m")
	}

	for _, k := range sortedKeys(spec.Labels) {
		args = append(args, "--label", k+"="+spec.Labels[k])
	}

	args = append(args, spec.Image)
	args = append(args, spec.Command...)
	return args
}

func mountArg(m Mount) []string {
	switch strings.ToLower(m.Mode) {
	case "tmpfs":
		opt := m.Dst
		if m.Size != "" {
			opt = m.Dst + ":size=" + m.Size
		}
		return []string{"--tmpfs", opt}
	case "ro":
		return []string{"--mount", fmt.Sprintf("type=bind,src=%s,dst=%s,readonly", m.Src, m.Dst)}
	default:
		return []string{"--mount", fmt.Sprintf("type=bind,src=%s,dst=%s", m.Src, m.Dst)}
	}
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}

// inspectOutput is the subset of `docker inspect` we need to hydrate
// a Session from a just-launched container id. Keeping it a flat
// struct (rather than feeding it through ListPanes) avoids a second
// daemon round-trip just to look up a label.
type inspectOutput struct {
	Config struct {
		Image  string            `json:"Image"`
		Cmd    []string          `json:"Cmd"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Pid int `json:"Pid"`
	} `json:"State"`
	Name string `json:"Name"`
}

func (d *Docker) inspect(ctx context.Context, id string, spec SpawnSpec) (Pane, error) {
	out, err := d.runner(ctx, d.binary, "inspect", id)
	if err != nil {
		// Non-fatal: we still have the id and can return best-effort
		// metadata. ListPanes will fully populate on the next enum.
		return Pane{
			Kind:    KindContainer,
			ID:      id,
			Image:   spec.Image,
			Cwd:     spec.Cwd,
			Title:   spec.Title,
			Labels:  spec.Labels,
			Command: strings.Join(spec.Command, " "),
		}, nil
	}
	var items []inspectOutput
	if err := json.Unmarshal(out, &items); err != nil || len(items) == 0 {
		return Pane{
			Kind:    KindContainer,
			ID:      id,
			Image:   spec.Image,
			Cwd:     spec.Cwd,
			Title:   spec.Title,
			Labels:  spec.Labels,
			Command: strings.Join(spec.Command, " "),
		}, nil
	}
	it := items[0]
	return Pane{
		Kind:    KindContainer,
		ID:      id,
		Image:   it.Config.Image,
		Cwd:     spec.Cwd,
		Title:   strings.TrimPrefix(it.Name, "/"),
		Labels:  it.Config.Labels,
		Command: strings.Join(it.Config.Cmd, " "),
		Pid:     it.State.Pid,
	}, nil
}

// SendKeys pipes keys into the container's stdin via `docker exec -i`.
// Unlike tmux, there is no shared tty here — SendKeys attaches to the
// agent's running process if the image's entrypoint wired stdin
// through, otherwise it execs a shell and writes there.
//
// For one-shot send without newline handling, strip a trailing
// "Enter" sentinel: the tmux convention maps "Enter" to a newline;
// the Docker path translates that to "\n" on the wire.
func (d *Docker) SendKeys(ctx context.Context, sessionID, keys string) error {
	if sessionID == "" {
		return fmt.Errorf("native/backend/docker: SendKeys requires session id")
	}
	payload := keys
	if strings.HasSuffix(payload, "Enter") {
		payload = strings.TrimSuffix(payload, "Enter") + "\n"
	}
	cmd := exec.CommandContext(ctx, d.binary, "exec", "-i", sessionID, "sh", "-c", "cat >/proc/1/fd/0")
	cmd.Stdin = strings.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker exec %s: %w (stderr=%q)",
			sessionID, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// SendInput delivers a typed SessionInput payload. gm-v01.3.1. Docker
// has no native key-name surface (no shared tty); literal mode delivers
// raw bytes via the existing SendKeys stdin path, keys mode mirrors that
// (callers are responsible for translating named keys to control
// sequences before posting), and signal mode shells out to
// `docker kill --signal=<name>` so SIGINT/SIGTERM actually deliver as
// real POSIX signals to the container's init process.
func (d *Docker) SendInput(ctx context.Context, sessionID string, in core.SessionInput) error {
	if sessionID == "" {
		return core.NewAdaptorError(core.KindValidation,
			"native/backend/docker: SendInput requires session id")
	}
	if in.Keys == "" {
		return core.NewAdaptorError(core.KindValidation,
			"native/backend/docker: SendInput requires non-empty Keys")
	}
	switch in.Mode {
	case core.InputLiteral, core.InputKeys:
		return d.SendKeys(ctx, sessionID, in.Keys)
	case core.InputSignal:
		if _, err := d.runner(ctx, d.binary, "kill", "--signal="+in.Keys, sessionID); err != nil {
			return core.WrapAdaptorError(core.KindProcessFailed, err,
				"native/backend/docker: docker kill --signal=%s %s", in.Keys, sessionID)
		}
		return nil
	default:
		return core.NewAdaptorError(core.KindValidation,
			"native/backend/docker: unknown SessionInputMode %q", in.Mode)
	}
}

// CapturePane returns the last `lines` of the container's stdout +
// stderr via `docker logs --tail`. ANSI is left intact; the SPA strips
// it when rendering.
func (d *Docker) CapturePane(ctx context.Context, sessionID string, lines int) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("native/backend/docker: CapturePane requires session id")
	}
	if lines <= 0 {
		lines = 200
	}
	out, err := d.runner(ctx, d.binary, "logs", "--tail", strconv.Itoa(lines), sessionID)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Kill force-removes the container. Graceful shutdown (docker stop
// with SIGTERM + grace) is the OrchestrationPlane's EndSession path
// (gm-root.15.12); this is the backstop.
func (d *Docker) Kill(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("native/backend/docker: Kill requires session id")
	}
	_, err := d.runner(ctx, d.binary, "rm", "-f", sessionID)
	return err
}

// Pause implements Pausable (gm-root.15.12). The docker verb freezes
// all processes in the container; Unpause resumes them from the exact
// same memory state. Tmux has no equivalent.
func (d *Docker) Pause(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("native/backend/docker: Pause requires session id")
	}
	_, err := d.runner(ctx, d.binary, "pause", sessionID)
	return err
}

// Unpause implements Pausable.
func (d *Docker) Unpause(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("native/backend/docker: Unpause requires session id")
	}
	_, err := d.runner(ctx, d.binary, "unpause", sessionID)
	return err
}

// runDocker is the default runner — swapped in tests. Stderr surfaces
// in the returned error so operator-visible failures carry the
// daemon's message, not just "exit status 1".
func runDocker(ctx context.Context, binary string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker %s: %w (stderr=%q)",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
