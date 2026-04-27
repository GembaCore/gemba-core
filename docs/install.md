# Installing gemba-server

This page documents the install paths supported for v1.0. Pick the one
that matches your environment:

| Path | Best for | Status |
| --- | --- | --- |
| Build from source | Hackers, contributors, Linux/macOS dev boxes | gm-e14.7 |
| Container image | Servers, ops-friendly deploys, Kubernetes | gm-e14.8 (this section) |

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
  --certificate-identity-regexp=https://github.com/MikeBengtson/gemba/.* \
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
