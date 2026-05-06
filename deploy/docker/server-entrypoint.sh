#!/usr/bin/env bash
set -euo pipefail

DATA_DIR="${GEMBA_DATA_DIR:-/data}"
LISTEN="${GEMBA_LISTEN:-0.0.0.0:7666}"
AUTH="${GEMBA_AUTH:-token}"
ORCHESTRATION="${GEMBA_ORCHESTRATION:-none}"

export HOME="${HOME:-/home/gemba}"
export GEMBA_HOME="${GEMBA_HOME:-${DATA_DIR}/gemba-home}"

truthy() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

log() {
  printf 'gemba-server: %s\n' "$*" >&2
}

append_flag() {
  local name="$1"
  local value="$2"
  if [ -n "${value}" ]; then
    serve_args+=("${name}" "${value}")
  fi
}

if [ "${1:-serve}" != "serve" ]; then
  exec gemba "$@"
fi
shift || true

mkdir -p "${DATA_DIR}" "${GEMBA_HOME}" "${HOME}"

serve_args=(serve --listen "${LISTEN}" --auth "${AUTH}")

if truthy "${GEMBA_NOOP:-}"; then
  serve_args+=(--noop)
else
  project_dir="${GEMBA_PROJECT_DIR:-${GEMBA_BEADS_DIR:-}}"
  append_flag --project-dir "${project_dir}"
  append_flag --beads-url "${GEMBA_BEADS_URL:-}"
fi

if truthy "${GEMBA_BEADS_ONLY:-}" || [ "${GEMBA_MODE:-}" = "beads_only" ]; then
  serve_args+=(--beads-only)
fi
if truthy "${GEMBA_BEADS_READ_ONLY:-}"; then
  serve_args+=(--beads-read-only)
fi
if [ -n "${GEMBA_BEADS_HISTORY:-}" ]; then
  mkdir -p "$(dirname "${GEMBA_BEADS_HISTORY}")"
  serve_args+=(--beads-history "${GEMBA_BEADS_HISTORY}")
fi
if truthy "${GEMBA_RESTART:-}"; then
  serve_args+=(--restart)
fi

append_flag --city "${GEMBA_CITY:-}"
append_flag --town "${GEMBA_TOWN:-}"
append_flag --config "${GEMBA_CONFIG:-}"
append_flag --orchestration "${ORCHESTRATION}"
append_flag --terminal "${GEMBA_TERMINAL:-}"
append_flag --agents-registry "${GEMBA_AGENTS_REGISTRY:-}"
append_flag --worktrees-dir "${GEMBA_WORKTREES_DIR:-}"
append_flag --pool-config "${GEMBA_POOL_CONFIG:-}"
append_flag --prom-url "${GEMBA_PROM_URL:-}"

if truthy "${GEMBA_DANGEROUSLY_SKIP_PERMISSIONS:-}"; then
  serve_args+=(--dangerously-skip-permissions)
fi
if truthy "${GEMBA_QUIET:-}"; then
  serve_args+=(--quiet)
fi

log "starting Gemba on ${LISTEN}"
log "data: ${DATA_DIR}"
if [ -n "${GEMBA_PROJECT_DIR:-${GEMBA_BEADS_DIR:-}}" ]; then
  log "project dir: ${GEMBA_PROJECT_DIR:-${GEMBA_BEADS_DIR:-}}"
fi
if [ -n "${GEMBA_BEADS_URL:-}" ]; then
  log "beads url: ${GEMBA_BEADS_URL}"
fi

exec gemba "${serve_args[@]}" "$@"
