// fixtures/workplane.ts
//
// In-memory WorkPlane store consulted by the fake-backend route
// dispatcher in fixtures/server.ts. Each test gets its own store
// instance so seeded items can't bleed across specs.
//
// Usage from a spec:
//
//   import { test, expect } from '../../fixtures/server';
//   import * as build from '../../builders/workitem';
//
//   test('drawer renders bead', async ({ page, workPlane }) => {
//     workPlane.seed([
//       build.workItem({ id: 'gm-1', title: 'Hello' }),
//     ]);
//     await page.goto('/board?bead=gm-1');
//     // ...
//   });
//
// The store mutates synchronously; the route handler reads the
// current snapshot when each request arrives, so post-seed
// changes (workPlane.add(...) mid-test) do propagate.

import type { WorkItem } from '../../../web/src/types/core.gen';

export interface FakeBeadsHistoryEvent {
  event_id: string;
  occurred_at: string;
  actor: string;
  mode: 'beads_only';
  action: string;
  entity: { type: string; id: string; title?: string };
  before?: Record<string, unknown>;
  after?: Record<string, unknown>;
  summary: string;
}

export interface WorkPlaneStore {
  seed(items: WorkItem[]): void;
  add(item: WorkItem): void;
  update(id: string, patch: Partial<WorkItem>): WorkItem | undefined;
  remove(id: string): WorkItem | undefined;
  addHistory(event: FakeBeadsHistoryEvent): void;
  history(): FakeBeadsHistoryEvent[];
  get(id: string): WorkItem | undefined;
  list(): WorkItem[];
  clear(): void;
}

export function createWorkPlane(): WorkPlaneStore {
  const byId = new Map<string, WorkItem>();
  const history: FakeBeadsHistoryEvent[] = [];
  return {
    seed(items) {
      byId.clear();
      for (const it of items) byId.set(it.id, it);
    },
    add(item) {
      byId.set(item.id, item);
    },
    update(id, patch) {
      const before = byId.get(id);
      if (!before) return undefined;
      const next = { ...before, ...patch, updated_at: new Date().toISOString() };
      byId.set(id, next);
      return next;
    },
    remove(id) {
      const before = byId.get(id);
      if (!before) return undefined;
      byId.delete(id);
      return before;
    },
    addHistory(event) {
      history.push(event);
    },
    history() {
      return [...history];
    },
    get(id) {
      return byId.get(id);
    },
    list() {
      return Array.from(byId.values());
    },
    clear() {
      byId.clear();
      history.length = 0;
    },
  };
}
