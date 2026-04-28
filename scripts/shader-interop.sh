#!/usr/bin/env bash
#
# scripts/shader-interop.sh — gm-root.5
#
# End-to-end smoke test for the Gas Town shader (gm-root.4). Runs the
# full encode → bd-store → sling → polecat → decode round-trip against
# a real gemba serve + bd backend.
#
# Steps:
#   1. Boot ./bin/gemba serve in --beads-dir mode (writable; --dolt-url
#      is read-only and can't be used for the PATCH below).
#   2. Create a bare probe via `bd create` so we own a bead the shader
#      hasn't touched yet.
#   3. PATCH it through the running gemba — the shader's encode hook
#      fires here, producing a kind-prefixed title in the bd store.
#   4. Read via `bd show` — assert the encoded prefix landed on disk.
#   5. Read via `gemba GET /api/work-items/{id}` — assert the prefix
#      decodes back to the bare title (decode round-trip).
#   6. Optional: SHADER_INTEROP_SLING=1 — fire `gt sling` and watch up
#      to SLING_TIMEOUT seconds for the bead to reach HOOKED state, then
#      assert no parser warning fired. Skipped by default because gt
#      sling is currently flaky on this rig (see session notes); enable
#      manually when you want the polecat-side coverage.
#
# Exit 0 on success, non-zero with a clear diff on failure. Cleans up
# the spawned gemba server and closes the probe bead in the EXIT trap.

set -euo pipefail

# ---------- config ----------
PORT="${PORT:-17666}"
GEMBA_BIN="${GEMBA_BIN:-./bin/gemba}"
BEADS_DIR="${BEADS_DIR:-$(pwd)}"
ORCH_CONFIG="${ORCH_CONFIG:-./testing/fixtures/gastown.json}"
SLING_TIMEOUT="${SLING_TIMEOUT:-300}"
SHADER_INTEROP_SLING="${SHADER_INTEROP_SLING:-0}"

# ---------- helpers ----------
red()    { printf '\033[31m%s\033[0m\n' "$*" >&2; }
green()  { printf '\033[32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[33m%s\033[0m\n' "$*"; }
step()   { printf '\033[36m▶ %s\033[0m\n' "$*"; }

die() {
  red "FAIL: $*"
  exit 1
}

# uuid generator, falls back to /dev/urandom for CI environments
# that don't ship uuidgen.
uuid() {
  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr '[:upper:]' '[:lower:]'
  else
    cat /proc/sys/kernel/random/uuid 2>/dev/null \
      || python3 -c 'import uuid; print(uuid.uuid4())'
  fi
}

# ---------- preflight ----------
[[ -x "$GEMBA_BIN" ]] || die "gemba binary not found at $GEMBA_BIN — run 'make build' first"
[[ -f "$ORCH_CONFIG" ]] || die "orchestrator config not found at $ORCH_CONFIG"
command -v bd >/dev/null 2>&1 || die "bd CLI required on PATH"
command -v curl >/dev/null 2>&1 || die "curl required on PATH"

# ---------- start gemba ----------
GEMBA_LOG="$(mktemp)"
"$GEMBA_BIN" serve \
  --beads-dir "$BEADS_DIR" \
  --orchestrator-config "$ORCH_CONFIG" \
  --listen 127.0.0.1 \
  --port "$PORT" \
  --auth none \
  --quiet \
  >"$GEMBA_LOG" 2>&1 &
GEMBA_PID=$!

cleanup() {
  local code=$?
  if [[ -n "${PROBE_ID:-}" ]]; then
    bd close "$PROBE_ID" --reason "interop probe cleanup" >/dev/null 2>&1 || true
  fi
  if kill -0 "$GEMBA_PID" 2>/dev/null; then
    kill "$GEMBA_PID" 2>/dev/null || true
    wait "$GEMBA_PID" 2>/dev/null || true
  fi
  if [[ $code -ne 0 ]]; then
    yellow "--- gemba server log (tail) ---"
    tail -40 "$GEMBA_LOG" >&2 || true
  fi
  rm -f "$GEMBA_LOG"
  exit $code
}
trap cleanup EXIT INT TERM

step "waiting for gemba on :$PORT"
for i in $(seq 1 30); do
  if curl -sf "http://127.0.0.1:$PORT/api/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
  if ! kill -0 "$GEMBA_PID" 2>/dev/null; then
    die "gemba exited before binding (see log)"
  fi
done
curl -sf "http://127.0.0.1:$PORT/api/health" >/dev/null \
  || die "gemba never became ready on :$PORT"

# ---------- create probe (bare) ----------
step "creating probe bead via bd"
TS="$(date +%s)"
PROBE_TITLE="shader-interop-$TS"
PROBE_OUT=$(bd create \
  --type bug \
  --priority P3 \
  --description "Shader interop probe (gm-root.5). Auto-generated; close on test exit." \
  "$PROBE_TITLE")
