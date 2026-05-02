# Jira Cloud Board as a Gemba WorkPlane

Status: product/design proposal

Date: 2026-05-01

## Summary

Gemba can use Jira Cloud as a WorkPlane by treating Jira issues,
workflow transitions, Agile board state, issue links, versions, and
webhooks as the durable planning substrate. This would replace the
current Beads-backed Kanban data surface with a Jira-backed adaptor
while preserving Gemba's orchestration plane, session state, escalations,
agent dispatch, evidence synthesis, and right-hand detail surfaces.

The best fit is a Jira Software project with a simple three-column
Kanban board:

- Ready
- In Progress
- Done

Gemba-specific planning state that does not deserve its own Jira column
should be carried by custom fields, issue properties, labels, and card
badges. This matches Gemba's current simplified board, where Ready
combines backlog/unstarted/staged states and exposes "Next up",
"Staged", "Triage", "Needs input", "Review", and "Ready" as card-level
signals.

The largest compromise is milestone modeling. Jira project versions
map cleanly to releases, but versions are not workflow items on the
board and cannot be dragged from Ready to In Progress. If Gemba needs
the "drag M1 into progress and cascade child work" gesture to happen
inside Jira, milestones must be represented as Jira issues, not only
versions. For teams on Jira Premium or Enterprise, a hierarchy level
above Epic is the cleanest analog. For standard Jira Software, Gemba
should use either a custom issue type linked to epics or a project
version plus a Gemba-owned command/surface for starting the milestone.

Overall assessment: good fit for work-item CRUD, status transitions,
board visibility, epic/bead hierarchy, and enterprise adoption; moderate
fit for dependency-aware dispatch and wrapper rollups; weak fit for
milestone-as-draggable-wrapper, live agent telemetry, and Gemba-specific
evidence unless Gemba remains the orchestration UI around Jira.

## Goals

1. Replace Gemba's Beads-backed Kanban board with a Jira-backed board
   that operators can also use directly in Jira Cloud.
2. Preserve Gemba semantics for beads, epics, milestones, dependencies,
   status propagation, escalation signals, and evidence.
3. Use only supported Jira Cloud APIs and administration surfaces.
4. Keep Jira as the source of truth for issue state and hierarchy when
   Jira mode is enabled.
5. Allow Gemba's dispatch engine to react to Jira transitions and author
   new Jira work items from agent or persona activity.

## Non-Goals

1. Rebuild Jira's full board configuration UI inside Gemba.
2. Depend on private or undocumented Jira Cloud APIs.
3. Require every Gemba surface to become a Jira surface. Sessions,
   token spend, source-analysis freshness, Gemba Walks, and live
   escalations remain Gemba-native unless explicitly mirrored.
4. Guarantee automatic setup of arbitrary customer workflows. Jira board
   column/status mapping remains an admin-controlled configuration area.

## Relevant Jira Cloud API Surface

Jira Cloud exposes two API families that matter here:

- Jira platform REST API v3 for issues, transitions, fields, issue
  links, comments, project versions, and changelogs.
- Jira Software Agile REST API for boards, board issues, epics, sprints,
  versions, and board configuration reads.

Important operations:

- Create issue: `POST /rest/api/3/issue`
- Bulk create issues: `POST /rest/api/3/issue/bulk`
- Edit issue: `PUT /rest/api/3/issue/{issueIdOrKey}`
- Get transitions: `GET /rest/api/3/issue/{issueIdOrKey}/transitions`
- Transition issue: `POST /rest/api/3/issue/{issueIdOrKey}/transitions`
- Get changelog: `GET /rest/api/3/issue/{issueIdOrKey}/changelog`
- Create issue link: `POST /rest/api/3/issueLink`
- Create version: `POST /rest/api/2/version`
- Create board: `POST /rest/agile/1.0/board`
- Read board configuration: `GET /rest/agile/1.0/board/{boardId}/configuration`
- Read board issues: `GET /rest/agile/1.0/board/{boardId}/issue`
- Read board epics: `GET /rest/agile/1.0/board/{boardId}/epic`
- Register webhooks for issues and versions through Jira Cloud webhook
  support.

Jira Cloud API notes that affect the design:

