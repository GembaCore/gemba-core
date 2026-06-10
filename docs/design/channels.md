---
title: "Pluggable channel-bridge architecture — Telegram, email, Slack as first-class actor surfaces"
decision: gm-thq1
---

# D14 — Channels as a first-class actor surface

> **Status:** Draft. Implementation deferred. This document captures the
> ratified shape that any future channel integration — Telegram, email,
> Slack, SMS, Matrix — must plug into. It is the input artifact for the
> implementation epic that lands later.
>
> **Decision bead:** [gm-thq1](../../) (D14)
> **Companion bead:** the implementation epic, to be filed when the operator schedules the work.

---

## 1. Problem

Today Gemba reaches its operator through one channel: the SPA tab. Every
external integration so far (the bd-hook bridge, Prometheus scrape) has
been one-off plumbing for a single consumer. There is no actor model
for "a human reachable via Telegram" or "an on-call rotation peer
reachable via SMS," and no shared primitive for the things every
channel will need:

- Identity mapping (external user id ↔ `AgentRef`).
- Per-actor subscriptions to the events hub.
- A durable inbox so messages survive bridge restarts.
- A grammar for incoming user actions ("claim gm-x1", "defer 1d", `[Approve]` button).
- Rate limiting + digest mode + redaction policy.
- Audit trail for channel-driven mutations.

Every new channel rebuilding these primitives from scratch is the failure
mode this decision prevents.

The architectural gap is concrete:

- **Inbound mutations** today have exactly one surface — `POST /api/workitems/notify`, used by `gemba-bd-hook`.
- **Outbound delivery** today has exactly one consumer — the SPA's SSE subscription to `/api/events`.
- **Identity** has the right shape (`AgentRef.Kind = agent | human`) but no story for non-SPA humans. The `assignee` field expects `Kind=agent`; humans land on `owner`. Channel-driven claims need a `Kind=human` actor that the assignment API accepts as a claimant.

## 2. Constraints carried forward

This decision must hold under:

- **DD-16** (gm-ege, external-consumer optionality): the API surface a channel adaptor uses must be the same surface any third-party consumer would use. No channel-specific endpoints.
- **DD-13** (synthesized vs operator distinction): a channel-driven action must be attributable to the human, not the bridge. Audit clarity > display brevity.
- **D6** (decision-capture convention, gm-d1m1): this decision and any superseding decisions follow the `D#` convention.
- **The propulsion principle**: agents pulling from their hook is autonomous. Channel-driven human claims must coexist with autonomous agent claims without deadlock.
- **Capability gating** (DD-15, hide-not-disable): a channel adaptor must respect the same capability flags the SPA does — e.g., on adaptors where `evidence_synthesis_required: false`, the bridge does not surface the missing-evidence cue.

## 3. Existing system map (foundation)

### 3.1 Events hub

- Defined in `internal/events/schema.go`. Envelope: `GembaEvent{ID, Kind, At, Source, Scope, Payload, TraceID}`.
- 14 canonical kinds in 5 categories: `workitem.*` (4), `session.*` (3), `escalation.*` (2), `reservation.*` / `workspace.*` (4), `budget.*` (3). Plus `capability.refresh`, `skill.output_emitted`, `phase.transitioned`.
- Hub is **in-memory and ephemeral**. Subscribers receive events live via `/api/events` SSE; no Last-Event-ID replay. Reconnecting subscribers lose history.
- Filter: kinds (OR), planes (workplane | orchestration), assignment_id, session_id, workitem_id, epic_id (all AND).
- Producers: `internal/adapter/bd` (workitem.* via `pollAndEmit`), `internal/orchestration` (session.*, escalation.*, reservation.*), budget thresholds (`internal/server/milestone_autoclose.go`).
- Existing consumers: SPA SSE (`web/src/data/sse.ts`), Prometheus collector (`internal/server/metrics/collector.go`), walk bridge (`internal/walk/sources/orchestration.go`).

### 3.2 Inbound surface

