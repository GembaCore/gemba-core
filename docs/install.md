# Installing Gemba

Two MVP install paths — pick the one that matches your environment:

| Path | Best for | Bead |
| --- | --- | --- |
| Build from source | Hackers, contributors, Linux/macOS dev boxes | gm-e14.7 |
| Container image | Servers, ops-friendly deploys, Kubernetes | gm-e14.8 |

For the package-manager path (`.deb` / `.rpm` from a goreleaser build),
see [docs/deployment/vps.md](deployment/vps.md).

## Build from source

Clone, `make deps && make install`, drop in a systemd unit, run. Always
works regardless of distro.

## Prerequisites

| Tool | Version | Why |
| --- | --- | --- |
| Go | >= 1.25 (see `go.mod`) | builds the `gemba` binary |
| Node.js | >= 20 | runs the SPA build |
| pnpm | >= 9 | frontend package manager (`corepack enable` is enough) |
| git, make | any recent | drive the build |

Runtime deps (`tmux`, `git`, `bd`, `dolt`, agent CLIs) are listed in
[docs/deployment/vps.md](deployment/vps.md) under "Runtime dependencies"
— they are not required to build, only to run sessions.

## Build + install

```bash
git clone https://github.com/GembaCore/gemba-core.git
cd gemba

make deps                       # go mod download + pnpm install
make install                    # builds + installs /usr/local/bin/gemba
# or for a userland install:
# make install PREFIX=$HOME/.local
```

`make install` builds the SPA, embeds it into the binary, and copies it
to `$(PREFIX)/bin/gemba` (default `/usr/local/bin/gemba`). The target is
idempotent — re-running upgrades the binary in place.

## Configure

Create the system user and state dirs:

```bash
sudo useradd --system --home-dir /var/lib/gemba --shell /usr/sbin/nologin gemba
sudo install -d -o gemba -g gemba /var/lib/gemba
sudo install -d -o root  -g gemba -m 0750 /etc/gemba
```

Drop in the env file (every knob is commented out by default — fill in
what you need):

```bash
sudo install -m 0640 -o root -g gemba \
    packaging/systemd/gemba.env.example /etc/gemba/gemba.env
sudo $EDITOR /etc/gemba/gemba.env
```

Rotate the auth token (writes a hash under gemba's config dir; prints
the bearer once on stdout):

```bash
sudo -u gemba /usr/local/bin/gemba auth token rotate
```

The token-rotate command is implemented in
[`internal/cli/auth.go`](../internal/cli/auth.go). Re-run it any time
you need to invalidate the previous token.

## Enable the systemd unit

```bash
sudo cp packaging/systemd/gemba.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now gemba
sudo journalctl -u gemba -f       # watch the startup banner
```

The shipped unit (see `packaging/systemd/gemba.service`) defaults to
`ExecStart=/usr/local/bin/gemba serve --listen 127.0.0.1 --port 7666`.
To expose on the public network, edit that line **and** turn auth on at
the same time:

```ini
ExecStart=/usr/local/bin/gemba serve \
    --listen 0.0.0.0 --port 7666 \
    --auth token \
    --tls-cert /etc/gemba/fullchain.pem --tls-key /etc/gemba/privkey.pem
```

The unit ships with hardening defaults (`ProtectSystem=strict`,
`PrivateTmp=yes`, `NoNewPrivileges=yes`, `ProtectHome=read-only`,
`RestrictNamespaces=yes`, ...). `ReadWritePaths=/var/lib/gemba
/etc/gemba` is the only writable surface.

## Verify

```bash
curl -fsS http://127.0.0.1:7666/api/v1/capabilities | head
```

You should see a JSON capabilities document. If you enabled `--auth
token`, pass `-H "Authorization: Bearer <token>"`.

## Uninstall

```bash
sudo systemctl disable --now gemba
sudo make uninstall              # removes /usr/local/bin/gemba and the unit
# /etc/gemba and /var/lib/gemba are intentionally NOT removed; clean
# them up by hand if you want to discard state.
```

## Troubleshooting

- **`pnpm: command not found`** — run `corepack enable && corepack
  prepare pnpm@latest --activate`.
- **`go: cannot find main module`** — make sure you cloned the repo and
  `cd`'d into it before `make deps`.
