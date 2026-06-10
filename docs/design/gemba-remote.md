---
title: "Gemba Remote — design proposal"
decision: gm-o9t8
---

# Gemba Remote — design proposal

**Status:** draft for review (2026-05-11)
**Author:** mike
**Supersedes / extends:** none yet — this is a net-new epic family layered on top of `gm-root` and existing phases gm-e1..gm-e14
**Outcome:** **laptop-closed operation.** A user closes the lid and Gemba keeps planning, dispatching, observing, and reporting. They drive it from a phone — primarily through chat (Slack/Discord) — and occasionally from a browser at the cloud URL.

---

## 1. Why this exists

The v1 design (see `README.md` and `gm-root`) assumes Gemba runs as a single binary on the same workstation as `gt`, `bd`, the local Dolt server, and the tmux sessions hosting agents. Locked decision #8 is **localhost-by-default**; gm-e14 ships either a `make install` source build or a `docker run` image; "remote access" today means binding `0.0.0.0` with token+TLS on the same operator-owned box.

That model has a hard ceiling:

- The Dolt database is **runtime state pinned to one machine**. Branch switches, restarts, and "another project's Dolt server is running on this port" already break daily flow (we hit this above when probing `bd`).
- Agents (tmux on Gas Town v1, k8s pods on Gas City) require the box to stay awake and on the network. Closing the laptop = halting the rig.
- Mobile is currently out-of-scope (`gm-root` non-goals: "Mobile native apps").
- "Drive an agent" today means SSHing in or opening `localhost:7666` over a Tailscale link.

We want to remove all three constraints **without** discarding the architecture we've spent gm-e2..e14 building. This document specifies the smallest set of changes — runtime topology, data path, agent control plane, and mobile control surface — that get us to laptop-closed operation while preserving the locked decisions (or explicitly amending the ones that have to give).

---

## 2. Goals