- One webhook: `POST /api/workitems/notify` in `internal/server/work_items_notify.go`. Accepts `{id, updated_at}`; bridge re-reads the bead via the bd adaptor and re-emits `workitem.updated` through the hub. Used by `gemba-bd-hook` for terminal-driven `bd update` calls.
- This is the only inbound mutation surface. Every other write goes through the typed REST API.

### 3.3 Dispatch and lifecycle

- States: `open`, `in_progress`, `hooked`, `pinned`, `blocked`, `deferred`, `closed`. Transitions are idempotent; mutating verbs use `ConfirmNonce`.
- Claim is atomic: `POST /api/assignments` → `ClaimNextReady(filter, claimant AgentRef)` against `internal/planner/claims` (RWMutex-guarded `(BeadID → SessionID, ClaimedAt)` map). Stale claims reaped on a timer.
- "Hooked" is a status with no special orchestration semantics beyond a label; the **post-write hook** (`gemba-bd-hook`) is the actual cross-process bridge.

### 3.4 Escalations

- `EscalationRequest{ID, Kind, Urgency: blocking|advisory, State: open|resolved|canceled}`.
- 10+ kinds: `permission_prompt`, `mcp_elicitation`, `a2a_input_required`, `hitl_approval`, `orchestrator_pause`, `blocker`, `question`, `witness_finding`, `refinery_rejection`, `beads_degraded`.
- Resolved via `POST /api/escalations/{id}/respond {kind, value}` where `kind` is `approve|deny|modify|defer`.
- `urgency: blocking` suspends the active session; `advisory` does not.

### 3.5 Identity

- `AgentRef{ID: workspace-qualified, Name, Kind: agent|human, Role?, Dialect?}`.
- `WorkItem.assignee` expects `Kind=agent`; `WorkItem.owner` expects `Kind=human`. The claim API today accepts both kinds as the claimant, but channel-driven claims write to `assignee` and need that field to accept `Kind=human` cleanly. (See §10 risk.)

## 4. Architecture

### 4.1 Three-component split

```
                    Gemba API (existing)
                    ────────────────────
                    /api/events         (SSE, exists)
                    /api/workitems      (CRUD, exists)
                    /api/assignments    (claim, exists)
                    /api/escalations    (respond, exists)
                    /api/workitems/notify (out-of-process push, exists)
                          ▲ │
                          │ ▼
        ┌────────────────────────────────────────┐
        │       Channel Bridge (NEW)              │
        │   internal/channels/                    │
        │                                         │
        │  ┌─────────────┐    ┌─────────────┐    │
        │  │ Outbound    │    │ Inbound     │    │
        │  │ Router      │    │ Dispatcher  │    │
        │  │ event→msg   │    │ msg→action  │    │
        │  └──────┬──────┘    └──────┬──────┘    │
        │         │                  │            │
        │         │   ChannelAdaptor │            │
        │         │   interface      │            │
        │         ▼                  ▲            │
        └─────────┼──────────────────┼────────────┘
                  ▼                  │
        ┌──────────────────────────────────────┐
        │     Pluggable channel adaptors        │
        │  ┌──────────┐ ┌──────┐ ┌─────────┐  │
        │  │ telegram │ │ smtp │ │  slack  │  │
        │  └──────────┘ └──────┘ └─────────┘  │
        └──────────────────────────────────────┘
                  │                  ▲
                  ▼                  │
              External users (humans on phones, in inboxes)
```

### 4.2 The `ChannelAdaptor` interface

