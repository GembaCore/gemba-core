#!/usr/bin/env bash
# m1-smoke.sh — curl-level pre-flight for the M1 milestone endpoints.
#
# Boots gemba against an ephemeral seeded fixture, curls each endpoint the
# M1.8 human smoke depends on, prints pass/fail per endpoint, and exits
# nonzero on any failure. Intended as the "does the stack even come up"
# check before a user runs the manual M1.8 acceptance pass.
#
# Does not touch any real rig: uses `mktemp -d` + embedded Dolt + an
# ephemeral port, cleans up on exit.
#
# Usage:
#   ./scripts/m1-smoke.sh                # build if needed, run smoke
#   GEMBA_BIN=/path/to/gemba ./scripts/m1-smoke.sh
#   GEMBA_SMOKE_PORT=7777 ./scripts/m1-smoke.sh

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
FIXTURE_SRC="${REPO_ROOT}/testing/fixtures/m1-smoke"

GEMBA_BIN="${GEMBA_BIN:-${REPO_ROOT}/bin/gemba}"
PORT="${GEMBA_SMOKE_PORT:-7690}"
HOST="127.0.0.1"
BASE_URL="http://${HOST}:${PORT}"

# ---- helpers --------------------------------------------------------------

c_red=$'\033[31m'; c_green=$'\033[32m'; c_yellow=$'\033[33m'; c_reset=$'\033[0m'
# No colours if stdout is not a TTY (CI-parseable).
if [[ ! -t 1 ]]; then c_red=""; c_green=""; c_yellow=""; c_reset=""; fi

log()  { printf '%s\n' "$*"; }
info() { printf '%s[m1-smoke]%s %s\n' "${c_yellow}" "${c_reset}" "$*"; }
pass() { printf '%sPASS%s %s\n' "${c_green}" "${c_reset}" "$*"; }
fail() { printf '%sFAIL%s %s\n' "${c_red}" "${c_reset}" "$*"; }

FAILURES=0

require() {
  command -v "$1" >/dev/null 2>&1 || {
    fail "missing required tool: $1"
    exit 2
  }
}

# ---- preflight ------------------------------------------------------------

require curl
require bd
require mktemp

if [[ ! -x "${GEMBA_BIN}" ]]; then
  info "building gemba (no binary at ${GEMBA_BIN})…"
  ( cd "${REPO_ROOT}" && GOFLAGS="-buildvcs=false" go build \
      -o "${GEMBA_BIN}" ./cmd/gemba ) || {
    fail "gemba build failed"
    exit 2
  }
fi

if [[ ! -f "${FIXTURE_SRC}/seeds.jsonl" ]]; then
  fail "fixture seeds missing: ${FIXTURE_SRC}/seeds.jsonl"
  exit 2
fi

TMP_FIXTURE="$(mktemp -d -t gemba-m1-smoke-XXXXXX)"
SERVER_PID=""

cleanup() {
  # MUST be the very first statement — any other command would clobber $?.
  # `local` (with or without assignment) runs the `local` builtin which
  # resets $? on some bashes, so keep this as a plain global assignment.
  SMOKE_RC=$?
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    # Give it up to 2s to shut down cleanly, then SIGKILL.
    for _ in 1 2 3 4; do
      kill -0 "${SERVER_PID}" 2>/dev/null || break
      sleep 0.5
    done
    kill -9 "${SERVER_PID}" 2>/dev/null || true
  fi
  if [[ -n "${TMP_FIXTURE}" && -d "${TMP_FIXTURE}" ]]; then
    rm -rf "${TMP_FIXTURE}"
  fi
  exit "${SMOKE_RC}"
}
trap cleanup EXIT INT TERM

# ---- seed fixture ---------------------------------------------------------

info "seeding fixture at ${TMP_FIXTURE}"
cp "${FIXTURE_SRC}/seeds.jsonl" "${TMP_FIXTURE}/seeds.jsonl"

# Run bd in the fixture dir. Embedded Dolt = no shared-server pollution.
# --skip-hooks/--skip-agents keep the ephemeral dir clean (no git hooks, no
# AGENTS.md generation).
(
  cd "${TMP_FIXTURE}" &&
  bd init --prefix smk --non-interactive \
    --skip-hooks --skip-agents --quiet
) || {
  fail "bd init failed"
  exit 2
}

( cd "${TMP_FIXTURE}" && bd import seeds.jsonl >/dev/null ) || {
  fail "bd import failed"
  exit 2
}

# ---- launch server --------------------------------------------------------

info "launching gemba on ${BASE_URL}"
# stderr captured so logs don't scramble test output; surfaced on failure.
LOG_FILE="${TMP_FIXTURE}/gemba.log"
"${GEMBA_BIN}" serve \
  --beads-dir "${TMP_FIXTURE}" \
  --auth none \
  --listen "${HOST}" \
  --port "${PORT}" \
  >"${LOG_FILE}" 2>&1 &
SERVER_PID=$!

# Wait for /api/health to answer 200 (up to 10s).
READY=0
for _ in $(seq 1 20); do
  if curl -s -f -o /dev/null "${BASE_URL}/api/health" 2>/dev/null; then
    READY=1
    break
  fi
  if ! kill -0 "${SERVER_PID}" 2>/dev/null; then
    break
  fi
  sleep 0.5
done

if [[ "${READY}" -ne 1 ]]; then
  fail "server did not become ready within 10s"
  log "---- gemba log ----"
  cat "${LOG_FILE}" || true
  exit 1
fi

info "server ready (pid=${SERVER_PID})"

# ---- probes ---------------------------------------------------------------

# check_endpoint <name> <path> <jq-style-grep-pattern>
#   Fetches BASE_URL/path, asserts HTTP 200 and body contains the pattern.
#   No jq dependency — grep -E is enough for the smoke level.
check_endpoint() {
  local name="$1" path="$2" pattern="$3"
  local body status
  body="$(curl -sS -w '\n%{http_code}' "${BASE_URL}${path}")" || {
    fail "${name} ${path} — curl error"
    FAILURES=$((FAILURES + 1))
    return
  }
  status="$(printf '%s' "${body}" | tail -n1)"
  body="$(printf '%s' "${body}" | sed '$d')"

  if [[ "${status}" != "200" ]]; then
    fail "${name} ${path} — http ${status}"
    printf '      body: %s\n' "${body}" | head -c 400
    printf '\n'
    FAILURES=$((FAILURES + 1))
    return
  fi

  if ! printf '%s' "${body}" | grep -Eq "${pattern}"; then
    fail "${name} ${path} — body missing ${pattern}"
    printf '      body: %s\n' "${body}" | head -c 400
    printf '\n'
    FAILURES=$((FAILURES + 1))
    return
  fi

  pass "${name} ${path}"
}

check_endpoint health        /api/health        '"status":"ok"'
check_endpoint capabilities  /api/capabilities  '"work_plane"'
check_endpoint beads-list    /api/beads         '"items"'
# The beads adaptor composes its default prefix ("gemba/gemba/") onto ids in
# responses, but resolves bare ids on the path. smk-1 is the first seed.
check_endpoint bead-by-id    /api/beads/smk-1   'smk-1"'

# ---- summary --------------------------------------------------------------

log ""
if [[ "${FAILURES}" -eq 0 ]]; then
  info "all endpoints healthy"
  exit 0
else
  fail "${FAILURES} endpoint check(s) failed"
  log "---- gemba log (last 40 lines) ----"
  tail -n 40 "${LOG_FILE}" || true
  exit 1
fi
