# syntax=docker/dockerfile:1.7
#
# Standard Gemba server image.
#
# Unlike Dockerfile.quickstart, this image does not seed a demo Beads
# database and does not force Beads-only mode. It packages the Gemba
# server, sentinel CLIs, and bd CLI so operators can mount their own
# worktree or point at a Dolt URL with one container image.

ARG NODE_VERSION=22-bookworm
ARG GO_VERSION=1.25-bookworm
ARG PYTHON_VERSION=3.12-slim-bookworm
ARG BEADS_VERSION=1.0.3

FROM node:${NODE_VERSION} AS web-builder
WORKDIR /src
COPY . .
RUN corepack enable \
  && pnpm install --frozen-lockfile \
  && pnpm --filter gemba-web build \
  && touch web/dist/.keep

FROM golang:${GO_VERSION} AS go-builder
WORKDIR /src
COPY . .
COPY --from=web-builder /src/web/dist ./web/dist
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o /out/gemba ./cmd/gemba \
  && CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o /out/gemba-bridge ./cmd/gemba-bridge \
  && CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o /out/gemba-state ./cmd/gemba-state \
  && CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o /out/gemba-ask ./cmd/gemba-ask \
  && CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o /out/gemba-mcp ./cmd/gemba-mcp \
  && CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o /out/gemba-bd-hook ./cmd/gemba-bd-hook \
  && CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o /out/gemba-codex-driver ./cmd/gemba-codex-driver

FROM python:${PYTHON_VERSION} AS runtime
ARG TARGETARCH
ARG BEADS_VERSION

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates tini git bash openssh-client curl \
  && rm -rf /var/lib/apt/lists/*

RUN case "${TARGETARCH}" in \
      amd64|arm64) bd_arch="${TARGETARCH}" ;; \
      *) echo "unsupported TARGETARCH for bd: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
  && curl -fsSL \
      "https://github.com/gastownhall/beads/releases/download/v${BEADS_VERSION}/beads_${BEADS_VERSION}_linux_${bd_arch}.tar.gz" \
      -o /tmp/bd.tar.gz \
  && tar -xzf /tmp/bd.tar.gz -C /usr/local/bin bd \
  && chmod 0755 /usr/local/bin/bd \
  && rm /tmp/bd.tar.gz \
  && bd version

RUN useradd --create-home --home-dir /home/gemba --uid 10001 --shell /usr/sbin/nologin gemba \
  && mkdir -p /data /work \
  && chown -R gemba:gemba /data /work /home/gemba

COPY --from=go-builder /out/gemba /usr/local/bin/gemba
COPY --from=go-builder /out/gemba-bridge /usr/local/bin/gemba-bridge
COPY --from=go-builder /out/gemba-state /usr/local/bin/gemba-state
COPY --from=go-builder /out/gemba-ask /usr/local/bin/gemba-ask
COPY --from=go-builder /out/gemba-mcp /usr/local/bin/gemba-mcp
COPY --from=go-builder /out/gemba-bd-hook /usr/local/bin/gemba-bd-hook
COPY --from=go-builder /out/gemba-codex-driver /usr/local/bin/gemba-codex-driver
COPY deploy/docker/server-entrypoint.sh /usr/local/bin/gemba-server
RUN chmod 0755 /usr/local/bin/gemba /usr/local/bin/gemba-* /usr/local/bin/gemba-server

USER gemba
WORKDIR /work
ENV HOME=/home/gemba \
    GEMBA_HOME=/data/gemba-home \
    GEMBA_DATA_DIR=/data \
    GEMBA_LISTEN=0.0.0.0:7666 \
    GEMBA_AUTH=token \
    GEMBA_ORCHESTRATION=none
VOLUME ["/data", "/work"]
EXPOSE 7666

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/gemba-server"]
CMD ["serve"]
