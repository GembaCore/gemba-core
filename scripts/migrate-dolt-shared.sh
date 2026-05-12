#!/usr/bin/env bash
#
# migrate-dolt-shared.sh
#
# Reconcile a single shared dolt sql-server (port 3307, data dir ~/gt/.dolt-data)
# that serves every project's beads database, while leaving each project's
# bd git-remote backup untouched.
#
# Scope after audit (2026-05-11):
#   1. Stop the rogue dolt server currently bound to :3307.
#      Its data dir is ~/Documents/GitHub/ai-intelligence-system/.beads/dolt/
#      (single repo). We move that one repo into ~/gt/.dolt-data/.
#   2. Archive stale duplicate dolt repos at ~/gt/gemba/.beads/dolt/
#      (contains stale gemba + ai_intelligence_system copies — NOT the live
#      data; live gemba data is already at ~/gt/.dolt-data/gemba/).
#   3. Backfill missing .beads/dolt-server.port files for foolery and
#      lume_spark_api (both are registered rigs with data already in the
#      central dir but no pointer).
#   4. Start one canonical dolt sql-server rooted at ~/gt/.dolt-data.
#   5. Verify each project: rig and self-managed alike.
#
# Beads remotes (sync.remote in each project's .beads/config.yaml) are NEVER
# touched. They remain per-project and continue to work independently.
#
# Usage:
#   DRY_RUN=1 ./migrate-dolt-shared.sh   # plan, no changes
#   ./migrate-dolt-shared.sh             # execute

set -euo pipefail

PORT="${PORT:-3307}"
DATA_DIR="${DATA_DIR:-$HOME/gt/.dolt-data}"
LOG_DIR="${LOG_DIR:-$HOME/gt/.dolt-data/_logs}"
ARCHIVE_DIR="${ARCHIVE_DIR:-$HOME/gt/.dolt-data/_archive}"
DRY_RUN="${DRY_RUN:-0}"
TS="$(date +%Y%m%d-%H%M%S)"

# Single project whose dolt repo must MOVE into DATA_DIR.
# Format: "<source_dolt_repo_path>:<repo_path>:<db_name>"
MIGRATE=(
  "/Users/mikebengtson/Documents/GitHub/ai-intelligence-system/.beads/dolt:/Users/mikebengtson/Documents/GitHub/ai-intelligence-system:ai_intelligence_system"
)

# Stale .beads/dolt dirs to ARCHIVE (move out of the way; do not delete).
# Format: "<stale_path>"
ARCHIVE=(
  "/Users/mikebengtson/gt/gemba/.beads/dolt"
)

# Projects to (re)write pointer files for.
# Format: "<repo_path>:<db_name>"
POINTERS=(
  # registered gt rigs
  "/Users/mikebengtson/gt/foolery:foolery"
  "/Users/mikebengtson/gt/gemba:gemba"
  "/Users/mikebengtson/gt/gemba_prime:gemba_prime"
  "/Users/mikebengtson/gt/lume_spark:lume_spark"
  "/Users/mikebengtson/gt/lume_spark_api:lume_spark_api"
  "/Users/mikebengtson/gt/t3code:t3code"
  # self-managed under ~/gt
  "/Users/mikebengtson/gt/gastown:gastown"
  "/Users/mikebengtson/gt/mp:mp"
  "/Users/mikebengtson/gt/sb:sb"
  "/Users/mikebengtson/gt/second_brain:second_brain"
  # HQ
  "/Users/mikebengtson/gt:hq"
  # external self-managed (after MIGRATE moves its data)
  "/Users/mikebengtson/Documents/GitHub/ai-intelligence-system:ai_intelligence_system"
)

# ---------------------------------------------------------------------------

