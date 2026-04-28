#!/usr/bin/env bash
#
# run.sh — build + launch gemba serve against the local beads Dolt
# server in one command.
#
# The local rig's beads database is the `gemba` schema on the shared
# Dolt server at 127.0.0.1:3307. Pointing `gemba serve` at that URL
# gives the SPA + API a read-only view of the same beads the bd CLI
# writes (per .beads/metadata.json on this rig).
#
# Build step: `make build` rebuilds the SPA into web/dist (embedded
# via go:embed) and the Go binary into bin/gemba. Skip with
# RUN_SKIP_BUILD=1 for a fast restart when only config changed.
#
# Extra args after `./run.sh` get forwarded to `gemba serve` so the
# operator can add --port, --auth, --open, etc. without editing this
# script.
#
# Usage:
#   ./run.sh                        # default port 7666, no auth
#   ./run.sh --port 7667            # different port
#   ./run.sh --open                 # also open the SPA in a browser
#   RUN_SKIP_BUILD=1 ./run.sh       # skip the rebuild step
#   GEMBA_DOLT_URL=mysql://...  ./run.sh   # override the beads URL

set -euo pipefail

# Resolve repo root from the script's location so `./run.sh` works
# regardless of cwd.
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# Default beads URL: the local Dolt server's `gemba` database. Mirrors
# the rig's `.beads/metadata.json` (dolt_server_host:dolt_server_port/
# dolt_database). Overridable via env so a tester can point at a
# different schema without editing this file.
DOLT_URL="${GEMBA_DOLT_URL:-mysql://root@127.0.0.1:3307/gemba}"

if [[ "${RUN_SKIP_BUILD:-}" != "1" ]]; then
  echo "==> building (set RUN_SKIP_BUILD=1 to skip)"
  make build
fi

if [[ ! -x "bin/gemba" ]]; then
  echo "run.sh: bin/gemba is missing or not executable; build failed?" >&2
  exit 1
fi

echo "==> launching: bin/gemba serve --dolt-url $DOLT_URL $*"
# `exec` so Ctrl-C in the launching shell terminates gemba directly
# (no orphaned child) and the script's PID becomes gemba's PID.
exec ./bin/gemba serve --dolt-url "$DOLT_URL" "$@"
