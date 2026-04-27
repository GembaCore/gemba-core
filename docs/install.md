# Installing Gemba (build from source)

This is the **build-from-source** install path: clone, `make deps && make
install`, drop in a systemd unit, and run. It's the MVP install story
(gm-e14.7) and the one that always works regardless of distro.

For the package-manager path (`.deb` / `.rpm` from a goreleaser build),
see [docs/deployment/vps.md](deployment/vps.md). The container-image path
is tracked separately under gm-e14.8.

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
git clone https://github.com/MikeBengtson/gemba.git
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
