---
title: "Pool Config Editor — adaptor-aware SPA editor for sticky session pools"
decision: gm-s47n.15
---

# Pool Config Editor — adaptor-aware SPA editor for sticky session pools

> Status: **ratified** — 2026-04-30 (architect verbal greenlight; build proceeds)
> Owner: gemba mayor
> Scope: an SPA editor for `pool.toml` (gm-s47n.10) that adapts its UI
> to whichever orchestration adaptor is bound, splits config ownership
> cleanly between gemba and the adaptor, and shells to the adaptor's
> CLI for changes that aren't gemba's to write.

## §1. Why this exists

`gm-s47n.12` shipped pool config but only via TOML file editing —
no UI affordance. Operators discover pool sizing at the pace they
read `docs/deployment/pool-sizing.md`. An interactive editor:

1. Surfaces pool config as a settings page with constrained inputs
   (no typo'd persona ids, no out-of-range floors, no clamped sizes
   without warning).
2. Imports adaptor-managed entities (rigs, personas, polecats from
   gt; personas from `.gemba/personas/*.toml` on native) instead of
   asking the operator to type strings.
3. Distinguishes mutations the editor owns (gemba's `pool.toml`)
   from mutations it merely *invokes* on the adaptor (`gt rig create`,
   `gt polecat create`, `gt config set scheduler.*`).

## §2. Naming: `scope`, not `rig`

`rig` is gt-specific. The pool key as defined in
`docs/design/session-pool.md` is `(rig, persona)` — the first axis
is genuinely a **boundary of grouping for pools that share local
resources**. For gt that boundary is the rig. For native it is the
single gemba server (collapses to a singleton).

The data-model axis is renamed `scope`. UI labels are
adaptor-aware:

- **gt-bound editor** labels it **"Rig"** (matches operator vocabulary).
- **native-bound editor** hides the axis entirely — there is one
  implicit local scope per server.

### TOML migration

`internal/config/pool.go` accepts both `[pool.<scope>.<persona>]`
and `[pool.<rig>.<persona>]` for one release. When the file decodes
with `<rig>`-style keys, gemba emits a `WARN config: rig is now
scope; please rename …` line. New writes from the editor use
`<scope>`. The deprecation alias is removed in the release after
that.

`PoolConfig.Pools` Go field renames `map[rig]map[persona]PoolEntry`
→ `map[scope]map[persona]PoolEntry`. Test data + `pool_test.go`
update. Banner / startup log strings update. `/api/pools` JSON
field renames `rig` → `scope`.

## §3. Ownership split — the load-bearing rule

Every knob the editor touches falls into exactly one of three
buckets. The UI's behavior on save depends on which.

### §3.1 Gemba-owned (always written to `pool.toml`)

| Knob | Notes |
|------|-------|
| `default_size` | rig-level fallback |
| `default_persona` | three-layer routing fallback |
| `default_floor` | auto-dispatch score floor cascade |
| `reserved_for_manual` | clamp arithmetic |
| `routing.<bead-kind>` → persona-id | middle layer of routing cascade |
| per-pool `size` | clamped against MaxParallel |
| per-pool `floor` | overrides default_floor |
| per-pool `recycle_after_beads` | safety belt |
| per-pool `idle_ceiling_minutes` | reaper threshold |
| per-pool `min_interval_per_session_seconds` | dispatch rate limit |
| per-pool `max_concurrent` | pool-scoped concurrency cap |

### §3.2 Adaptor-owned (read-only in editor; surfaced via adaptor CLI)

| Knob | Native | gt |
|------|--------|----|
| Scope identity | implicit (singleton) | gt rigs |
| Persona definition | `.gemba/personas/*.toml` | `gt agents` registry |
| Agent runtime registry | `agents.toml` | gt internal |
| Polecat lifecycle | n/a (panes are spawned per dispatch) | `gt polecat list` |

The editor reads these via the orchestration plane's existing
`ListAgents` / `ListGroups` / `ResolveGroupMembers` (gt) or via a new
`/api/personas` (native) but **never writes them directly**. Mutation
is via shell-out to the adaptor CLI.

### §3.3 Adaptor-CLI mutations (gt-only buttons in editor)

| UI Action | Shells to | Gated by |
|-----------|-----------|----------|
| "Add rig" | `gt rig create <name>` | confirm-nonce + modal |
| "Add polecat" | `gt polecat create <rig> <name>` | confirm-nonce + modal |
| "Edit gt scheduler max_polecats" | `gt config set scheduler.max_polecats <n>` | confirm-nonce + modal |

A "Run" confirmation modal shows the exact command before execution
and surfaces stdout/stderr after. Every adaptor-CLI mutation is
nonce-gated identically to PATCH/POST endpoints.

These endpoints return `KindUnsupported` when the bound adaptor is
native. The editor's adaptor-aware branch hides the buttons in that
case so the user never sees a 503 path.

## §4. Adaptor detection

The editor reads `/api/capabilities` (existing — `useCapabilities()`
in `web/src/capabilities/`):

```ts
const adaptorID = orchestrationPlane?.adaptor_id;
// "native" | "gastown" | undefined
```

Three branches:
- `native` → render NativeFlow (§5)
- `gastown` → render GTFlow (§6)
- `undefined` / unknown → render UnsupportedAdaptor placeholder
  with a raw-TOML editor as escape hatch

## §5. Native flow

### §5.1 Layout

```
┌ Pool Dispatch · Native ──────────────────────────────┐
│ File: [.gemba/pool.toml] [📂] [Reload]              │
├ Server defaults ────────────────────────────────────┤
│ Default persona [▼ engineer-claude]                  │
│ Default size  [0]   Default floor [─●─] 0.5         │
│ Reserved for manual [1]                              │
├ Routing ────────────────────────────────────────────┤
│ epic → [▼ pm-claude]    [×]                         │
│ bug  → [▼ engineer-claude]    [×]                   │
│ [+ Add routing rule]                                 │
├ Pools ──────────────────────────────────────────────┤
│ Persona [▼ engineer-claude] · Agent [▼ claude]       │
│ size [3]  floor [0.4]  recycle [5]  idle [30m]      │
│ max_concurrent [4]  min_interval [300s]              │
│ ⚠ MaxParallel=4 → effective size will clamp to 3    │
│ [+ Add pool]                                         │
├ Personas (read-only) ───────────────────────────────┤
│ • engineer-claude  • pm-claude  • documentarian      │
│ [Open .gemba/personas/]                              │
├ Generated TOML ─────────────────────────────────────┤
│ <live preview>                                       │
└ [Cancel] [Save] · ⚠ restart to apply ───────────────┘
```

### §5.2 Data sources

- **Personas dropdown** — `GET /api/personas` returns the list of
  `.gemba/personas/*.toml` files (id, name, declared agent_type if
  present, declared skills).
- **Agent types dropdown** — `GET /api/agents` (existing — used by
  the SPA's session-start flow already) returns the local
  `agents.toml` registry.
- **MaxParallel** — `GET /api/pool-config` returns
  `{path, body, parsed, max_parallel, reserved_for_manual}`. The
  `max_parallel` field comes from `agents.toml`'s aggregated
  `max_parallel` (matching gm-s47n.12's resolution path).
- **Bead kind dropdown** — frontend constant: `epic`, `task`,
  `bug`, `decision`, `feature`, `chore` (matches
  `internal/cli/bead.go:104` and `core/state.go`).

### §5.3 Save

`PUT /api/pool-config` with body = JSON of `PoolConfig`. Server
serializes via `BurntSushi/toml.Marshal`, writes to `path`,
returns the rendered TOML body. Nonce-gated via the existing
`requireConfirmNonce` middleware.

The save does NOT restart the daemon. A persistent banner under
the Save button reads "Changes apply on next server restart" until
the operator explicitly dismisses it. Hot-reload is filed as a
follow-up.

## §6. gt flow

### §6.1 Layout

```
┌ Pool Dispatch · Gas Town ────────────────────────────┐
│ Pools dispatch via `gt sling` to rigs.               │
│ Config: [.gemba/pool.toml]    [Refresh from gt]      │
├ Imported from gt (read-only) ───────────────────────┤
│ Rigs:     gemba · lume · sb · t3code                 │
│ Polecats: per-rig list                               │
│ [+ New rig]    [+ New polecat]                       │
│ Edit gt scheduler → [Run `gt config set …`]          │
├ Server defaults (gemba) ────────────────────────────┤
│ Default persona [▼ pm]    Default size [0]           │
│ Default floor [─●─] 0.5    Reserved for manual [1]   │
├ Routing ────────────────────────────────────────────┤
│ epic → rig [▼ gemba] · persona [▼ pm]      [×]      │
│ [+ Add routing rule]                                 │
├ Pools (gemba daemon params per rig/persona) ────────┤
│ Rig [▼ gemba] · Persona [▼ engineer]                 │
│ ⚠ no polecat exists for this pair                   │
│ [Run `gt polecat create gemba engineer`]             │
│ size [2]  floor [0.4]  recycle [5]  idle [30m]      │
│ ┌ Imported from gt (read-only) ──┐                   │
│ │ rig kind: rig                  │                   │
│ │ max_polecats: 5                │                   │
│ └────────────────────────────────┘                   │
├ Generated TOML ─────────────────────────────────────┤
│ <live preview>                                       │
└ [Cancel] [Save] · ⚠ restart to apply ───────────────┘
```

### §6.2 Data sources

- **Rigs dropdown** — `GET /api/orchestration/scopes` returns the
  list of gt rigs from `OrchestrationPlane.ListGroups` filtered
  to scope-kind groups, plus polecat aggregation from
  `ListAgents`.
- **Personas dropdown per rig** — same endpoint, projected by rig.
- **Polecat existence indicator** — same data; render warning when
  no polecat exists for the (rig, persona) the operator is editing.
- **gt scheduler config** — `GET /api/orchestration/scheduler-config`
  returns the current `gt config get scheduler.*` values.

### §6.3 Adaptor-CLI mutations

Three buttons, all confirm-modal-gated:

1. **+ New rig** opens a modal: `<input name="rig-name" />` →
   "Run `gt rig create <name>`" button → on confirm,
   `POST /api/orchestration/scopes` with body `{name}` → server
   shells `gt rig create <name>` → modal shows command + stdout.
2. **+ New polecat** opens a modal with rig + persona selectors
   → "Run `gt polecat create <rig> <persona>`" button →
   `POST /api/orchestration/polecats` with body `{rig, persona}`
   → server shells the command.
3. **Edit gt scheduler** opens a modal with numeric input →
   `PUT /api/orchestration/scheduler-config` with body
   `{max_polecats}` → server shells `gt config set
   scheduler.max_polecats <n>`.

Every mutation:
- Requires `X-GEMBA-Confirm` nonce.
- Returns the executed command + stdout/stderr.
- On native, returns `KindUnsupported` (UI hides the buttons
  upstream so this is defense-in-depth only).

### §6.4 Save

Same as §5.3. Pool.toml only stores gemba's daemon parameters; the
adaptor-imported state is not serialized.

## §7. New API surface

| Endpoint | Verb | Owner | Purpose |
|----------|------|-------|---------|
| `/api/pool-config` | GET | gemba | Current pool.toml body + parsed `PoolConfig` + `max_parallel` + `reserved_for_manual` |
| `/api/pool-config` | PUT | gemba | Write pool.toml (nonce-gated). Body = JSON of `PoolConfig`. |
| `/api/personas` | GET | gemba (native) | Enumerate `.gemba/personas/*.toml` |
| `/api/orchestration/state` | GET | adaptor | Adaptor-aware: returns scopes (rigs for gt), personas-per-scope, agent_types. Wraps existing `ListAgents`/`ListGroups`. |
| `/api/orchestration/scopes` | POST | adaptor | gt: shells `gt rig create`. Native: `KindUnsupported`. |
| `/api/orchestration/polecats` | POST | adaptor | gt: shells `gt polecat create`. Native: `KindUnsupported`. |
| `/api/orchestration/scheduler-config` | GET / PUT | adaptor | gt: reads/sets `gt config scheduler.*`. Native: `KindUnsupported`. |

All mutations nonce-gated.

## §8. Validation

Live, on-edit, before save:

| Rule | Severity |
|------|----------|
| `default_persona` references a known persona | warn (allow save with broken default; logs at startup) |
| Routing rule's persona is known | warn |
| Per-pool persona is known | error (block save) |
| Per-pool agent_type is in agents registry | error (block save) |
| `size` integer ≥ 0 | error |
| `floor` float in `[0.0, 1.0]` | error |
| `reserved_for_manual` ≥ 0 | error |
| Same `(scope, persona)` declared twice | error |
| `size > MaxParallel - reserved_for_manual` | warn (show clamp preview) |
| `default_size > 0` AND no `default_persona` | warn ("default_size will create no pools without default_persona") |

Live TOML preview re-renders on every keystroke. Reuses
`BurntSushi/toml.Marshal` shape; deterministic key order.

## §9. Phase 0 invariant preserved

If the operator opens the editor and never saves: pool.toml
unchanged → no daemons → today's behavior.

If they only browse the gt-imported view: nothing in gt changes
either (read-only until they press a button).

If they save an empty config (default_size = 0, no per-pool
entries): equivalent to no pool.toml → still phase 0.

## §10. Migration path

1. **Phase 1 (this bead — `gm-s47n.16`):** ship the editor, the
   scope rename with TOML alias, and the new endpoints. Editor
   defaults to `<scope>` syntax on save; existing `<rig>` files
   keep working with a `WARN`.
2. **Phase 2 (follow-up):** hot-reload daemon on `pool.toml`
   change. Removes the "restart to apply" banner.
3. **Phase 3 (future):** drop the `<rig>` deprecation alias.

## §11. Open questions

These are non-blocking for the build and can be answered in PR
review:

1. **Where does the editor live in routing?** Sub-route `/settings/pools`
   or new tab on `/settings`? Spec assumes a new tab next to
   Organization/Execution but a sub-route is cleaner if pool
   editing has many surfaces. Defer to implementor's judgment with
   a slight preference for the sub-route (`/settings/pools`) so
   the existing Settings page stays read-only.

2. **TOML preview width.** Long pool configs may exceed comfortable
   reading width. Truncate? Allow horizontal scroll? Implementor
   decides.

3. **Adding a persona on native.** Out of scope for this editor —
   personas are edited via direct file edit in
   `.gemba/personas/`. A future bead may add a persona editor.

4. **Polecat lifecycle for gt.** "Create polecat" maps to
   `gt polecat create`. Is "delete polecat" needed? Initial
   shipping scope: no. Polecats are created on-demand by `gt
   sling`'s `--create` flag; explicit creation is for warming a
   cold pool. Deletion is rare enough to defer to CLI.

## §12. Code touchpoints (estimate for `gm-s47n.16`)

| File | Change |
|------|--------|
| `internal/config/pool.go` | Rename `Rig` → `Scope` everywhere; accept both TOML aliases |
| `internal/config/pool_test.go` | Add round-trip tests; deprecation-alias tests |
| `internal/server/pool_config.go` | NEW: GET / PUT `/api/pool-config` |
| `internal/server/pool_config_test.go` | NEW |
| `internal/server/personas.go` | NEW: GET `/api/personas` (native enumeration) |
| `internal/server/orchestration_state.go` | NEW: GET `/api/orchestration/state` |
| `internal/server/orchestration_mutations.go` | NEW: POST scopes/polecats, PUT scheduler-config |
| `internal/adapter/native/orchestration.go` | New methods: enumerate state for editor |
| `internal/adapter/gt/agents.go` | New methods: shell wrappers for `gt rig create`, `gt polecat create`, `gt config set` |
| `internal/server/router.go` | Wire 5 new routes; nonce gates on mutations |
| `web/src/pages/SettingsPage.tsx` | Add a Pools tab or link to `/settings/pools` |
| `web/src/pages/PoolsPage.tsx` | NEW: top-level page; adaptor-detect + branch |
| `web/src/components/pools/NativePoolEditor.tsx` | NEW |
| `web/src/components/pools/GTPoolEditor.tsx` | NEW |
| `web/src/components/pools/PoolCard.tsx` | NEW: per-pool form |
| `web/src/components/pools/RoutingTable.tsx` | NEW |
| `web/src/components/pools/RunCommandModal.tsx` | NEW: gt-CLI confirmation |
| `web/src/components/pools/TOMLPreview.tsx` | NEW |
| `web/src/api/poolConfig.ts` | NEW: typed client |
| `web/src/api/orchestrationState.ts` | NEW: typed client |
| `docs/deployment/pool-sizing.md` | Append "Editing via the SPA" subsection |

Net new files: ~12 web + ~6 Go. Touched: ~6 files. Estimated total
LOC: 1100–1400 across web + Go + tests.

---

**Greenlit by architect 2026-04-30; build proceeds under
`gm-s47n.16`.** Open questions above are deferred to PR review.
