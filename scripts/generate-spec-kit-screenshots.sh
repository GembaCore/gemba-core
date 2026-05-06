#!/usr/bin/env bash
# generate-spec-kit-screenshots.sh
#
# Build a disposable Spec Kit + Beads workspace, run gemba serve
# against it, drive the Spec Kit -> Beads sync in Playwright, and write
# screenshots to docs/img.
#
# Usage: scripts/generate-spec-kit-screenshots.sh [TARGET_DIR] [PORT]

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="${1:-/tmp/gemba-spec-kit-rig}"
PORT="${2:-17891}"
FIXTURE="${ROOT}/testing/e2e/fixtures/spec-kit/pixel-avatar"
OUT_DIR="${ROOT}/docs/img"
LOG="${TMPDIR:-/tmp}/gemba-spec-kit-screenshots.log"

if [[ ! -d "${FIXTURE}/specs" ]]; then
  echo "spec-kit screenshots: fixture missing at ${FIXTURE}" >&2
  exit 1
fi

if [[ ! -x "${ROOT}/bin/gemba" ]]; then
  echo "spec-kit screenshots: bin/gemba missing; running make build" >&2
  (cd "${ROOT}" && make build)
fi

echo "==> Preparing Spec Kit sample rig: ${TARGET}"
rm -rf "${TARGET}"
mkdir -p "${TARGET}" "${TARGET}/.home" "${OUT_DIR}"
rm -f "${OUT_DIR}"/spec-kit-*.png
cp -R "${FIXTURE}/specs" "${TARGET}/specs"
cp "${FIXTURE}/SOURCE.md" "${TARGET}/SOURCE.md"

echo "==> Initializing isolated Beads workspace"
(
  cd "${TARGET}"
  HOME="${TARGET}/.home" \
  BEADS_DIR="${TARGET}/.beads" \
  BD_NON_INTERACTIVE=1 \
    bd init --prefix sk --non-interactive --quiet --skip-agents --skip-hooks
)

echo "==> Booting gemba on :${PORT}"
HOME="${TARGET}/.home" \
BEADS_DIR="${TARGET}/.beads" \
"${ROOT}/bin/gemba" serve --project-dir "${TARGET}" --port "${PORT}" --orchestration=none --quiet \
  >"${LOG}" 2>&1 &
GEMBA_PID=$!
trap 'kill "${GEMBA_PID}" 2>/dev/null || true' EXIT

for _ in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
if ! curl -sf "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
  echo "spec-kit screenshots: gemba did not become healthy on :${PORT}" >&2
  cat "${LOG}" >&2
  exit 1
fi

echo "==> Running Spec Kit -> Beads screenshot spec"
cd "${ROOT}/testing/e2e"
GEMBA_E2E_BASE_URL="http://127.0.0.1:${PORT}" \
GEMBA_E2E_SCREENSHOT_OUT="${OUT_DIR}" \
  pnpm exec playwright test specs/screenshots/spec-kit-beads.spec.ts \
    --project=route-fake --reporter=list

echo "==> Wrote:"
ls -la "${OUT_DIR}"/spec-kit-*.png