```go
// ChannelAdaptor is the contract every channel implementation satisfies.
// The bridge owns routing, identity, and persistence; adaptors own
// transport (long-poll, IMAP IDLE, webhook receivers) and per-channel
// rendering.
type ChannelAdaptor interface {
    // ChannelID is the stable identifier ("telegram", "smtp", "slack").
    // Used as the second segment of the workspace-qualified actor ID
    // and as the key in subscription routing.
    ChannelID() string

    // ResolveActor maps an external identity to an AgentRef. Returns
    // ErrUnknownActor if the external identity has not been enrolled —
    // the bridge handles bootstrap from there.
    ResolveActor(ctx context.Context, ext ExternalIdentity) (AgentRef, error)

    // RegisterActor creates the (channel, ext_id, agent_ref_id, role)
    // mapping. Called by the bridge after enrollment policy authorizes
    // the user. Persisted in the channel_actors Dolt table.
    RegisterActor(ctx context.Context, ext ExternalIdentity, name string) (AgentRef, error)

    // Send delivers a structured ChannelMessage to a recipient. Adaptors
    // project the structured form down to their wire format (Telegram
    // inline keyboard, Slack Block Kit, email HTML+text). Returns the
    // adaptor-specific message ID for later editing/deletion.
    Send(ctx context.Context, recipient AgentRef, msg ChannelMessage) (MessageRef, error)

    // Run owns the inbound transport for the lifetime of the bridge.
    // The adaptor calls `handle(action)` for each parsed inbound user
    // action. The bridge guarantees handle is goroutine-safe.
    Run(ctx context.Context, handle InboundHandler) error

    // Capabilities advertises feature support so the bridge degrades
    // gracefully — e.g., if SupportsButtons=false, the outbound router
    // renders the same actions as text instructions instead.
    Capabilities() ChannelCaps
}

type ChannelCaps struct {
    SupportsThreads        bool
    SupportsButtons        bool
    SupportsRichFormatting bool   // bold / italic / inline code
    MaxMessageLen          int    // hard limit for chunking
    SupportsEditMessage    bool   // for ack-on-handled patterns
    SupportsReadReceipt    bool
}

type ChannelMessage struct {
    Kind     MessageKind   // info | action_required | confirmation | digest
    Title    string        // single-line summary
    Body     string        // markdown (adaptors downconvert)
    Fields   []KVField     // labeled facts (id, owner, severity, ...)
    Actions  []Action      // [Claim] [Approve] [Defer 1h] etc.
    Links    []Link        // deep links into the SPA
    Meta     MessageMeta   // event_id, dedup_key, expires_at
}

type Action struct {
    ID    string  // stable across renderings
    Verb  string  // "claim" | "respond" | "defer" | ...
    Label string  // "Approve"
    Args  map[string]any
    // Filled in by adaptor: Telegram callback_data, Slack action_id, etc.
}
```

### 4.3 `InboundAction`

```go
// InboundAction is what the bridge receives from any adaptor. Adaptors
// own the parsing (text grammar, button decode, mailto link parse);
// the bridge owns the dispatch.
type InboundAction struct {
    Actor    AgentRef       // resolved via adaptor.ResolveActor
    Verb     ActionVerb     // claim | update | comment | defer | ...
    Target   string         // bead id or escalation id
    Payload  map[string]any // verb-specific args
    ReplyTo  MessageRef     // adaptor's source message; bridge can ack/edit
    Channel  string         // ChannelAdaptor.ChannelID()
    SourceID string         // unique source message id for idempotency
}

type ActionVerb string
const (
    VerbClaim    ActionVerb = "claim"
    VerbStatus   ActionVerb = "status"
    VerbList     ActionVerb = "list"
    VerbRespond  ActionVerb = "respond"   // escalation respond
    VerbDefer    ActionVerb = "defer"
    VerbComment  ActionVerb = "comment"
    VerbTriage   ActionVerb = "triage"    // priority/scope adjust
    VerbEscalate ActionVerb = "escalate"
    VerbHelp     ActionVerb = "help"
    VerbInbox    ActionVerb = "inbox"     // replay durable inbox
    VerbMute     ActionVerb = "mute"      // pause channel for window
)
```

## 5. Identity

### 5.1 Mapping rule

A channel user becomes a workspace-qualified `AgentRef` of `Kind=human`:

```
gemba/channels/<channel-id>/<ext-id>
```

Examples:

- `gemba/channels/telegram/u123456789`
- `gemba/channels/slack/U0G9QF9C6`
- `gemba/channels/smtp/mike@example.com`

Persisted in a new Dolt-backed table:

```
channel_actors
  channel        TEXT     -- "telegram"
  ext_id         TEXT     -- channel-native external id
  agent_ref_id   TEXT     -- "gemba/channels/telegram/u123456789"
  display_name   TEXT     -- from the channel ("@mike")
  role           TEXT     -- "viewer" | "claimant" | "operator"
  enrolled_at    TIMESTAMP
  enrolled_by    TEXT     -- agent_ref_id of the enroller, or "magic-link"
  last_seen_at   TIMESTAMP
  status         TEXT     -- "active" | "suspended" | "revoked"
  PRIMARY KEY (channel, ext_id)
```

Dolt-tracking gives multi-machine durability for free (the same way beads survive machine loss via the per-rig Dolt remote).

### 5.2 Enrollment policy

Configurable per-channel. Three modes:

| Mode | Description |
|---|---|
| **closed** | Only IDs already in `channel_actors` are accepted. New IDs get a polite "not enrolled" reply and the bridge takes no action. |
| **magic-link** | Unknown ID receives "DM the bot from a session that ran `gt channel pair telegram <code>`." Pairing call hits a new `/api/channels/enrollments` endpoint, which writes the row. |
| **open + role-gate** | Anyone can register but starts with `role: viewer`. Mutating verbs require `role: claimant`, granted explicitly by an existing operator. |

Default for v1: **magic-link**. It's the only mode that doesn't require a separate trust establishment.

### 5.3 Comment authorship

When the bridge writes a comment via `bd update --append-notes` from a Telegram message by mike, the author is `gemba/channels/telegram/u123456789`, not `gemba/crew/mike`. The SPA can collapse them in display ("mike (via Telegram)") but the audit ledger keeps them distinct. This is the DD-13 spirit applied to actor identity.

## 6. Outbound: events → channel messages

### 6.1 Subscription model

```
channel_subscriptions
  id             TEXT PRIMARY KEY    -- ulid
  actor          TEXT                -- agent_ref_id
  channel        TEXT                -- "telegram"
  filter         JSON                -- EventFilter (topics, planes, scope ids)
  transport      JSON                -- adaptor-specific delivery args (tg_chat_id, slack_channel, smtp_to)
  enabled        BOOL
  digest_mode    TEXT                -- "live" | "hourly" | "daily"
  redaction      TEXT                -- "full" | "summary-only"
  rate_limit     INT                 -- max msgs/hour (0 = no limit; bridge fallback is 60)
  created_at     TIMESTAMP
```

The `filter` reuses the existing `internal/events.Filter` struct (kinds, planes, assignment_id, session_id, workitem_id, epic_id) plus two channel-specific fields:

- `severity_floor` — for escalation events, only deliver `urgency >= floor`.
- `mute_until` — operator-set silence window.

### 6.2 Routing pipeline

```
event arrives at hub
   │
   ▼
outbound router consumes via hub.Subscribe()
   │
   ▼
match against channel_subscriptions where filter matches event
   │
   ▼  (per matched subscription)
apply digest_mode (live → emit now; hourly/daily → enqueue + flush on tick)
   │
   ▼
apply redaction policy
   │
   ▼
template into ChannelMessage (per-event-kind renderer)
   │
   ▼
adaptor.Send(actor, message) → write copy to channel_inbox
   │
   ▼
emit channel.delivered event (for observability)
```

### 6.3 Default per-event renderings

| Event kind | Default outbound shape |
|---|---|
| `escalation.opened` (urgency=blocking) | "🛑 *Blocking escalation* on `gm-foo`: <summary>. \[Claim\] \[Approve\] \[Deny\] \[Defer 1h\]" |
| `escalation.opened` (urgency=advisory) | (suppressed by default; opt-in) |
| `escalation.resolved` | "✅ `e-42` on `gm-foo` resolved by @obsidian: approve" |
| `workitem.closed` | "✅ `gm-foo` closed by @obsidian. \[View\]" |
| `workitem.evidence_attached` | (suppressed by default — too chatty; opt-in per-user) |
| `budget.warn` | "⚠ Session `s-abc` at 80% budget on `gm-foo`. \[Extend\] \[Stop\]" |
| `budget.stop` | "🛑 Session `s-abc` hit budget stop on `gm-foo`. \[Extend\] \[End\]" |
| `session.transition` (to `failed`) | "❌ Session `s-abc` failed on `gm-foo`: <reason>. \[Retry\] \[Escalate\]" |
| `phase.transitioned` (to `m1-cut`) | "🚀 Phase transitioned to M1 cut by @mike. \[Project view\]" |

