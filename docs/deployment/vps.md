# Deploying Gemba on a VPS

Gemba is a single Go binary with the SPA embedded, so deployment is
"install a binary, install its deps, run it." This doc is the runbook.

## Runtime dependencies

| Dep | Required when | Install path |
| --- | --- | --- |
| `tmux` | `--orchestration native --terminal tmux` | distro package (declared in `.deb`/`.rpm`) |
| `git` | native orchestration (worktree provisioning) | distro package (declared in `.deb`/`.rpm`) |
| `bd` (beads) | `--beads-dir` WorkPlane mode | `scripts/install-deps.sh` (GitHub releases) |
| `dolt` | running a local Dolt server, or `bd dolt` ops | `scripts/install-deps.sh` (dolthub installer) |
| Dolt server reachable via mysql:// | `--dolt-url` WorkPlane mode | network only — no binary on the gemba host |
| agent CLIs (claude, gemini, …) | whatever your `.gemba/agents.toml` declares | out of scope for gemba; operator installs per agent type |

The `.deb` / `.rpm` built by goreleaser (see `.goreleaser.yml` →
`nfpms:`) declares `tmux`, `git`, and `ca-certificates` as `Depends:`,
so `apt install ./gemba_*.deb` pulls them automatically. `bd` and
`dolt` are not in standard distro repos, so they ride on
`scripts/install-deps.sh` which ships inside the package at
`/usr/share/gemba/install-deps.sh`.

## Install from package (recommended)

```bash
# 1. Install gemba + distro deps in one shot.
sudo apt install ./gemba_<version>_linux_amd64.deb

# 2. Install the non-distro deps (bd, dolt).
sudo /usr/share/gemba/install-deps.sh            # default: installs all
# sudo /usr/share/gemba/install-deps.sh --no-dolt  # skip dolt
# sudo /usr/share/gemba/install-deps.sh --check    # report what's missing
```

## Install from tarball

```bash
tar xzf gemba_<version>_linux_amd64.tar.gz
sudo install -m 0755 gemba /usr/bin/gemba
sudo bash install-deps.sh
sudo install -m 0644 gemba.service /lib/systemd/system/gemba.service
sudo useradd --system --home-dir /var/lib/gemba --shell /usr/sbin/nologin gemba
sudo mkdir -p /var/lib/gemba /etc/gemba && sudo chown gemba:gemba /var/lib/gemba
```

## Configure

Create `/etc/gemba/gemba.env`:

```ini
# Override the unit's ExecStart via a drop-in or wrap in a shell — the
# default unit runs `gemba serve` with no flags.
```

Or edit the unit's `ExecStart=` line directly:

```ini
ExecStart=/usr/bin/gemba serve \
  --listen 0.0.0.0 --port 7666 \
  --auth token \
  --tls-cert /etc/gemba/fullchain.pem --tls-key /etc/gemba/privkey.pem \
  --beads-dir /var/lib/gemba/rig \
  --orchestration native --terminal tmux
```

Then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now gemba
sudo journalctl -u gemba -f    # watch the startup banner; grab the token on first boot
```

## Reverse proxy (TLS + SSE)

If you front gemba with nginx, SSE needs buffering off:

```nginx
location / {
    proxy_pass http://127.0.0.1:7666;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
location ~ ^/(api/adaptors/stream|events)(/|$) {
    proxy_pass http://127.0.0.1:7666;
    proxy_http_version 1.1;
    proxy_buffering off;
    proxy_read_timeout 1h;
    proxy_set_header Host $host;
}
```

Caddy handles SSE correctly with `reverse_proxy 127.0.0.1:7666`
out of the box.

## Remote docker dispatch (laptop → VPS)

Gemba's container backend honors the standard `DOCKER_HOST=ssh://user@host`
pattern, so you can run gemba on your laptop and have it spawn
containerized agent sessions on a VPS **without installing gemba on
the VPS**. You only need ssh reach + a running docker daemon on the
remote.

```bash
# On the VPS: just install docker. No gemba needed.
sudo apt-get install -y docker.io
sudo usermod -aG docker "$USER"   # log out/in so group takes effect

# On the laptop:
export DOCKER_HOST="ssh://op@vps.example.com"
docker info                       # sanity check — should print the VPS daemon
gemba serve --orchestration native --terminal container \
  --beads-dir ~/work/rig
```

### Bind-mount paths must resolve on the remote host

Any mount whose `src` references `~`, `$HOME`, or a relative path will
be resolved on the **remote** daemon, not your laptop. Gemba's
`backend.LocalMountWarning` helper flags these at spawn time. Safe
patterns for remote dispatch:

- Absolute paths that exist on the remote (`/srv/work`)
- A workspace provisioned by gemba on the remote (mount `/var/lib/gemba/worktrees/{{session_id}}`)
- NFS / sshfs layouts where the laptop and remote see identical paths

If the path only exists on your laptop, you want the full `gemba-on-VPS`
deployment (earlier in this doc), not remote dispatch.

### Why this works without gemba-on-remote

`docker` over ssh:// is a native Docker CLI feature — the docker
binary on the gemba host opens an ssh connection to the remote,
forwards the daemon socket, and every `docker run` / `exec` / `logs`
command executes on the remote. Gemba is unaware of the transport;
it shells to `docker` as always. This is why `DOCKER_HOST=ssh://…`
is a zero-code-change path (gm-root.15.4).

## Verifying deps

```bash
bash /usr/share/gemba/install-deps.sh --check
```

Prints one line per dep with `✓` or `✗`. Run this in CI against a
fresh image to catch regressions in the install story.
