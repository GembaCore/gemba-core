#!/usr/bin/env bash
set -euo pipefail

DATA_DIR="${GEMBA_DATA_DIR:-/data}"
DEMO_DIR="${GEMBA_DEMO_DIR:-${DATA_DIR}/example-project}"
DEMO_PREFIX="${GEMBA_DEMO_PREFIX:-mp}"
SEED_FILE="${GEMBA_DEMO_SEED:-/usr/share/gemba/examples/my-project/seed.json}"
HISTORY_FILE="${GEMBA_BEADS_HISTORY:-${DATA_DIR}/session-manifest.jsonl}"
LISTEN="${GEMBA_LISTEN:-0.0.0.0:7666}"
AUTH="${GEMBA_AUTH:-token}"

export HOME="${HOME:-/home/gemba}"
export GEMBA_HOME="${GEMBA_HOME:-${DATA_DIR}/gemba-home}"

truthy() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

log() {
  printf 'gemba-quickstart: %s\n' "$*" >&2
}

bd_list_is_empty() {
  local compact
  compact="$(cd "${DEMO_DIR}" && bd list --json --limit 1 | tr -d '[:space:]')"
  [ "${compact}" = "[]" ]
}

seed_demo() {
  if [ ! -f "${SEED_FILE}" ]; then
    log "seed file missing: ${SEED_FILE}"
    return 1
  fi

  mkdir -p "${DEMO_DIR}" "$(dirname "${HISTORY_FILE}")" "${GEMBA_HOME}" "${HOME}"

  if [ ! -d "${DEMO_DIR}/.beads" ]; then
    log "initializing demo Beads database at ${DEMO_DIR}"
    (
      cd "${DEMO_DIR}"
      BD_NON_INTERACTIVE=1 bd init \
        -p "${DEMO_PREFIX}" \
        --non-interactive \
        --skip-agents \
        --skip-hooks
    )
  fi

  if [ -f "${DEMO_DIR}/.gemba/quickstart-seeded" ]; then
    return 0
  fi

  if ! bd_list_is_empty; then
    log "existing Beads found at ${DEMO_DIR}; leaving them untouched"
    mkdir -p "${DEMO_DIR}/.gemba"
    touch "${DEMO_DIR}/.gemba/quickstart-seeded"
    return 0
  fi

  log "seeding example project"
  local graph_out
  graph_out="$(cd "${DEMO_DIR}" && bd create --graph "${SEED_FILE}" 2>&1)"

  key_to_id() {
    printf '%s\n' "${graph_out}" | awk -v k="$1" '$1==k{print $3; exit}'
  }

  local closed_keys=(f1 a1 d1 d2)
  local in_progress_keys=(a2 a3 d3 f2 u1)
  # Keep a few future/unclear items in Backlog so the quickstart
  # demonstrates /refine as a real grooming surface instead of an
  # empty "all work is already accepted" table.
  local backlog_keys=(u3 u4 p5 b2)
  local key id

  for key in "${closed_keys[@]}"; do
    id="$(key_to_id "${key}")"
    if [ -n "${id}" ]; then
      (cd "${DEMO_DIR}" && bd close "${id}" -m "shipped" >/dev/null 2>&1) || true
    fi
  done

  for key in "${in_progress_keys[@]}"; do
    id="$(key_to_id "${key}")"
    if [ -n "${id}" ]; then
      (cd "${DEMO_DIR}" && bd update "${id}" --status in_progress >/dev/null 2>&1) || true
    fi
  done

  for key in "${backlog_keys[@]}"; do
    id="$(key_to_id "${key}")"
    if [ -n "${id}" ]; then
      (cd "${DEMO_DIR}" && bd update "${id}" --status deferred >/dev/null 2>&1) || true
    fi
  done

  mkdir -p "${DEMO_DIR}/.gemba"
  touch "${DEMO_DIR}/.gemba/quickstart-seeded"
}

if [ "${1:-serve}" = "seed-demo" ]; then
  seed_demo
  exit 0
fi

if [ "${1:-serve}" != "serve" ]; then
  exec gemba "$@"
fi
shift || true

mkdir -p "${DATA_DIR}" "${GEMBA_HOME}" "${HOME}" "$(dirname "${HISTORY_FILE}")"

mode_args=(--beads-only)
if [ -n "${GEMBA_BEADS_URL:-}" ]; then
  mode_args+=(--beads-url "${GEMBA_BEADS_URL}")
else
  if [ -z "${GEMBA_BEADS_DIR:-}" ]; then
    seed_demo
    GEMBA_BEADS_DIR="${DEMO_DIR}"
  fi
  mode_args+=(--beads-dir "${GEMBA_BEADS_DIR}")
fi

if truthy "${GEMBA_BEADS_READ_ONLY:-}"; then
  mode_args+=(--beads-read-only)
fi

mode_args+=(--beads-history "${HISTORY_FILE}")

log "starting Gemba on ${LISTEN}"
log "data: ${DATA_DIR}"
if [ -n "${GEMBA_BEADS_URL:-}" ]; then
  log "beads source: ${GEMBA_BEADS_URL}"
else
  log "beads source: ${GEMBA_BEADS_DIR}"
fi

exec gemba serve \
  --listen "${LISTEN}" \
  --auth "${AUTH}" \
  "${mode_args[@]}" \
  "$@"