- The platform REST v3 API is the current Jira Cloud platform API.
- API v3 descriptions and comments use Atlassian Document Format for
  rich text.
- Jira has moved away from Epic Link/Parent Link toward a single
  `parent` field for issue hierarchy.
- Issue edit does not perform workflow transition; transitions must use
  the transition endpoint.
- Agile board configuration can be read through the API, and boards can
  be created from filters, but status-to-column configuration is
  primarily a Jira board/workflow administration concern.
- Jira webhooks may produce more than one issue update for a board drag:
  one for status transition and one for rank change. Gemba must de-dupe
  and classify webhook changes by changelog content.

Sources:

- https://developer.atlassian.com/cloud/jira/platform/rest/v3/intro/
- https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issues/
- https://developer.atlassian.com/cloud/jira/software/rest/api-group-board/
- https://developer.atlassian.com/cloud/jira/software/webhooks/
- https://support.atlassian.com/jira-software-cloud/docs/configure-columns/
- https://support.atlassian.com/jira/kb/how-to-handle-two-web-hooks-triggered-when-a-issue-is-transitioned-from-one-status-to-another-via-board/
- https://community.atlassian.com/forums/Jira-articles/Introducing-the-new-Parent-field-in-company-managed-projects/ba-p/2377758

## Product Model Mapping

| Gemba concept | Jira mapping | Recommendation | Fit |
| --- | --- | --- | --- |
| Workspace / scope | Jira site + project + board + optional repository metadata | One Gemba workspace binds to one Jira project and one primary board. | Good |
| Bead | Jira standard issue: Story, Task, Bug, Chore, Decision, or custom type | Use Jira issues as beads. Store original Gemba kind in issue type where possible, otherwise in custom field `Gemba Kind`. | Good |
| Epic | Jira Epic issue | Use Jira Epic. Children use `parent` field. | Good |
| Milestone | Jira Version, Jira Initiative/Feature, or custom Milestone issue | Prefer custom issue above Epic where Premium hierarchy exists. Otherwise use Version for release tracking plus issue links to epics. | Mixed |
| Dependency | Jira issue link, usually Blocks / Is blocked by | Use issue links. Gemba still computes runnable work from dependency graph. | Good |
| Acceptance criteria / DoD | ADF description section, custom field, or issue property | Use ADF description for human-readable content; issue property for structured Gemba contract. | Good |
| Evidence | Comments, attachments, remote links, issue property | Human evidence as comments/links; structured evidence in issue property. | Moderate |
| Escalation | Jira Flagged, label, custom field, comment, or separate issue | Use custom field `Gemba Attention` plus optional Jira flag. Keep full escalation conversation in Gemba. | Moderate |
| Agent/session state | Custom fields/properties, comments, or Gemba-only session store | Keep live session state Gemba-native; mirror summary fields to Jira. | Weak as Jira-only |
| Recent auto-created bead | JQL filter on created date and creator/label/property | Use quick filter or saved filter; Gemba board can expose richer recent view. | Good |
| Gemba Walk | Gemba-native review workflow with links/comments back to Jira | Jira has no exact native analog. | Weak |
| Token spend / generated LOC | Custom fields or issue properties | Mirror summary counters only. Detailed telemetry stays in Gemba. | Moderate |

## State and Board Design

### Recommended Jira workflow statuses

Use a minimal workflow aligned to Gemba's simplified board:

| Jira status | Jira board column | Gemba state category | Notes |
| --- | --- | --- | --- |
| Backlog | Backlog or Ready | `backlog` | Prefer not visible by default on execution board. |
| Ready | Ready | `unstarted` or `staged` | Card pill distinguishes "Next up", "Staged", or plain "Ready". |
| In Progress | In Progress | `started` | Transition into this status is the dispatch signal. |
| Done | Done | `completed` | Done must be mapped to the right-most Jira board column. |
| Canceled | Done or hidden/canceled column | `canceled` | If mapped to Done, preserve canceled distinction in field/property. |

Jira columns should stay simple. Gemba-specific sub-states should not
become separate columns unless the operator's Jira team explicitly wants
them. Extra columns create mismatches with Gemba's current board and
make automated dispatch harder to reason about.

### Required custom fields or properties

Use custom fields for data that Jira users should filter/report on.
Use issue properties for structured data mostly consumed by Gemba.

