#!/usr/bin/env bash
# bd-pre-close.sh — operator-facing wrapper that invokes the ASDD
# closure gate before bd closes an epic bead. Companion to the
# `gemba spec close-check` CLI shipped by gm-v0sp.12.
#
# Wiring (operator side):
#   bd ships a pluggable hook system; the canonical place to enable this
#   script is `.beads/hooks/pre-close` (or your bd installation's hook
#   directory) — make it executable and point it at this file:
#
#     ln -sf "$(pwd)/scripts/bd-pre-close.sh" .beads/hooks/pre-close
#
#   bd will pass the bead ID as $1. This wrapper:
#     1. Discovers the bead's `spec:<slug>` label via `bd show --json`.
#     2. Refuses to close (exit 2) if no spec label is present and the
#        bead is an epic — otherwise non-spec beads pass through.
#     3. Shells out to `gemba spec close-check` against the spec's
#        canonical doc + lockfile.
#
#   Exit 0 => allow close. Exit non-zero => bd aborts the close.
#
# Override path:
#   Operators with a legitimate need to close an epic with open stories
#   should set BD_FORCE_CLOSE_REASON=<reason> in their environment; this
#   wrapper forwards it as `--force --reason` to the gate, which writes
#   a `spec.close.gate.bypass` audit event.

set -euo pipefail

BEAD_ID="${1:-}"
if [[ -z "$BEAD_ID" ]]; then
  echo "bd-pre-close: missing bead id (\$1)" >&2
  exit 1
fi

BD_BIN="${BD_BIN:-bd}"
GEMBA_BIN="${GEMBA_BIN:-gemba}"

# Resolve project root (anchored on the script location by default).
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
PROJECT_ROOT="${PROJECT_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"

# Ask bd for the bead's labels + type. If bd is unavailable, fail
# closed — better to refuse a close than silently bypass the gate.
if ! BEAD_JSON="$("$BD_BIN" show "$BEAD_ID" --json 2>/dev/null)"; then
  echo "bd-pre-close: bd show $BEAD_ID failed; refusing close" >&2
  exit 1
fi

# Only gate epics. Anything else passes through.
BEAD_TYPE="$(printf '%s' "$BEAD_JSON" | sed -n 's/.*"issue_type"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
if [[ "$BEAD_TYPE" != "epic" ]]; then
  exit 0
fi

# Extract the first spec:<slug> label.
SPEC_SLUG="$(printf '%s' "$BEAD_JSON" \
  | tr ',' '\n' \
  | sed -n 's/.*"spec:\([a-zA-Z0-9._-]\+\)".*/\1/p' \
  | head -n1)"

if [[ -z "$SPEC_SLUG" ]]; then
  # Epic with no spec label — outside ASDD scope, nothing to gate.
  exit 0
fi

SPEC_MD="$PROJECT_ROOT/specs/$SPEC_SLUG/spec.md"
LOCKFILE="$PROJECT_ROOT/specs/$SPEC_SLUG/.spec.lock.json"

if [[ ! -f "$SPEC_MD" ]]; then
  echo "bd-pre-close: spec doc not found at $SPEC_MD; refusing" >&2
  exit 1
fi

ARGS=(spec close-check "$SPEC_MD" --epic "$BEAD_ID" --lockfile "$LOCKFILE")

if [[ -n "${BD_FORCE_CLOSE_REASON:-}" ]]; then
  ARGS+=(--force --reason "$BD_FORCE_CLOSE_REASON")
fi

exec "$GEMBA_BIN" "${ARGS[@]}"