Every renderer reads the manifest (capability flags) so outputs respect the same hide-not-disable rule as the SPA.

## 7. Inbound: channel messages → bead actions

### 7.1 Grammar (default, channel-extensible)

```
/claim <bead-id>                  → POST /api/assignments {claimant: actor, target}
/status <bead-id>                 → GET  /api/workitems/{id}     → render
/list [filter...]                 → GET  /api/workitems?ready=true&assignee=actor
/respond <esc-id> <verb> [args]   → POST /api/escalations/{id}/respond
/defer <bead-id> <duration>       → bd update --defer
<reply text> on a bead message    → bd update --append-notes
/triage <bead-id> P[0-4]          → PATCH /api/workitems/{id} {priority}
/escalate <bead-id> [severity]    → POST /api/escalations
/help                             → static help message
/inbox                            → render last N undelivered/unread inbox entries
/mute <duration>                  → set mute_until on subscription
```

Buttons (Telegram inline keyboard / Slack Block Kit) are pre-templated invocations of these verbs — no separate code path.

### 7.2 Authorization

Every inbound verb checks the actor's `role`:

| Verb | Required role |
|---|---|
| status, list, help, inbox | viewer |
| comment, defer | claimant |
| claim, triage, respond | claimant |
| escalate | claimant |
| mute | viewer (mutes only own subscriptions) |

A `viewer` invoking a higher-role verb gets a polite refusal message and the action is logged as `channel.action_invoked {result: "denied"}`.

### 7.3 Idempotency

`InboundAction.SourceID` is the dedup key. The bridge keeps a 24-hour LRU of recently-handled `(channel, source_id)` pairs to handle adaptor retries (Telegram occasionally re-delivers webhook events after timeouts, SMTP receivers can deliver duplicates).

## 8. Persistence — the inbox table

Events are ephemeral. Channels need durability so:

- A bridge restart doesn't drop in-flight messages.
- A user can `/inbox` to see what was delivered while they were away.
- Delivery failures (Telegram API down) get retried.

```
channel_inbox
  id              TEXT PRIMARY KEY     -- ulid
  actor           TEXT                 -- recipient agent_ref_id
  channel         TEXT
  subscription_id TEXT
  event_id        TEXT                 -- source event for traceability
  message         JSON                 -- ChannelMessage envelope
  state           TEXT                 -- "pending" | "sent" | "failed" | "expired" | "acked"
  attempts        INT
  created_at      TIMESTAMP
  sent_at         TIMESTAMP
  acked_at        TIMESTAMP            -- when user took action or read
  expires_at      TIMESTAMP            -- TTL; default 7 days
```

Lifecycle:

- New event matches a subscription → row inserted with `state: pending`.
- Adaptor.Send succeeds → `state: sent`.
- Adaptor.Send fails → `attempts++`, retry with exponential backoff up to 5 attempts, then `state: failed`.
- User invokes a verb whose target matches an inbox entry → `state: acked`.
- TTL expires → `state: expired` (still queryable via `/inbox` history).

This is option 2 from the design discussion. Option 1 (no persistence) is unacceptable for blocking escalations; option 3 (full event log) is a heavier change deferred to its own decision when a second consumer needs it.

## 9. New event kinds

Three additions, fully additive (no schema break, callers ignore unknown kinds):

| Kind | Emitted when | Payload |
|---|---|---|
| `channel.delivered` | adaptor.Send returns success | `{actor, channel, subscription_id, event_id, message_ref}` |
| `channel.action_invoked` | inbound verb dispatched (success or denial) | `{actor, channel, verb, target, result, source_id}` |
| `channel.delivery_failed` | adaptor.Send exhausted retries | `{actor, channel, subscription_id, event_id, error, attempts}` |