Recommended fields:

- `Gemba Kind`: bead, epic, milestone, decision, chore.
- `Gemba Dispatch State`: backlog, staged, ready, claimed, dispatched,
  blocked, review, done.
- `Gemba Attention`: none, triage, needs-input, escalation, review.
- `Gemba Session`: current session id or agent label.
- `Gemba Evidence State`: missing, partial, conforming.
- `Gemba Source Scope`: repository/worktree/scope id.

Recommended issue property namespace:

- `gemba.contract`: acceptance criteria, DoD, original bead id, canonical
  parent/children snapshot, dependency metadata.
- `gemba.runtime`: active sessions, latest lifecycle frame, token
  counters, escalation ids.
- `gemba.evidence`: generated files, test output, screenshots, commits,
  conformance summary.

### Card signals

Jira native cards can show selected fields, labels, priority, assignee,
flag, and issue type. Gemba should map important card pills to visible
Jira card fields where possible:

| Gemba card pill | Jira representation |
| --- | --- |
| Staged | `Gemba Dispatch State = staged` |
| Next up | `Gemba Dispatch State = ready` plus rank/order |
| Triage | `Gemba Attention = triage` and/or Jira Flagged |
| Needs input | `Gemba Attention = needs-input` |
| Review | `Gemba Dispatch State = review` |
| Working | `Gemba Session` non-empty or `Gemba Dispatch State = dispatched` |
| Evidence | `Gemba Evidence State` |

## Transition Signalling

### User drags a bead issue into In Progress

1. Jira transitions the issue to an In Progress status.
2. Jira emits `jira:issue_updated`.
3. Gemba webhook handler fetches or reads changelog details and detects
   a status transition into a `started` mapped status.
4. Gemba claims/dispatches the bead if it is runnable.
5. Gemba updates `Gemba Dispatch State`, `Gemba Session`, and issue
   property `gemba.runtime`.
6. The native OrchestrationPlane starts or reuses an agent session.
7. Agent lifecycle events remain in Gemba and are mirrored back to Jira
   as summary fields/comments only when useful.

If Jira also emits a rank-change webhook for the same drag, Gemba must
ignore the duplicate for dispatch purposes unless the changelog includes
a status change.

### User drags an epic into In Progress

1. Jira transitions the Epic to In Progress.
2. Gemba detects the Epic transition.
3. Gemba finds child issues by `parent = EPIC-KEY`.
4. Children in Backlog move to Ready/Staged.
5. Runnable children transition to In Progress up to concurrency limits.
6. Blocked children remain Ready/Staged with dependency or attention
   fields updated.
7. Gemba keeps the Epic status consistent with its children:
   - In Progress if at least one child is active or incomplete and work
     has begun.
   - Done when all non-canceled children are Done.
   - Ready/Backlog if no child has started.

Because Jira workflow transitions can be constrained, the adaptor must
call `GET /transitions` before every parent or child transition and
either pick the configured transition or report an actionable
configuration gap.

### User starts a milestone

There are two viable designs.

Option A: Milestone as Jira issue.

1. Milestone is an issue type on the Jira board.
2. User drags milestone to In Progress.
3. Gemba detects transition.
4. Gemba stages descendant epics and beads.
5. Gemba dispatches runnable child beads.
6. Gemba rolls up child status to milestone issue.

This is the best behavioral match, but it depends on Jira hierarchy and
workflow configuration. Above-Epic hierarchy is cleanest in Premium or
Enterprise. In standard Jira, the milestone issue can link to epics but
does not become their true parent.

Option B: Milestone as Jira Version.

1. Milestone is a project version/release.
2. Epics and beads use `fixVersions` or a custom `Gemba Milestone`
   field.
3. User starts the milestone from Gemba or by updating a version-related
   control.
4. Gemba stages and dispatches associated work.
5. Release completion maps to version release state.

This is a good release/reporting match, but it does not support native
Jira board drag/drop for milestones because versions are not board
cards with workflow status.

## Authoring Flows

### New project / onboarding

When the Onboarder or PM persona produces a plan:

1. Create or bind Jira project and board.
2. Create milestones:
   - as versions via `POST /rest/api/2/version`, or
   - as Milestone/Initiative issues via `POST /rest/api/3/issue`.