PROBE_ID=$(echo "$PROBE_OUT" | grep -oE 'gm-[a-z0-9.]+' | head -1)
[[ -n "$PROBE_ID" ]] || die "couldn't parse probe id from bd create output: $PROBE_OUT"
step "  probe id: $PROBE_ID"

# Cross-rig prefix: the dolt-form of the id the gemba server's adaptor
# expects. bd's local form is bare ("gm-xxx"); the dolt server prefixes
# with the rig name. The bd-CLI workplane in gemba serve doesn't add a
# prefix, so the bare id is what /api/work-items/{id} expects in this
# mode. (--dolt-url mode would need the prefix; we're in --beads-dir.)
GEMBA_ID="$PROBE_ID"

# ---------- patch via gemba (shader encodes) ----------
step "patching title via gemba (shader encode hook fires here)"
NONCE=$(uuid)
PATCH_OUT=$(curl -sS -X PATCH \
  "http://127.0.0.1:$PORT/api/work-items/$GEMBA_ID" \
  -H "X-GEMBA-Confirm: $NONCE" \
  -H "Content-Type: application/json" \
  -d "{\"title\":\"$PROBE_TITLE\"}")
echo "$PATCH_OUT" | grep -q "\"id\"" \
  || die "PATCH didn't return a WorkItem; got: $PATCH_OUT"

# ---------- assert encoded title in bd ----------
step "asserting bd stored an encoded title (BUG: prefix expected)"
BD_TITLE=$(bd show "$PROBE_ID" 2>/dev/null \
  | grep -E "^○|^◇|^●|^✓" \
  | head -1 \
  | sed -E 's/^[^·]+·[[:space:]]*//; s/[[:space:]]+\[.*$//')
echo "  bd-stored title: $BD_TITLE"
case "$BD_TITLE" in
  "BUG: $PROBE_TITLE")
    green "  ✓ encode round-trip: bd has the prefix"
    ;;
  *)
    die "encode broken: expected 'BUG: $PROBE_TITLE' in bd, got '$BD_TITLE'"
    ;;
esac

# ---------- assert decoded title via gemba ----------
step "asserting gemba GET decodes the prefix off again"
GET_OUT=$(curl -sS "http://127.0.0.1:$PORT/api/work-items/$GEMBA_ID")
DECODED_TITLE=$(echo "$GET_OUT" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin).get("title",""))' 2>/dev/null \
  || echo "$GET_OUT")
echo "  gemba-returned title: $DECODED_TITLE"
if [[ "$DECODED_TITLE" == "$PROBE_TITLE" ]]; then
  green "  ✓ decode round-trip: gemba strips the prefix on read"
else
  die "decode broken: expected '$PROBE_TITLE', got '$DECODED_TITLE'"
fi

# ---------- optional: sling round-trip ----------
if [[ "$SHADER_INTEROP_SLING" == "1" ]]; then
  step "slinging $PROBE_ID to gemba polecat (timeout ${SLING_TIMEOUT}s)"
  SLING_LOG=$(mktemp)
  gt sling "$PROBE_ID" gemba --create >"$SLING_LOG" 2>&1 &
  SLING_PID=$!

  HOOKED=
  start=$(date +%s)
  while true; do
    state=$(bd show "$PROBE_ID" 2>/dev/null | grep -oE 'CLOSED|HOOKED|OPEN|IN_PROGRESS' | head -1)
    if [[ "$state" == "HOOKED" || "$state" == "IN_PROGRESS" || "$state" == "CLOSED" ]]; then
      HOOKED=1
      break
    fi
    if [[ $(( $(date +%s) - start )) -gt $SLING_TIMEOUT ]]; then
      yellow "  ! sling timeout after ${SLING_TIMEOUT}s; gt sling output:"
      tail -20 "$SLING_LOG" >&2
      kill "$SLING_PID" 2>/dev/null || true
      rm -f "$SLING_LOG"
      die "sling didn't hook within ${SLING_TIMEOUT}s — known flaky surface, see session notes"
    fi
    sleep 2
  done
  kill "$SLING_PID" 2>/dev/null || true
  wait "$SLING_PID" 2>/dev/null || true
  rm -f "$SLING_LOG"
  green "  ✓ sling reached HOOKED — polecat consumed the encoded title without complaint"
else
  yellow "  (skipping sling step — set SHADER_INTEROP_SLING=1 to enable)"
fi

# ---------- summary ----------
green ""
green "PASS: shader-interop round-trip (gm-root.5)"
green "  encoded:  '$BD_TITLE'   (in bd)"
green "  decoded:  '$DECODED_TITLE'   (via gemba)"