run() {
  if [[ "$DRY_RUN" == "1" ]]; then printf '  [dry] %s\n' "$*"
  else printf '  [run] %s\n' "$*"; eval "$@"
  fi
}
note()  { printf '\n=== %s ===\n' "$*"; }
info()  { printf '    %s\n' "$*"; }
warn()  { printf '  ! %s\n' "$*" >&2; }
fail()  { printf '  X %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------

note "Preflight"
[[ -d "$DATA_DIR" ]] || fail "DATA_DIR does not exist: $DATA_DIR"
command -v dolt >/dev/null || fail "dolt not found in PATH"
command -v bd   >/dev/null || fail "bd not found in PATH"
command -v lsof >/dev/null || fail "lsof not found in PATH"
command -v jq   >/dev/null || warn "jq not found — metadata.json patches will be skipped"
info "PORT      = $PORT"
info "DATA_DIR  = $DATA_DIR"
info "ARCHIVE   = $ARCHIVE_DIR"
info "DRY_RUN   = $DRY_RUN"

# ---------------------------------------------------------------------------

note "Stop dolt server on :$PORT"
PID="$(lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t 2>/dev/null || true)"
if [[ -n "$PID" ]]; then
  CMD="$(ps -o command= -p "$PID" 2>/dev/null || true)"
  CWD="$(lsof -p "$PID" 2>/dev/null | awk '$4=="cwd"{print $NF; exit}')"
  info "PID=$PID"
  info "cmd : $CMD"
  info "cwd : $CWD"
  [[ "$CMD" == *"dolt"* ]] || fail "PID $PID on :$PORT is not dolt — refusing to kill"
  run "kill $PID"
  if [[ "$DRY_RUN" != "1" ]]; then
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      sleep 0.5
      lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t >/dev/null 2>&1 || break
    done
    lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t >/dev/null 2>&1 \
      && fail "PID $PID still holding :$PORT after 5s"
  fi
else
  info "nothing bound to :$PORT"
fi

# ---------------------------------------------------------------------------

note "Archive stale .beads/dolt dirs"
mkdir -p "$ARCHIVE_DIR" 2>/dev/null || true
for stale in "${ARCHIVE[@]}"; do
  if [[ -d "$stale" ]]; then
    name="$(echo "$stale" | sed 's|/|_|g; s|^_||')"
    dst="$ARCHIVE_DIR/${name}.$TS"
    info "archive $stale → $dst"
    run "mv \"$stale\" \"$dst\""
  else
    info "skip (not present): $stale"
  fi
done

# ---------------------------------------------------------------------------

note "Migrate project-local dolt repos into $DATA_DIR"
for entry in "${MIGRATE[@]}"; do
  src_root="${entry%%:*}"; rest="${entry#*:}"
  repo="${rest%%:*}";      db="${rest#*:}"
  # src_root is a directory containing one or more dolt repos; we expect exactly one named <db>
  src_repo="$src_root/$db"
  dst="$DATA_DIR/$db"
  info "$repo  →  db=$db"
  if [[ -d "$dst" ]]; then
    warn "  destination $dst already exists — leaving in place (assumes already migrated)"
    continue
  fi
  if [[ -d "$src_repo/.dolt" ]]; then
    run "mv \"$src_repo\" \"$dst\""
  elif [[ -d "$src_root/.dolt" ]]; then
    # rogue server's data-dir IS the single repo (no nested dirname)
    run "mv \"$src_root\" \"$dst\""
    run "mkdir -p \"$src_root\""
    info "  recreated empty $src_root as placeholder"
  else
    warn "  no dolt repo at $src_repo or $src_root — nothing to migrate"
  fi
done

# ---------------------------------------------------------------------------

note "(Re)write per-project .beads pointers"
write_pointer() {
  local repo="$1" db="$2"
  local mdir="$repo/.beads"
  [[ -d "$mdir" ]] || { warn "no $mdir — skipping"; return; }
  run "printf '%s' '$PORT' > \"$mdir/dolt-server.port\""
  local meta="$mdir/metadata.json"
  if [[ -f "$meta" && -n "$(command -v jq)" ]]; then
    if [[ "$DRY_RUN" == "1" ]]; then
      info "  [dry] jq edit $meta (drop dolt_server_port; set dolt_database=\"$db\")"
    else
      tmp="$(mktemp)"
      jq --arg db "$db" 'del(.dolt_server_port) | .dolt_database = $db' \
        "$meta" > "$tmp" && mv "$tmp" "$meta"
      info "  patched $meta"
    fi
  fi
}
for entry in "${POINTERS[@]}"; do
  repo="${entry%%:*}"; db="${entry##*:}"
  info "$repo → db=$db"
  write_pointer "$repo" "$db"
done

# ---------------------------------------------------------------------------

note "Start canonical dolt sql-server"
mkdir -p "$LOG_DIR" 2>/dev/null || true
LOG_FILE="$LOG_DIR/dolt-$(date +%Y%m%d).log"
START="nohup dolt sql-server -H 127.0.0.1 -P $PORT --data-dir \"$DATA_DIR\" --loglevel=warning >> \"$LOG_FILE\" 2>&1 &"
if [[ "$DRY_RUN" == "1" ]]; then
  info "[dry] $START"
else
  eval "$START"
  disown || true
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    sleep 0.5
    lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t >/dev/null 2>&1 && break
  done
  lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t >/dev/null 2>&1 \
    || fail "dolt did not bind :$PORT — check $LOG_FILE"
  info "server up; log → $LOG_FILE"
fi

# ---------------------------------------------------------------------------

note "Verify"
if [[ "$DRY_RUN" == "1" ]]; then
  info "[dry] would: SHOW DATABASES + bd ready per project"
  exit 0
fi
( cd "$DATA_DIR" && dolt sql -q "SHOW DATABASES;" 2>&1 | sed 's/^/    /' )

for entry in "${POINTERS[@]}"; do
  repo="${entry%%:*}"; db="${entry##*:}"
  [[ -d "$repo/.beads" ]] || continue
  printf '\n  bd ready @ %s (db=%s):\n' "$repo" "$db"
  ( cd "$repo" && bd ready --json 2>&1 | head -c 250; echo )
done

note "Done"
cat <<EOF

Beads remote backups are independent — verify per project:
  cd <project> && bd push      # JSONL → that project's git remote

Server PID: $(lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t 2>/dev/null || echo '?')
Server log: $LOG_FILE
Archived:   $ARCHIVE_DIR
EOF