3. Create Epics.
4. Create bead issues with `parent` pointing to the Epic.
5. Create dependency links with `POST /rest/api/3/issueLink`.
6. Store structured Gemba metadata in issue properties.
7. Rank issues according to plan order where supported.

For large plans, use Jira bulk create where possible, but the adaptor
still needs multiple passes because child issues need parent keys and
dependencies need issue keys.

### Agent-created bead

When an agent creates new work during execution:

1. Gemba calls `POST /rest/api/3/issue` with the current Epic as parent
   when known.
2. If the parent Epic or Milestone is active, Gemba sets
   `Gemba Dispatch State = staged` or transitions the new bead to Ready.
3. If dependencies are satisfied and capacity exists, Gemba may
   transition the bead to In Progress and dispatch it.
4. The issue gets label/property `gemba:auto-created`.
5. Recent view maps to a JQL filter such as project = KEY and labels =
   gemba:auto-created and created >= -1d.

### PM/refinement edits

Persona skills that split, reorder, or reparent work translate to Jira
issue edits:

- Summary/description: issue edit.
- Parent Epic: edit `parent`.
- Milestone/release: edit `fixVersions` or custom field.
- Dependency: create/delete issue links.
- State: transition endpoint, not issue edit.
- Acceptance criteria: ADF description and/or custom field/property.

## Synchronization Architecture

### Components

1. `JiraWorkPlaneAdaptor`
   - Implements Gemba `WorkPlane`.
   - Translates Jira issues to `core.WorkItem`.
   - Translates `WorkItemPatch` to Jira edit/transition/link calls.
   - Publishes `WorkPlaneEvent` after every mutation.

2. `JiraWebhookReceiver`
   - Receives issue, comment, and version events.
   - Verifies webhook authenticity as supported by the chosen Atlassian
     app/auth model.
   - De-dupes status/rank double events.
   - Emits normalized work-plane events to Gemba.

3. `JiraSchemaInspector`
   - Reads create/edit metadata, board configuration, issue types,
     statuses, fields, transitions, and custom field ids.
   - Produces a startup readiness report.

4. `JiraProvisioner`
   - Optional setup assistant.
   - Creates filters/boards where APIs support it.
   - Validates board column mapping and workflow transitions.
   - Emits manual admin steps where public APIs are insufficient.

5. `JiraMirror`
   - Optional write-back layer for Gemba-native telemetry:
     session id, evidence summary, attention status, final comments.

### Event contract

Every successful WorkPlane mutation must result in a normalized event
visible to Gemba's UI:

- issue created -> work item created
- issue edited -> work item updated
- status transitioned -> state changed
- issue linked/unlinked -> dependency changed
- version released/updated -> milestone changed
- comment/evidence added -> evidence changed

The adaptor must be idempotent. Jira webhooks and Gemba's own mutation
responses can arrive in either order. Use a per-issue high-water mark
based on Jira changelog id, updated timestamp, and event timestamp.

## Configuration

Example workspace config:

```toml
[workplane.jira]
site = "https://example.atlassian.net"
cloud_id = "..."
project_key = "GEMBA"
board_id = 123
auth = "oauth_3lo"

[workplane.jira.mapping]
bead_issue_types = ["Story", "Task", "Bug", "Chore", "Decision"]
epic_issue_type = "Epic"
milestone_mode = "issue" # issue | version
milestone_issue_type = "Initiative"

[workplane.jira.fields]
gemba_kind = "customfield_12345"
gemba_dispatch_state = "customfield_12346"
gemba_attention = "customfield_12347"
gemba_session = "customfield_12348"
gemba_evidence_state = "customfield_12349"

[workplane.jira.status_map]
Backlog = "backlog"
Ready = "staged"
"To Do" = "unstarted"
"In Progress" = "started"
Done = "completed"
Canceled = "canceled"
```

## Fit / Gap Assessment

### Strong fit

- Jira is a mature work tracker and can be the durable data plane.
- Issue CRUD maps directly to bead CRUD.
- Epics and child beads map well through Jira Epic + `parent`.
- Status transitions are first-class and can be observed through
  webhooks.
- Dependency links map to Jira issue links.
- Existing enterprise teams already understand boards, releases,
  fields, permissions, and audit trails.
