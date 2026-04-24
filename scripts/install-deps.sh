#!/usr/bin/env bash
# Installs gemba's runtime dependencies on a fresh Debian/Ubuntu VPS
# (the common VPS target). macOS is covered for local-dev parity.
#
# What this handles:
#   tmux, git, ca-certificates — standard distro packages. Also declared
#     as Depends: in the .deb/.rpm so `apt install gemba` pulls them.
#     This script is for operators who install from tarball instead.
#   bd (beads)                 — NOT in distro repos; pulled from the
#                                beads GitHub releases.
#   dolt                       — custom apt repo (Linux) / brew cask
#                                (macOS). Optional; only needed when
#                                gemba is run with --dolt-url, or when
#                                a local Dolt server backs bd.
#
# What this does NOT handle:
#   - agent CLIs (claude, gemini, codex, ...) — those are declared in
#     the operator's .gemba/agents.toml and are not gemba's concern.
#   - TLS certificates — use certbot / caddy / your CDN.
#
# Usage:
#   sudo bash install-deps.sh          # install everything (default)
#   sudo bash install-deps.sh --no-dolt    # skip dolt
#   sudo bash install-deps.sh --no-bd      # skip bd (e.g. --dolt-url only)
#   bash install-deps.sh --check       # report what's missing, install nothing

set -euo pipefail

INSTALL_BD=1
INSTALL_DOLT=1
CHECK_ONLY=0

BEADS_VERSION="${BEADS_VERSION:-latest}"
DOLT_VERSION="${DOLT_VERSION:-latest}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-bd)    INSTALL_BD=0 ;;
    --no-dolt)  INSTALL_DOLT=0 ;;
    --check)    CHECK_ONLY=1 ;;
    -h|--help)
      sed -n '2,30p' "$0"
      exit 0
      ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

say()  { printf '▶ %s\n' "$*"; }
miss() { printf '✗ %s missing\n' "$*"; }
have() { printf '✓ %s present\n' "$*"; }

check() {
  local bin="$1"
  if command -v "$bin" >/dev/null 2>&1; then
    have "$bin ($(command -v "$bin"))"
    return 0
  fi
  miss "$bin"
  return 1
}

detect_os() {
  case "$(uname -s)" in
    Linux)
      if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        echo "linux:${ID}"
      else
        echo "linux:unknown"
      fi
      ;;
    Darwin) echo "darwin" ;;
    *) echo "unsupported" ;;
  esac
}

OS="$(detect_os)"

if [[ "$CHECK_ONLY" == "1" ]]; then
  say "dependency check (no install)"
  check tmux   || true
  check git    || true
  check bd     || true
  check dolt   || true
  exit 0
fi

# ---------------------------------------------------------------------------
# Distro-native deps: tmux, git, ca-certificates
# ---------------------------------------------------------------------------
install_distro_deps() {
  case "$OS" in
    linux:ubuntu|linux:debian)
      say "apt: installing tmux git ca-certificates curl"
      apt-get update -y
      apt-get install -y tmux git ca-certificates curl
      ;;
    linux:fedora|linux:rhel|linux:centos|linux:rocky|linux:almalinux)
      say "dnf: installing tmux git ca-certificates curl"
      dnf install -y tmux git ca-certificates curl
      ;;
    linux:alpine)
      say "apk: installing tmux git ca-certificates curl"
      apk add --no-cache tmux git ca-certificates curl
      ;;
    darwin)
      if ! command -v brew >/dev/null 2>&1; then
        echo "Homebrew not installed; see https://brew.sh" >&2
        exit 1
      fi
      say "brew: installing tmux git"
      brew install tmux git
      ;;
    *)
      echo "unsupported OS: $OS" >&2
      exit 1
      ;;
  esac
}

# ---------------------------------------------------------------------------
# bd (beads) — github releases
# ---------------------------------------------------------------------------
install_bd() {
  if command -v bd >/dev/null 2>&1; then
    have "bd already installed"
    return 0
  fi
  say "installing bd (beads) from GitHub releases"
  local arch os_tag
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) echo "unsupported arch $(uname -m) for bd" >&2; return 1 ;;
  esac
  case "$OS" in
    linux:*) os_tag="linux" ;;
    darwin)  os_tag="darwin" ;;
    *) echo "unsupported OS for bd: $OS" >&2; return 1 ;;
  esac

  local url
  if [[ "$BEADS_VERSION" == "latest" ]]; then
    url="$(curl -fsSL https://api.github.com/repos/steveyegge/beads/releases/latest \
      | grep -Eo "https://[^\"]+beads_[^\"]*${os_tag}_${arch}\\.tar\\.gz" \
      | head -n1)"
  else
    url="https://github.com/steveyegge/beads/releases/download/${BEADS_VERSION}/beads_${BEADS_VERSION#v}_${os_tag}_${arch}.tar.gz"
  fi
  [[ -n "$url" ]] || { echo "could not resolve bd release url" >&2; return 1; }
  local tmp
  tmp="$(mktemp -d)"
  curl -fsSL "$url" -o "$tmp/bd.tgz"
  tar -xzf "$tmp/bd.tgz" -C "$tmp"
  install -m 0755 "$tmp/bd" /usr/local/bin/bd
  rm -rf "$tmp"
  have "bd installed: $(bd --version 2>/dev/null || echo '(version unknown)')"
}

# ---------------------------------------------------------------------------
# dolt — custom apt repo (Linux) / brew (macOS)
# ---------------------------------------------------------------------------
install_dolt() {
  if command -v dolt >/dev/null 2>&1; then
    have "dolt already installed"
    return 0
  fi
  say "installing dolt"
  case "$OS" in
    linux:ubuntu|linux:debian|linux:fedora|linux:rhel|linux:centos|linux:rocky|linux:almalinux|linux:alpine)
      # Official installer; handles distro detection and sets up /usr/local/bin/dolt.
      curl -L https://github.com/dolthub/dolt/releases/latest/download/install.sh | bash
      ;;
    darwin)
      brew install dolt
      ;;
    *)
      echo "unsupported OS for dolt: $OS" >&2
      return 1
      ;;
  esac
  have "dolt installed: $(dolt version 2>/dev/null | head -n1 || echo '(version unknown)')"
}

# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------
if [[ "$(id -u)" != "0" && "$OS" != darwin ]]; then
  echo "this script needs root on Linux; re-run with sudo" >&2
  exit 1
fi

install_distro_deps
[[ "$INSTALL_BD"   == "1" ]] && install_bd
[[ "$INSTALL_DOLT" == "1" ]] && install_dolt

say "done. verify with: bash $0 --check"