These let the SPA show a "channels" panel in Settings, let metrics observe channel health, and let other channels react to each other (e.g., email digest summarizing the week's `channel.action_invoked` for an exec subscription).

## 10. Risks and open questions

### 10.1 Authorization is the security boundary

Compromised Telegram session → bead writes. Mitigations:

- Magic-link enrollment as default (proves the channel user controls a workspace seat).
- Role gate on mutating verbs.
- Audit log: every `channel.action_invoked` event is durable in beads (events normally aren't, but this kind specifically is — the bridge files a tiny `event`-typed bead per action).
- Per-channel revoke: setting `channel_actors.status='revoked'` immediately rejects future actions and forces re-enrollment.

### 10.2 Rate limiting / flood control

A noisy adaptor can bury a user. Bridge enforces:

- Per-subscription `rate_limit` (default 60 msgs/hour).
- Burst control: if a single event kind floods (e.g., 50 `workitem.evidence_attached` in 10s), the bridge collapses into a digest message.
- `mute_until` honored for both `/mute` user requests and operator-set quiet hours.

### 10.3 Reply attribution

When a Telegram comment lands via `bd update --append-notes`, the author is `gemba/channels/telegram/u123` not `gemba/crew/mike`. Decision: **keep distinct**. SPA can collapse for display; ledger keeps them apart. This honors DD-13's spirit (operator vs derived/synthesized authorship distinction).

### 10.4 Event ordering across reconnects

SSE is per-connection ordered, but a bridge restart can deliver events out of order across the gap. Mitigation: `event_id` is sequence-able (ulid-style). The bridge tracks per-actor `last_delivered_event_id` and only delivers events with strictly greater ids on reconnect. Older events drop into the inbox as `state: pending` and only `/inbox` surfaces them.

### 10.5 Dispatch boundary with autonomous agents

The propulsion principle says agents take work off their hook autonomously. A human claiming via Telegram could race an agent's hook claim. Decision: **the existing claim index already enforces single-assignee** via SoftConflict; whichever request hits `ClaimNextReady` first wins. Humans rarely race; in the unusual case where they do, the second claimant gets a clean rejection and a "claimed by X" message. We do *not* preempt an agent's existing claim — that violates the propulsion contract.

### 10.6 Privacy / redaction

Channel-routed messages may contain bead descriptions with credentials, customer data, etc. Subscription `redaction` policy:

- `full`: title + summary + description (default for operator-role internal channels).
- `summary-only`: title + first-line summary, no description (default for external SMTP digests).
- Per-bead override: a bead can carry `labels: ["confidential"]` to force `summary-only` regardless of subscription policy.

### 10.7 The `assignee` field's `Kind=human` issue

Today `WorkItem.assignee` is documented as expecting `Kind=agent`; `owner` is for humans. Channel-driven claims write to `assignee` and need that field to accept `Kind=human` cleanly without confusing the orchestration plane. Either:

- (a) Update the documented contract to say `assignee` is "the actor doing the work, regardless of kind" — minimal change, most APIs already accept it.
- (b) Add `human_claimant` field to `WorkItem` distinct from `assignee` — bigger schema change.

Implementation epic decides; **default recommendation is (a)** because it's smaller and the orchestration plane only checks `assignee.id` for claim-index conflicts, not `assignee.kind`.

### 10.8 Co-located vs sidecar bridge

Two viable deployment shapes:

- **Co-located in `cmd/gemba-server`:** simpler ops, shares the events hub by direct subscription. Risk: a misbehaving adaptor (slow Telegram API call) blocks server resources.
- **Sidecar `cmd/gemba-channel-bridge`:** isolated failure domain, but needs its own SSE subscription back to the server. Slight latency / reconnect cost.

Implementation epic decides. **Default recommendation: sidecar** — channels are a "nice-to-have" feature whose failure mode (bot down) should never affect the SPA's responsiveness.

## 11. Module layout (target shape)

```
internal/channels/                          (new)
  bridge.go               # outbound router + inbound dispatcher; lifecycle
  subscription.go         # filter wrapper, subscription CRUD, persistence
  inbox.go                # per-actor durable queue (Dolt-backed)
  parser.go               # default verb grammar (channels can extend)
  identity.go             # ResolveActor / RegisterActor / role gate
  enrollment.go           # magic-link, closed, open enrollment policies
  redaction.go            # description scrubbing per policy
  ratelimit.go            # per-subscription rate limit + burst collapse
  adaptor.go              # ChannelAdaptor interface
  testing.go              # in-memory adaptor + fixtures for tests
  adaptors/
    telegram/
      bot.go              # long-poll loop; webhook alt
      render.go           # ChannelMessage → tg payload
      keyboard.go         # inline keyboard from Action[]
      parse.go            # text + callback_data → InboundAction
    smtp/
      imap.go             # IMAP IDLE inbound
      mime.go             # outbound MIME composition
      mailto.go           # action → mailto: link generation
    slack/
      events_api.go       # webhook receiver + socket mode alt
      blockkit.go         # ChannelMessage → Block Kit
      identity.go         # team_id + user_id mapping
cmd/gemba-channel-bridge/                   (new, sidecar option)
  main.go                 # SSE consumer + ChannelAdaptor host

internal/server/                            (existing)
  channels_api.go         # NEW: /api/channels/{actors,subscriptions,enrollments}

web/src/pages/SettingsPage.tsx              (existing, extended)
  + Channels tab — manage subscriptions, view delivery health, revoke actors
web/src/api/channels.ts                     (new)
  + typed REST client for channels API

docs/design/channels.md                     ← this document
```

## 12. Implementation epic — proposed breakdown

> The implementation epic is **not yet filed**. This section is the
> input artifact for that epic when the operator schedules it.

**Suggested wave structure:**

- **Wave 1 — bridge skeleton + first adaptor (~1 week)**
  - Bead A: `internal/channels/` skeleton (interface, bridge, identity, parser, default grammar). Tests against `testing.go` in-memory adaptor.
  - Bead B: Telegram adaptor with magic-link enrollment.
  - Bead C: Channels REST API (`/api/channels/...`) for actor + subscription management.
  - Bead D: Channel actors / subscriptions / inbox Dolt tables + migrations.

- **Wave 2 — operator-facing management (~3 days)**
  - Bead E: SettingsPage Channels tab. Manage subscriptions, view delivery health, view enrolled actors, revoke.
  - Bead F: 3 new event kinds (`channel.delivered`, `channel.action_invoked`, `channel.delivery_failed`) wired through metrics + SPA.

- **Wave 3 — additional adaptors (~2 days each)**
  - Bead G: SMTP adaptor (IMAP IDLE in, MIME out, mailto: action links).
  - Bead H: Slack adaptor (Events API + Socket Mode, Block Kit rendering).
  - Bead I (later): SMS adaptor (Twilio).
  - Bead J (later): Matrix adaptor.

- **Wave 4 — hardening (~3 days)**
  - Bead K: Rate limiting + burst collapse + digest mode.
  - Bead L: Redaction policy + per-bead `confidential` label honored.
  - Bead M: Per-actor `/inbox` replay command + TTL'd inbox compaction.

Each adaptor bead has the same shape: implement `ChannelAdaptor`, register
with the bridge, document the channel's enrollment quirks, ship tests. The
bridge is built once.

## 13. Acceptance criteria for this decision

This decision (D14) is **ratified** when:

- `docs/design/channels.md` (this file) exists and links back to bead `gm-thq1`.
- Implementation epic filed and scheduled.

This decision is **rejected** when:

- The operator decides channels should not be a first-class actor surface (e.g., "stick with one-off scripts per integration").
- A superseding decision (D# > 14) is filed with a `supersedes:gm-thq1` dependency.

Until either resolution, status is **draft**.

## 14. References

- D6 (`gm-d1m1`) — Decision-capture convention
- DD-16 (`gm-ege`) — External-consumer optionality
- DD-13 — Synthesized vs operator distinction (evidence; same spirit applied here to actor identity)
- `internal/events/schema.go` — Event envelope + 14 canonical kinds
- `internal/server/work_items_notify.go` — Existing inbound webhook (the only one)
- `internal/orchestration` — Claim mechanism, escalation respond
- `internal/planner/claims` — Claim index (single-assignee guarantee)
- `web/src/data/sse.ts` — Existing SSE consumer the bridge mirrors