- Gemba's current three-lane board fits Jira's default Kanban shape.

### Moderate fit

- Card pills can be represented by fields/labels/flags, but Jira cards
  will not feel as rich as Gemba's custom board without configuration.
- Wrapper rollups can be implemented, but Jira will not naturally keep
  Epic/Milestone status equal to child state unless Gemba or Jira
  Automation does it.
- Evidence can be stored in comments/properties, but Jira is not ideal
  for structured generated evidence, test output, screen captures, and
  agent traces.
- Recent auto-created work is feasible with JQL, but Gemba's recent
  surface remains more direct.
- Workflow transitions can be automated, but transition availability is
  workflow-specific and must be discovered at runtime.

### Weak fit / compromises

- Jira versions are good release analogs but poor draggable milestone
  wrappers.
- Above-Epic milestone hierarchy is clean only with Premium/Enterprise
  hierarchy customization. Standard Jira requires issue links or custom
  fields.
- Jira board status/column configuration cannot be fully controlled
  through the public Agile board API. We can create a board and inspect
  configuration, but "properly configured" status mapping needs project
  template discipline, Jira admin setup, or guided manual steps.
- Live agent session state, token spend, source graph freshness, Gemba
  Walks, and escalation conversations are Gemba-native concepts. Jira
  can mirror summaries, not replace those surfaces cleanly.
- Jira permissions, workflow validators, required transition screens,
  and field contexts can block otherwise valid Gemba actions.

## Recommendation

Build this as a first-class `jira` WorkPlane adaptor, not as a total
product replacement.

Use Jira as the durable planning and board substrate:

- Jira issues are beads.
- Jira Epics are Gemba epics.
- Jira issue links are dependencies.
- Jira transitions are state changes.
- Jira webhooks are the primary external state signal.

Keep Gemba as the orchestration and intelligence layer:

- dispatch planning
- session status
- escalation triage
- evidence synthesis
- Gemba Walks
- source-analysis freshness
- agent-created work handling
- parent rollup enforcement

For milestones, support two modes:

1. `milestone_mode = "issue"` for customers who can model milestones as
   Jira issues on the board. This is the best functional match.
2. `milestone_mode = "version"` for customers who want release-native
   reporting and accept that milestone start/drag gestures remain in
   Gemba rather than Jira.

Overall fit: 7/10 as a WorkPlane replacement, 5/10 as a full Gemba board
replacement, 8/10 as a hybrid where Jira owns durable work state and
Gemba owns agentic execution.

## Open Product Questions

1. Is the goal for users to spend most planning time in Jira, or should
   Jira be the durable backend while Gemba remains the primary execution
   cockpit?
2. Is Jira Premium/Enterprise acceptable for the best milestone model,
   or must standard Jira Software be enough?
3. Should Gemba write parent rollup transitions directly, or should it
   install Jira Automation rules and merely observe the result?
4. Which Gemba fields must be visible on Jira cards versus stored as
   hidden issue properties?
5. Should evidence be mirrored eagerly to Jira comments or kept in Gemba
   with a final summary comment on completion?

## MVP Acceptance Criteria

1. Operator configures a Jira site/project/board and starts Gemba with
   `--workplane=jira`.
2. Gemba lists Jira board issues as WorkItems with correct kind, parent,
   status, state category, dependencies, labels, and custom fields.
3. Creating an Epic and bead in Gemba creates Jira issues with correct
   parent/metadata.
4. Dragging a bead to In Progress in Jira starts Gemba dispatch when the
   bead is runnable.
5. Dragging an Epic to In Progress stages and dispatches runnable child
   beads.
6. When all child beads complete, Gemba transitions the Epic to Done
   or reports the exact Jira transition/configuration blocker.
7. Agent-created beads appear in Jira, attach to the active Epic, and
   are visible through a recent-work filter.
8. Open escalations are visible on Jira cards through a field/flag and
   clear when the escalation is resolved in Gemba.
9. The adaptor emits normalized WorkPlane events within the declared
   latency budget after Jira webhook receipt or Gemba-originated
   mutation.
10. Startup validation reports missing fields, missing statuses,
   unmapped board columns, unavailable transitions, and insufficient
   permissions before dispatch is enabled.

