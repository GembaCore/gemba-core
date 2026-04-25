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

export interface WorkPlaneStore {
  seed(items: WorkItem[]): void;
  add(item: WorkItem): void;
  get(id: string): WorkItem | undefined;
  list(): WorkItem[];
  clear(): void;
}

export function createWorkPlane(): WorkPlaneStore {
  const byId = new Map<string, WorkItem>();
  return {
    seed(items) {
      byId.clear();
      for (const it of items) byId.set(it.id, it);
    },
    add(item) {
      byId.set(item.id, item);
    },
    get(id) {
      return byId.get(id);
    },
    list() {
      return Array.from(byId.values());
    },
    clear() {
      byId.clear();
    },
  };
}
