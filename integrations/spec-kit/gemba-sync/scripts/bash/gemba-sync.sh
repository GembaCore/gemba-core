#!/usr/bin/env bash
set -euo pipefail

FEATURE_ID="${1:-}"

find_root() {
  local dir="${PWD}"
  while [ "${dir}" != "/" ]; do
    if [ -d "${dir}/.specify" ] || [ -d "${dir}/specs" ]; then
      printf '%s\n' "${dir}"
      return 0
    fi
    dir="$(dirname "${dir}")"
  done
  return 1
}

yaml_value() {
  local file="$1"
  local key="$2"
  [ -f "${file}" ] || return 0
  grep -E "^[[:space:]]*${key}:" "${file}" 2>/dev/null |
    tail -n 1 |
    sed 's/^[^:]*:[[:space:]]*//' |
    sed 's/^["'\'']//; s/["'\'']$//'
}

bool_value() {
  local value
  value="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
  [ "${value}" = "true" ] || [ "${value}" = "1" ] || [ "${value}" = "yes" ] || [ "${value}" = "on" ]
}

infer_feature() {
  if [ -n "${FEATURE_ID}" ]; then
    printf '%s\n' "${FEATURE_ID}"
    return 0
  fi
  if [ -n "${SPECIFY_FEATURE:-}" ]; then
    printf '%s\n' "${SPECIFY_FEATURE}"
    return 0
  fi
  if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    local branch
    branch="$(git rev-parse --abbrev-ref HEAD)"
    branch="${branch##*/}"
    if [ -d "specs/${branch}" ] || [ -d ".specify/specs/${branch}" ]; then
      printf '%s\n' "${branch}"
      return 0
    fi
  fi
  local latest
  latest="$(find specs .specify/specs -mindepth 1 -maxdepth 1 -type d 2>/dev/null |
    xargs ls -td 2>/dev/null |
    head -n 1 || true)"
  if [ -n "${latest}" ]; then
    basename "${latest}"
    return 0
  fi
  return 1
}

ROOT="$(find_root || true)"
if [ -z "${ROOT}" ]; then
  echo "[gemba] Could not find a Spec Kit project root" >&2
  exit 1
fi
cd "${ROOT}"

CONFIG="${ROOT}/.specify/extensions/gemba/gemba-config.yml"
API_BASE="${GEMBA_API_BASE:-$(yaml_value "${CONFIG}" api_base)}"
AUTH_TOKEN="${GEMBA_AUTH_TOKEN:-$(yaml_value "${CONFIG}" auth_token)}"
AUTO_APPLY="${GEMBA_SYNC_AUTO_APPLY:-$(yaml_value "${CONFIG}" auto_apply)}"
ALLOW_DELETES="${GEMBA_SYNC_ALLOW_DELETES:-$(yaml_value "${CONFIG}" allow_deletes)}"
API_BASE="${API_BASE:-http://127.0.0.1:7666/api}"

FEATURE_ID="$(infer_feature)" || {
  echo "[gemba] Could not infer Spec Kit feature id" >&2
  exit 1
}

if ! command -v curl >/dev/null 2>&1; then
  echo "[gemba] curl is required" >&2
  exit 1
fi

AUTH_ARGS=()
if [ -n "${AUTH_TOKEN}" ]; then
  AUTH_ARGS=(-H "Authorization: Bearer ${AUTH_TOKEN}")
fi

PLAN_FILE="$(mktemp)"
trap 'rm -f "${PLAN_FILE}" "${APPLY_FILE:-}"' EXIT

curl -fsS "${AUTH_ARGS[@]}" \
  "${API_BASE}/spec-kit/features/${FEATURE_ID}/sync-plan" \
  -o "${PLAN_FILE}"

echo "Gemba Spec Kit sync plan for ${FEATURE_ID}"
if command -v jq >/dev/null 2>&1; then
  CREATE="$(jq -r '.counts.create' "${PLAN_FILE}")"
  UPDATE="$(jq -r '.counts.update' "${PLAN_FILE}")"
  DELETE="$(jq -r '.counts.delete' "${PLAN_FILE}")"
  HASH="$(jq -r '.hash' "${PLAN_FILE}")"
  echo "create: ${CREATE} update: ${UPDATE} delete: ${DELETE}"
  echo "hash: ${HASH}"
else
  cat "${PLAN_FILE}"
  echo
  echo "[gemba] Install jq to print concise counts or use auto_apply" >&2
  HASH=""
  DELETE="0"
fi

if ! bool_value "${AUTO_APPLY}"; then
  echo "Review in Gemba: Refine -> Spec Kit"
  exit 0
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "[gemba] jq is required for auto_apply because the plan hash must be echoed back" >&2
  exit 1
fi

if [ "${DELETE}" != "0" ] && ! bool_value "${ALLOW_DELETES}"; then
  echo "[gemba] Refusing to auto-apply ${DELETE} delete(s). Set allow_deletes true after reviewing the plan." >&2
  exit 1
fi

APPLY_FILE="$(mktemp)"
NONCE="speckit-gemba-$(date +%s)-${RANDOM:-0}"
BODY="$(jq -n --arg hash "${HASH}" --argjson allow "$(bool_value "${ALLOW_DELETES}" && echo true || echo false)" '{plan_hash:$hash, allow_deletes:$allow}')"

curl -fsS "${AUTH_ARGS[@]}" \
  -H "Content-Type: application/json" \
  -H "X-GEMBA-Confirm: ${NONCE}" \
  -X POST \
  -d "${BODY}" \
  "${API_BASE}/spec-kit/features/${FEATURE_ID}/sync-to-beads" \
  -o "${APPLY_FILE}"

echo "Applied Gemba sync for ${FEATURE_ID}"
jq -r '"created: \((.created // []) | length) updated: \((.updated // []) | length) deleted: \((.deleted // []) | length)"' "${APPLY_FILE}"
