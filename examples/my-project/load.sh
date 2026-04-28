#!/usr/bin/env bash
# load.sh — bootstrap a scratch beads rig from seed.json (gm-57p6).
#
# Usage:
#   examples/my-project/load.sh [TARGET_DIR] [PREFIX]
#
# When TARGET_DIR is omitted, the rig is created at /tmp/my-project-rig
# so this script never clobbers an operator's working directory by
# accident. PREFIX defaults to 'mp'.
#
# Prerequisite: a Dolt server is running on 127.0.0.1:3307 (the gt
# town convention; see ~/gt/CLAUDE.md). bd init points at it via
# --external --server flags so the rig database is registered with
# the running server rather than spinning up its own embedded
# instance — important on dev boxes where embedded mode would
# port-conflict with the town's shared server.
#
# After creating issues, the script progresses a deliberate slice
# through the workflow so the SPA's Board view shows cards in
# multiple columns (a few completed, a few in progress) — a
# never-touched seed is visually flat. Progression is name-driven
# (the keys from seed.json) so a re-run produces the same shape.

set -euo pipefail

TARGET="${1:-/tmp/my-project-rig}"
PREFIX="${2:-mp}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SEED="${SCRIPT_DIR}/seed.json"

if [[ ! -f "${SEED}" ]]; then
  echo "load.sh: seed file missing at ${SEED}" >&2
  exit 1
fi

echo "==> Preparing target rig: ${TARGET}"
rm -rf "${TARGET}"
mkdir -p "${TARGET}"

# Drop any orphaned 'mp' database left on the shared Dolt server by a
# prior run — without this, bd init silently re-uses the existing
# database and a re-run produces double-seeded data. cleanup is a
# no-op when nothing's orphaned.
if command -v gt >/dev/null 2>&1; then
  gt dolt cleanup --force >/dev/null 2>&1 || true
fi

(cd "${TARGET}" && BD_NON_INTERACTIVE=1 bd init \
  -p "${PREFIX}" \
  --server --external \
  --server-host 127.0.0.1 \
  --server-port 3307 \
  --server-user root \
  --non-interactive >/dev/null 2>&1)

echo "==> Loading seed graph"
graph_out=$(cd "${TARGET}" && bd create --graph "${SEED}" 2>&1)

# Map seed keys → assigned bd ids (lines look like "  key -> mp-xxxx").
key_to_id() {
  echo "${graph_out}" | awk -v k="$1" '$1==k{print $3; exit}'
}

# Walk the workflow forward by a believable amount. Closes are the
# beads that you'd already have shipped at this point in MVP work;
# in-progress are what the team is touching now.
CLOSED_KEYS=(f1 a1 d1 d2)
IN_PROGRESS_KEYS=(a2 a3 d3 f2 u1)

echo "==> Progressing workflow state"
for k in "${CLOSED_KEYS[@]}"; do
  id=$(key_to_id "${k}")
  if [[ -n "${id}" ]]; then
    (cd "${TARGET}" && bd close "${id}" -m "shipped" >/dev/null 2>&1) || true
  fi
done
for k in "${IN_PROGRESS_KEYS[@]}"; do
  id=$(key_to_id "${k}")
  if [[ -n "${id}" ]]; then
    (cd "${TARGET}" && bd update "${id}" --status in_progress >/dev/null 2>&1) || true
  fi
done

echo "==> Done. ${TARGET}/.beads/ ready."
echo
echo "Boot Gemba against the sample:"
echo "  ./bin/gemba serve --beads-dir ${TARGET} --port 7666"
echo "  open http://127.0.0.1:7666"