1. **Managed data tier.** Beads/Dolt state lives on **Dolt Hub** (or a self-hosted equivalent). Gemba and `bd` connect over the wire; no laptop-pinned Dolt server.
2. **Cloud-hosted Gemba server.** A single canonical Gemba instance reachable at a stable URL (`https://gemba.<org>.<tld>/`). Browser SPA, REST/SSE, OIDC auth.
3. **Remote agent hosts.** Agents run on machines that are *not* the user's laptop — could be the same box as the Gemba server, a separate VM, a Mac mini at home, a k8s pod, or a Vercel Sandbox. Hosts opt-in by running a `gemba-agent` daemon that registers with the server.
4. **Chat-first mobile control.** A Slack/Discord bot is the primary mobile UX: review the board, dispatch convoys, peek at sessions, approve escalations, ack incidents, all from a phone. The web SPA is the secondary mobile UX (read-heavy, capped at what's safe on a small screen).
5. **Laptop-closed.** The laptop is one of N possible clients. The system runs without it. Nothing in the critical path requires anything on `~/`.

## 3. Non-goals (for this design)

- Multi-tenant SaaS. Single-org / single-team install. Anything resembling Linear's tenant model is post-v1.1.
- Federation across multiple Gemba servers. Still `fed:safe / fed:bridge / fed:blocked` labels on items, no cross-server WorkItems.
- Replacing the locked WorkPlane/OrchestrationPlane abstraction. This rides on top of those interfaces.
- Native iOS/Android apps. Mobile = chat bots + responsive SPA. (See gm-root non-goals; we are *not* lifting that one.)
- Per-capability permission scoping (still all-or-nothing per gm-root #8 amendment scope).

---

## 4. Existing design we're building on

A condensed picture of what's already in beads / code, because every part of the remote design hangs off something here.

| Source | What it gives us | What changes for remote |
|---|---|---|
| `gm-root` locked decisions | Sidecar binary, Go single-binary, adaptor-agnostic UI, ZFC, never-write-private-storage, mutation nonce, localhost-default auth | #1 still holds (Gemba is still a single binary, just deployed remote). #8 needs an **amendment**: remote production deploys require OIDC + TLS, no local-only fallback. #9 still holds (we still go through `bd` / `gt` / `gc`, but `bd` itself reaches a remote Dolt). |
| gm-e3 Core contracts | Adaptor interfaces, capability manifests, conformance harness, three transports `api | jsonl | mcp` | Adds a new **OrchestrationTransport** flavor: the agent runs on a *host* the server doesn't own. The adaptor describes hosts, not just sessions. |
| gm-e4 Transport | HTTP API, OpenAPI, SSE hub, mutation nonce | Hub must survive process restarts and reconnects (event replay window). Nonces must be issued by the server, not the laptop. |
| gm-e5 Auth (token, TLS, OIDC stub) | OIDC was stubbed for v1.1 | **OIDC graduates to v1 critical path** for the remote profile. Token auth stays for daemon-to-server (agent host registration). |
| gm-e6 Beads adaptor | `bd --json` shim | Adaptor configures `bd` with a remote Dolt connection string instead of relying on a local `dolt sql-server`. |
| gm-e7 Gas Town adaptor | tmux-based sessions on the local box | Generalize: the adaptor talks to a **host** identified by ID, not to `localhost`. The host's `gemba-agent` proxies `gt` / `tmux attach`. |
| gm-e10 Gas City stub | k8s/subprocess/exec providers | Pluggable workspace kinds already anticipate non-local execution; we use them for real here. |
| gm-e11 Cross-cutting | EscalationRequest, CostMeter, Sprint+TokenBudget, evidence | These primitives are exactly the chat surface — every one becomes a Slack/Discord interaction. |
| gm-e12 SPA | Adaptor-agnostic SPA, capability gates | Add a **mobile-responsive** mode. No new vocabulary; same components rendered for a 390pt screen. |
| gm-e14 Release | `make install` and docker image | Adds two more install paths: a **Helm chart** (or Compose stack) for the server, and a **`gemba agent` subcommand** for hosts. |

The point is: **none of the four planes (Work / Orchestration / Transport / Auth) need to be re-architected.** They need a remote profile.

---

## 5. Target topology

```
                 ┌──────────────────────────┐
                 │   Dolt Hub (managed)     │
                 │   beads schema           │
                 └─────────┬────────────────┘
                           │  mysql wire
                           │
            ┌──────────────┴──────────────┐
            │                             │
            │   Gemba Server (cloud)      │   ← `gemba serve --profile remote`
            │   - HTTP API + SPA          │
            │   - SSE hub                 │
            │   - Beads adaptor (bd→Hub)  │
            │   - Orchestration adaptor   │
            │   - Chat bridge (Slack/DC)  │
            │   - OIDC auth               │
            │                             │
            └──┬───────┬──────────┬───────┘
               │       │          │
       wss/    │  HTTPS│        Slack /
       mTLS    │       │        Discord
               │       │       (webhooks +
        ┌──────▼───┐   │        socket mode)
        │ Agent    │   │           │
        │ Host A   │   │           ▼
        │ (tmux)   │   │     ┌───────────┐
        │ gemba-   │   │     │  Mobile   │
        │ agent    │   │     │  (phone)  │
        └──────────┘   │     └───────────┘
                       │
              ┌────────▼─────────┐
              │ Browser (laptop  │
              │ closed → reopen) │
              └──────────────────┘

        ┌──────────┐   ┌──────────┐
        │ Agent    │   │ Agent    │
        │ Host B   │   │ Host C   │
        │ (k8s)    │   │ (sandbox)│
        └──────────┘   └──────────┘
```

### 5.1 Dolt Hub as the data tier

- Beads writes go through `bd` as today (gm-root #9 preserved); the *only* thing that changes is `bd`'s configured Dolt endpoint. `bd dolt push` becomes a no-op in the remote profile (the server **is** Dolt's client).
- Branching model: one **`main`** branch carries production state. Per-rig or per-experiment branches still work — Dolt Hub already supports them — but Gemba's UI defaults to `main`.
- **Connection security:** Dolt Hub user with scoped DB access, credential lives in the Gemba server's secret store, never on agent hosts. Agent hosts never speak SQL.
- **Backup:** Dolt Hub handles snapshots. Gemba server runs a nightly `dolt clone` to S3 as belt-and-braces, surfaced under "Settings → Backups."
- **Schema migrations:** `bd migrate` runs on the server during boot with a leader-elected lock so multiple server replicas (post-v1.1) don't double-migrate.

### 5.2 Cloud-hosted Gemba server

- Single Go binary, same one we ship today, with a new `--profile remote` flag (or auto-detected from env: `GEMBA_PROFILE=remote`).
- Deploy targets: **container** (gm-e14 image, unchanged) on Fly.io / Render / Vercel-style platform / k3s / a Mac mini. We pick one as the **reference deploy** for docs; the binary doesn't care.
- Stateless aside from the SSE hub's in-memory event ring (10k events, ~5 min replay). All durable state is Dolt Hub plus a small operational store (sessions, nonces, agent-host registrations) — see §6.
- mTLS terminates at the platform's load balancer; Gemba speaks HTTP internally.
- Outbound: Dolt Hub (mysql), Slack/Discord (websocket + REST), agent hosts (wss).

### 5.3 Remote agent hosts

A `gemba-agent` is a new subcommand of the same binary. It is **not** a separate package — it ships from `cmd/gemba/agent.go`. Boot sequence:

1. `gemba agent register --server https://… --token <one-time-enrollment-token>`
   - exchanges enrollment token for a **long-lived host credential** (JWT, rotating monthly)
   - prints a host ID; user copies the ID and labels it (`mac-mini-1`, `prod-runner-east`, …) from the SPA or Slack
2. `gemba agent run` opens a wss connection to the server. Reverse-proxy model: the server initiates requests over the websocket; the agent serves them on its local box (tmux, docker, k8s, etc).

What an agent host exposes to the server:
- `Workspace.kind` it supports (tmux only, or tmux+container, etc — declared in capability manifest, same shape as gm-e10).
- A health stream (CPU, RAM, agent slots free).
- A session-proxy: when the server says "attach this user to session `polecat-7`", the agent multiplexes that user's SSE/wss into a live `tmux pipe-pane` stream.
- A dispatch endpoint: "spawn convoy X with formula Y" → agent invokes `gt sling …` locally.

What the agent host does **not** see:
- Dolt Hub credentials.
- Other tenants' work (single-tenant for v1.1).
- Anyone else's session output.

Same-box and different-box agents are the same code path; "same box as server" is just a deployment choice that saves a network hop.

### 5.4 Browser SPA — unchanged semantics, new origin

- The SPA from gm-e12 ships from the Gemba server, exactly as it does today. The only change: the API base is the public URL, not `http://localhost:7666`.
- The SPA gets a "Host" picker (top-bar select) when more than one agent host is registered. Dispatching a convoy chooses a host (or "any with capacity").
- Mobile-responsive pass on gm-e12 work — same components, narrower layout. **Read-and-approve** UX on mobile web; the bulk grid + dep graph editor remain desktop-only for v1.1.

---

## 6. New domain types & primitives

Add to `internal/model/`:

```go
type HostID string

type AgentHost struct {
    ID           HostID
    Label        string             // user-supplied, e.g. "mac-mini-1"
    Kinds        []WorkspaceKind    // tmux | container | k8s_pod | vm | exec | subprocess
    Capabilities CapabilityManifest // same shape as adaptor manifests
    Status       HostStatus         // online | degraded | offline | enrolling | revoked
    LastSeen     time.Time
    EnrolledBy   UserID
    EnrolledAt   time.Time
}

type Subscription struct {
    ID        SubID
    UserID    UserID
    Channel   ChatChannel      // slack:#gemba-ops, discord:#town, email:user@…
    Filter    EventFilter      // labels, hosts, escalations-only, …
    Throttle  ThrottlePolicy   // collapse, dedupe, quiet hours
}
```

Operational store (separate from Dolt Hub, lives on the Gemba server, can be SQLite-on-volume or Postgres):

- `agent_hosts` (above)
- `user_sessions` (browser/OIDC; chat-link tokens)
- `chat_links` (User ↔ Slack user ID ↔ Discord user ID)
- `subscriptions`
- `mutation_nonces` (server-issued, replacing today's per-process map)
- `audit_log` (append-only mirror of mutations; the truth is still in Dolt)

This is **operational state, not work-tracker state.** It never goes in Dolt Hub. If the server dies and is rebuilt, hosts re-enroll and users re-link — annoying but recoverable, and the Dolt Hub data survives.

---

## 7. Chat surface — Slack and Discord

Same feature set, two adapters. Built on the same `ChatPlane` interface so adding Microsoft Teams later is purely adapter work.

### 7.1 Capabilities

Read commands (anyone in an authorized channel):

- `/gemba ready` — top N ready beads for the rig
- `/gemba show gm-123` — bead detail card (status, owner, labels, deps, recent comments)
- `/gemba sprint` — current sprint burn-up + token budget remaining
- `/gemba hosts` — agent-host roster: label, kind, free slots, last seen
- `/gemba sessions` — live agents: which rig, which bead, runtime, last activity

Action commands (authorized users only — OIDC identity bridged via chat-link):

- `/gemba claim gm-123` — claim a bead
- `/gemba dispatch <formula> [host:<label>]` — start a convoy; defaults to least-loaded host
- `/gemba peek <session>` — get a 60-line tail + a one-click link to a full session SSE in browser
- `/gemba pause <session>` / `/gemba resume <session>`
- `/gemba close gm-123 [--reason …]`
- `/gemba escalate <session> "<reason>"`

Approval interactions (push, not pull): every `EscalationRequest` (gm-e11) and every mutation issued from outside the SPA fans out as a chat card with **Approve / Deny / Mute** buttons. Approval is the nonce — the chat card carries the `X-GEMBA-Confirm` value; clicking Approve POSTs it back through the bot, server validates, mutation lands. This means **mobile approval is a first-class flow, not a workaround.** Locked decision #7 holds without change.

Push notifications:

- Escalations → DM the rig owner.
- Stuck-agent over threshold → channel post.
- Sprint at 80/95/100% of token budget → channel post (three-tier inform/warn/stop from gm-e11).
- Cost spike → DM the owner.
- New comment on a bead the user owns → DM (configurable).

### 7.2 Auth bridge

- Slack/Discord OAuth flow on first interaction: user runs `/gemba link`, gets a short-lived code, types it into the SPA while signed in via OIDC. After that, chat user ID ↔ Gemba user ID is durable in `chat_links`.
- Unlinked users get **read-only** results in public channels and nothing in DMs.
- Channel allow-lists: `gemba.toml` declares which channels can receive what (`#gemba-ops` gets everything, `#general` gets nothing).

### 7.3 Threading model

A bead, a session, and a convoy each map to a **canonical thread**. Posting a `peek` or an escalation against the same session always replies in the same thread, so a phone scroll reads as one conversation per workstream.

### 7.4 Discord parity

Slash commands and components in Discord. Same JSON contracts. We pick one as the **reference adapter** for the first deliverable (Slack, only because the API is more mature for our shape), but neither is preferred in the design.

---

## 8. Auth, in detail

This is where locked decision #8 has to flex. Proposed amendment to `gm-root`:

> **#8 (amended).** Gemba runs in one of two profiles:
> - **`local`** (default for `gemba serve`): unchanged from today. Localhost bind, optional token, optional TLS. Mutations gated by nonce.
> - **`remote`**: non-loopback bind, OIDC required, mTLS required, agent hosts authenticate with rotating JWTs, chat bots authenticate with signing-secret-verified webhooks. `--dangerously-skip-permissions` is **rejected at startup** in this profile. The flag exists for `local` only.

Concretely:

- **Browser → server:** OIDC (Google, GitHub OAuth, Okta — pick one for the reference deploy). Sessions issued as short-lived JWTs + refresh tokens stored httpOnly.
- **Agent host → server:** mTLS over wss, plus a bearer JWT scoped to that host ID. Rotation monthly, automatic.
- **Chat bot → server:** signing secret on inbound webhooks; outbound calls (bot → server) use a service token with no user permissions — every action carries the chat user's identity so the server can authorize per-user.
- **Server → Dolt Hub:** username + service token from secret store. Single credential, never leaves the server process.

---

## 9. Reliability & operations

Things that must be true for laptop-closed:

- **Server uptime:** standard cloud SLOs; not exotic. A single-replica deployment is fine for v1.1; HA is post.
- **Crash recovery:** the SSE hub loses in-flight events on restart; subscribers reconnect and pull a 5-minute replay from the ring buffer (already designed in gm-e4). Anything older comes from Dolt history.
- **Agent reconnect:** the agent's wss reconnects with exponential backoff, capped at 60s. The server treats a host as `degraded` after 30s of silence, `offline` after 5 min, and pages chat at `offline`.
- **Network partitions:** an agent that loses the server keeps its local tmux/k8s sessions alive. When it reconnects, it re-announces what it found running. The server reconciles against its `agent_hosts.observed_sessions` mirror.
- **Cost guardrails:** Sprint/TokenBudget enforcement (gm-e11) gains a hard-stop tier that the server enforces in the dispatch path; agents can't be told to spawn over budget. This is critical when the user can't see the laptop.

---

## 10. Phasing — proposed new epics

These are draft titles for review. None are filed in beads yet (per the user's "once approved we'll review against existing beads"). They depend on the existing phases as noted.

| ID (proposed) | Title | Depends on | Scope sketch |
|---|---|---|---|
| `gm-e15` | **Remote profile foundation** — `--profile remote`, OIDC graduation, mTLS, server-issued nonces | gm-e4, gm-e5 | Profile flag, OIDC adapter (one reference IdP), mTLS termination contract, ops store (sqlite/postgres), audit log |
| `gm-e16` | **Dolt Hub data tier** — managed Dolt, schema migration on boot, S3 belt-and-braces backup | gm-e6 | bd adaptor wired to remote Dolt, migration leader-election, backup job, restore drill documented |
| `gm-e17` | **Agent host protocol** — `gemba agent` subcommand, enrollment, reverse-proxy session bridge | gm-e7, gm-e10 | wss protocol spec, capability manifest extension for hosts, session multiplex, reconnect/replay semantics |
| `gm-e18` | **Chat plane — Slack reference adapter** | gm-e11, gm-e15 | ChatPlane interface, Slack adapter (slash + interactive components), chat-link OAuth, approval-as-nonce flow, subscription model |
| `gm-e19` | **Chat plane — Discord adapter** | gm-e18 | Discord adapter to the same ChatPlane interface; conformance against shared test suite |
| `gm-e20` | **Mobile-responsive SPA pass** | gm-e12 | Breakpoints for ≤768px on read flows + approval flows; bulk-edit + dep-graph remain desktop |
| `gm-e21` | **Remote deploy reference** — container image hardening, Helm chart / Compose stack, runbook | gm-e14, gm-e15, gm-e16, gm-e17 | One reference target chosen, docs site updated, restore/rotate/upgrade runbooks |
| `gm-e22` | **Notification rules & quiet hours** | gm-e18 | Subscription throttling, dedupe, quiet hours, escalation routing rules |

A leaner v1.1-remote (call it "minimum laptop-closed") is `gm-e15 + gm-e16 + gm-e17 + gm-e18 + gm-e21`. Discord (`gm-e19`), mobile web polish (`gm-e20`), and rules engine (`gm-e22`) can fast-follow.

---

## 11. Locked-decision deltas

For each, either no change, an amendment, or a deferred reconsideration.

| # | Decision | Verdict |
|---|---|---|
| 1 | Standalone sidecar binary | **Unchanged.** Still one binary; deployed remote. |
| 2 | Go single binary, embed SPA | **Unchanged.** |
| 3 | React + TS + Vite stack | **Unchanged.** Mobile pass is breakpoints, not a new stack. |
| 4 | Adaptor-agnostic UI | **Unchanged.** Slack/Discord live in `internal/adapter/chat/<vendor>/`, mirroring the WorkPlane/OrchPlane pattern. |
| 5 | Pluggable workspace kinds | **Unchanged.** This is what makes "agent on any box" cheap. |
| 6 | Multi-workspace, not federated | **Unchanged.** Single Gemba server, multiple agent hosts, one logical org. |
| 7 | Mutation nonce, `--dangerously-skip-permissions` | **Unchanged.** Chat approvals **are** nonces. Skip flag rejected in remote profile. |
| 8 | Localhost default, auth gate | **Amended** — see §8. Two profiles, `remote` mandates OIDC + mTLS + signed chat webhooks. |
| 9 | Never write any backend's private storage | **Unchanged.** We still go through `bd` / `gt` / `gc`. `bd` reaches Dolt Hub instead of a local socket — that's `bd`'s concern, not ours. |
| 10 | Declarative UX (desired vs observed) | **Unchanged.** Agent hosts contribute `observed_state` from wherever they live. |
| 11 | ZFC for the UI | **Unchanged.** Chat commands surface options; humans decide. No bot policy. |
| 12 | Distribution | **Amended at gm-e14 follow-on.** Add a hardened container image + Helm chart / Compose stack as a third install path. Source build and current docker image untouched. |

Out-of-scope items in `gm-root` that we are **explicitly not lifting**:
- Cross-workspace federation — still out.
- Mobile native apps — still out (chat + responsive web only).
- Multi-transport adaptors — still one transport per adaptor; the ChatPlane is its own adaptor pair, not a transport.

---

## 12. Open questions for review

1. **Reference deploy target.** Fly.io vs. a single Hetzner VM with k3s vs. Vercel-style platform vs. Mac mini at home. Each has a different ops story. Pick one for `gm-e21`.
2. **OIDC IdP for the reference.** Google Workspace is easiest for a solo-op deploy; GitHub OAuth fits the dev audience; Okta if we want to make enterprise-friendly noise. Recommendation: **GitHub OAuth** for v1.1, swap to a real IdP at v1.2.
3. **Dolt Hub tenancy.** One database per Gemba install vs. one database per rig. Per-install is simpler and matches `gm-root` #6 (multi-workspace, not federated); per-rig would mirror today's `~/gt/<rig>/.beads` layout. **Recommendation:** one database, schema-namespaced per rig — simpler ops, easier cross-rig views.
4. **Self-hosted Dolt option.** Do we support `docker run dolthub/dolt-sql-server` as a substitute for Dolt Hub? For air-gapped users, probably yes. Adds one config knob and a "self-hosted-dolt" runbook.
5. **Chat-first defaults.** Should `/gemba dispatch` require explicit host selection, or default to "any with capacity"? Defaulting is mobile-friendly but hides resource choices. Recommendation: **default to capacity-aware, surface the chosen host in the response card.**
6. **Session peek vs full attach on mobile.** A 60-line tail is fine on a phone; a full `tmux attach` over wss is not. Are there flows where partial peek is *insufficient* and we'd need a different mobile-attach UX? Likely no for v1.1; revisit if users push back.
7. **Quiet hours.** Per-user (each user sets their own) or per-org (one schedule for everyone). Per-user is cleaner but per-org is what most orgs actually want for "don't page anyone after midnight." Recommendation: **per-user with org override.**
8. **The agent enrollment token UX.** Today's mental model: user generates a token in the SPA, pastes it into `gemba agent register` on the new box. Reasonable. Alternative: QR-code flow scanned from phone. Cute, probably unnecessary, easy to add later.
9. **Migration path from v1 local install.** A user running `gemba serve` on their laptop today should be able to `gemba migrate local-to-remote --server https://…` and have their `~/gt` Dolt push to Dolt Hub, then their laptop becomes an agent host. Worth scoping under `gm-e16` or making its own ticket.

---

## 13. What this isn't trying to solve

To prevent scope creep when this turns into beads:

- It is **not** redesigning Gas Town or Gas City. It uses them through the existing orchestration adaptor.
- It is **not** redesigning Beads. It changes `bd`'s storage endpoint, not its semantics.
- It is **not** building a SaaS product. One org, one server, one Dolt Hub DB.
- It is **not** adding native mobile apps. Chat + responsive web is the deal.
- It is **not** changing the conformance harness from gm-e3. Adaptors still pass groups A–F; this just adds a few capabilities to declare (`hosts.remote`, `chat.*`).

---

## 14. Next step

Review this document and flag amendments. Once approved:

1. Reconcile §10's proposed epics against the existing `~/gt/gemba/.beads/issues.jsonl` — some may already exist as children of `gm-e5` / `gm-e11` / `gm-e14` and need to be promoted to epics or reparented.
2. File the new epics (`gm-e15..gm-e22`) with the deps in §10.
3. File the locked-decision amendment to `gm-root` (§11) as an explicit `notes:` block, dated, with this document linked.
4. Pick the reference deploy + IdP (§12 q1, q2) so `gm-e15` and `gm-e21` aren't underspecified at kickoff.
