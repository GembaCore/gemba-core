# Gemba Beads Sync For Spec Kit

This is an upstream-ready Spec Kit extension package for syncing Spec
Kit feature artifacts into Gemba/Beads.

It registers:

- Command: `speckit.gemba.sync`
- Hook: `after_tasks`

The hook previews the Gemba sync plan after `tasks.md` generation. It
can also auto-apply the plan when configured, but the default is preview
only so the operator still approves Beads mutations in Gemba.

## Install Locally

From a Spec Kit project:

```bash
specify extension add --dev /path/to/gemba-core/integrations/spec-kit/gemba-sync
```

Then configure the Gemba endpoint:

```bash
mkdir -p .specify/extensions/gemba
cp .specify/extensions/gemba/config-template.yml .specify/extensions/gemba/gemba-config.yml
```

Edit `.specify/extensions/gemba/gemba-config.yml` if Gemba is not
running at `http://127.0.0.1:7666/api`.

## Configuration

```yaml
api_base: "http://127.0.0.1:7666/api"
auth_token: ""
auto_apply: false
allow_deletes: false
```

Environment overrides:

- `GEMBA_API_BASE`
- `GEMBA_AUTH_TOKEN`
- `GEMBA_SYNC_AUTO_APPLY`
- `GEMBA_SYNC_ALLOW_DELETES`

## Behavior

When `auto_apply` is false:

1. Calls `GET /api/spec-kit/features/{id}/sync-plan`.
2. Prints create / update / delete counts and the plan hash.
3. Leaves approval to **Gemba -> Refine -> Spec Kit**.

When `auto_apply` is true:

1. Calls the preview route.
2. Refuses delete plans unless `allow_deletes` is true.
3. Calls `POST /api/spec-kit/features/{id}/sync-to-beads` with:

   ```json
   {
     "plan_hash": "sha256:...",
     "allow_deletes": false
   }
   ```

The hash prevents stale task-generation output from being applied after
the Spec Kit files or matching Beads have changed.

## Feature Detection

The helper script chooses the feature id in this order:

1. Command argument.
2. `SPECIFY_FEATURE`.
3. Current branch name when a matching `specs/<branch>` directory
   exists.
4. Most recently modified directory under `specs/` or
   `.specify/specs/`.

## Publishing Upstream

This package follows Spec Kit's extension manifest schema. To submit it
upstream, copy this directory into a standalone extension repository or
the Spec Kit extension catalog and update `extension.repository` to the
final repository URL.
