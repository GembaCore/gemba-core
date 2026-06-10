# m1-smoke fixture

Seed data for `scripts/m1-smoke.sh`. The script copies `seeds.jsonl` into a
freshly-initialised ephemeral `.beads/` directory (embedded Dolt, no shared
server) and then launches `gemba serve` against it to curl-check the M1
milestone endpoints.

Keeping the seed as JSONL (bd's `bd export` / `bd import` wire format) means
the fixture is a couple of readable text lines in git — no binary Dolt state
committed, no shared-server pollution on whoever runs the script.
