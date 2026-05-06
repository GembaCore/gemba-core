---
description: "Stage or apply a Gemba Beads sync plan for the current Spec Kit feature"
scripts:
  sh: ../scripts/bash/gemba-sync.sh
  ps: ../scripts/powershell/gemba-sync.ps1
---

# Gemba Beads Sync

Use this command after `tasks.md` has been generated for a Spec Kit
feature. It asks a running Gemba server to read the current
`spec.md` / `plan.md` / `tasks.md` artifacts, build a staged Beads
change plan, and optionally apply that plan.

## User Input

```text
$ARGUMENTS
```

If the user input contains a feature id, use it. Otherwise infer the
feature id from `SPECIFY_FEATURE`, the current feature branch, or the
most recently modified directory under `specs/`.

## Steps

1. Confirm the project root contains `.specify/` or `specs/`.
2. Load extension config from
   `.specify/extensions/gemba/gemba-config.yml` when present.
3. Apply environment overrides:
   - `GEMBA_API_BASE`
   - `GEMBA_AUTH_TOKEN`
   - `GEMBA_SYNC_AUTO_APPLY`
   - `GEMBA_SYNC_ALLOW_DELETES`
4. Run the helper script for this platform:
   - Bash: `.specify/extensions/gemba/scripts/bash/gemba-sync.sh`
   - PowerShell: `.specify/extensions/gemba/scripts/powershell/gemba-sync.ps1`
5. Print the staged create / update / delete counts and plan hash.
6. If `auto_apply` is false, stop after preview and tell the operator
   to review **Gemba -> Refine -> Spec Kit** before syncing.
7. If `auto_apply` is true, POST the plan hash back to Gemba with an
   `X-GEMBA-Confirm` nonce. Refuse delete plans unless
   `allow_deletes` is also true.

## Expected Output

```text
Gemba Spec Kit sync plan for 001-auth
create: 4 update: 1 delete: 0
hash: sha256:...
Review in Gemba: Refine -> Spec Kit
```

When auto-apply is enabled:

```text
Applied Gemba sync for 001-auth
created: 4 updated: 1 deleted: 0
```