- **Unit fails to start with `code=exited, status=203/EXEC`** — the
  binary isn't at `/usr/local/bin/gemba`; check `which gemba` or set
  `PREFIX=` accordingly when installing, then edit the unit's
  `ExecStart=` to match.
- **Capabilities endpoint 401** — auth is on; rotate the token and
  re-curl with `Authorization: Bearer <token>`.

## Container install

Gemba publishes a multi-arch (linux/amd64 + linux/arm64) container image
to GitHub Container Registry on every tagged release. The image is built
with [ko](https://ko.build) — no Dockerfile, distroless base
(`gcr.io/distroless/static-debian12:nonroot`), ~20MB compressed. The
SPA is embedded in the binary, so the image is the only artifact you
need to run.

### Pull

```bash
docker pull ghcr.io/mikebengtson/gemba-server:latest
```

Or pin a specific version:

```bash
docker pull ghcr.io/mikebengtson/gemba-server:v0.1.0
```

### Run

The image's ENTRYPOINT is the `gemba` binary. The recommended
invocation binds the server to all interfaces inside the container,
enables token authentication (gemba refuses to bind a non-loopback
address without auth), and persists the auto-provisioned auth-token
hash file on a host volume so restarts don't rotate the token:

```bash
docker run --rm \
  -p 7666:7666 \
  -v "$HOME/.gemba:/var/lib/gemba" \
  -e GEMBA_HOME=/var/lib/gemba \
  ghcr.io/mikebengtson/gemba-server:latest \
  serve --listen 0.0.0.0:7666 --auth token
```

On first start the server writes a freshly generated bearer token to
`$HOME/.gemba/auth-token` on the host (visible inside the container at
`/var/lib/gemba/auth-token`). Read it with `cat $HOME/.gemba/auth-token`
and pass it as a `Authorization: Bearer <token>` header on requests.

### Configure

Operators typically set:

| Env / mount | Purpose |
| --- | --- |
| `-v $HOME/.gemba:/var/lib/gemba` | Persist auth-token hash, sessions, agent profile across restarts |
| `-e GEMBA_HOME=/var/lib/gemba` | Tell gemba where to read/write its state |
| `-v <workspace>:/work` | Mount a workspace (Gas Town town root, Gas City city root, etc.) for gemba to drive |
| `--listen 0.0.0.0:7666` | Bind all interfaces (default 127.0.0.1; non-loopback requires `--auth`) |
| `--auth token` | Token-based bearer auth (default mode in container deploys) |
| `--city /work` / `--town /work` | Point gemba at the mounted workspace |

For TLS termination, run gemba behind a reverse proxy (Caddy, nginx,
Traefik, etc.). Gemba listens HTTP only inside the container; the proxy
handles certificates.

### Verify

With the container running, check the capabilities endpoint:

```bash
curl -sf http://localhost:7666/api/v1/capabilities | jq .
```

A successful 200 with the capabilities JSON confirms the container is
healthy. The browser UI is served at `http://localhost:7666/`.

### Image signing

Each published image is signed with cosign keyless (sigstore) via GitHub
OIDC. Verify before deploying:

```bash
cosign verify ghcr.io/mikebengtson/gemba-server:v0.1.0 \
  --certificate-identity-regexp=https://github.com/GembaCore/gemba-core/.* \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

### Building images locally

For testing image config changes without publishing:

```bash
make image-build-only   # smoke test (no push, no docker daemon)
make image-load         # build + load into local docker daemon
make image              # multi-arch local build (no push)
make image-push KO_DOCKER_REPO=ghcr.io/<you>/gemba-server   # build + publish
```

The ko config lives in `.ko.yaml` at the repo root. The release pipeline
is `.github/workflows/release-image.yml`.

### Limitations

- The image is the **plain server** — it does not bundle a docker CLI,
  so the docker-backend agent dispatcher (gm-root.15) cannot run from
  inside this image. Operators wanting docker-in-docker dispatch will
  need a follower image with a docker-cli base; tracked separately.
- The `tmux` orchestration backend will not function inside the image
  (no tmux binary in distroless). Use `--orchestration=native` with the
  default terminal backend, or `--orchestration=none`.
