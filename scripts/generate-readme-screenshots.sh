#!/usr/bin/env bash
# generate-readme-screenshots.sh (gm-57p6)
#
# Boot gemba serve against the My Project sample rig, run the
# dark-mode screenshot playwright spec, kill gemba.
#
# Usage:  scripts/generate-readme-screenshots.sh [RIG_DIR] [PORT]
#         RIG_DIR defaults to /tmp/my-project-rig (created by
#         examples/my-project/load.sh).
#         PORT defaults to 17890 to avoid stomping on a dev gemba
#         already on 7666.
#
# Prereq:  examples/my-project/load.sh has been run at least once.
#          ./bin/gemba is built (run `make build` if not).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RIG="${1:-/tmp/my-project-rig}"
PORT="${2:-17890}"

if [[ ! -d "${RIG}/.beads" ]]; then
  echo "screenshots: rig missing at ${RIG} — run examples/my-project/load.sh first" >&2
  exit 1
fi

if [[ ! -x "${ROOT}/bin/gemba" ]]; then
  echo "screenshots: bin/gemba missing — run make build" >&2
  exit 1
fi

echo "==> Booting gemba on :${PORT} against ${RIG}"
"${ROOT}/bin/gemba" serve --project-dir "${RIG}" --port "${PORT}" --quiet \
  >/tmp/gemba-screenshots.log 2>&1 &
GEMBA_PID=$!
trap 'kill "${GEMBA_PID}" 2>/dev/null || true' EXIT

# Wait up to 15s for /api/health.
for i in $(seq 1 30); do
  if curl -sf "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
if ! curl -sf "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
  echo "screenshots: gemba did not become healthy on :${PORT}" >&2
  cat /tmp/gemba-screenshots.log >&2
  exit 1
fi
echo "==> gemba healthy"

mkdir -p "${ROOT}/docs/img"

echo "==> Running playwright screenshot spec"
cd "${ROOT}/testing/e2e"
GEMBA_E2E_BASE_URL="http://127.0.0.1:${PORT}" \
GEMBA_E2E_SCREENSHOT_OUT="${ROOT}/docs/img" \
  pnpm exec playwright test specs/screenshots/readme.spec.ts \
    --project=route-fake --reporter=list

echo "==> Wrote:"
ls -la "${ROOT}/docs/img/screenshot-"*.png 2>/dev/null || true
