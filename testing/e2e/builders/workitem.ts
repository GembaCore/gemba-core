// builders/workitem.ts
//
// Builders over fixture-as-data: every spec that needs a WorkItem
// gets one through these factories so the wire shape stays in sync
// with web/src/types/core.gen.ts WorkItem. Each builder takes an
// override object, deep-merges over a sensible default, and returns
// the materialized record.
//
// The shapes here mirror the Go side (internal/core/workplane.go).
// When core.gen.ts grows new required fields, this file is the only
// place the e2e library needs to track them.

// Types come straight from the SPA's wire schema so the builders
// can never drift from the React side. core.gen.ts is generated;
// when its shape changes, builders fail typecheck — caught at PR time.
import type {
  AgentRef,
  DefinitionOfDone,
  Relationship,
  StateCategory,
  WorkItem,
} from '../../../web/src/types/core.gen';

let counter = 0;

const FIXED_TIMESTAMP = '2026-04-25T00:00:00Z';

// nextId yields stable ids across a single test run. Specs that need
// deterministic ids should pass `id` explicitly; this is just for
// convenience when the id doesn't matter to the assertion.
function nextId(prefix: string): string {
  counter += 1;
  return `${prefix}-${counter}`;
}

export function workItem(overrides: Partial<WorkItem> = {}): WorkItem {
  return {
    id: overrides.id ?? nextId('wi'),
    kind: 'task',
    title: 'Test work item',
    status: 'open',
    state_category: 'unstarted',
    created_at: FIXED_TIMESTAMP,
    updated_at: FIXED_TIMESTAMP,
    derived: { agent_claimable: true, human_action_required: false, review_pending: false },
    ...overrides,
  };
}

export function epic(overrides: Partial<WorkItem> = {}): WorkItem {
  return workItem({
    id: overrides.id ?? nextId('ep'),
    kind: 'epic',
    title: 'Test epic',
    state_category: 'unstarted',
    ...overrides,
  });
}

export function dod(criteria: string[], extras: Partial<DefinitionOfDone> = {}): DefinitionOfDone {
  return {
    acceptance_criteria: criteria,
    ...extras,
  };
}

export function relationship(
  kind: Relationship['kind'],
  from: string,
  to: string
): Relationship {
  return { kind, from, to };
}

export function agentRef(overrides: Partial<AgentRef> = {}): AgentRef {
  return {
    id: 'gemba/crew/mike',
    name: 'mike',
    agent_kind: 'agent',
    role: 'crew',
    workspace: 'gemba',
    ...overrides,
  };
}

// Convenience: make a parent_child relationship "parent → child".
export function parentChild(parent: string, child: string): Relationship {
  return { kind: 'parent_child', from: parent, to: child };
}

// resetIds is called automatically between tests by the fixture so
// counter drift doesn't leak across specs.
export function resetIds(): void {
  counter = 0;
}

export type { WorkItem, StateCategory, DefinitionOfDone, Relationship, AgentRef };
