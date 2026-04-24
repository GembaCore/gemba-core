// Board regression suite (gm-root.2). Drives the canonical project
// fixture at testing/fixtures/project-canonical.jsonl through the full
// Board pane + drawer and asserts DOM shape against the coverage
// matrix the fixture was built to exercise.
//
// Why this test exists: BoardPage.test.tsx covers narrow unit behaviour
// (one swimlane, one drawer, etc). This file covers the shape of a
// realistic project — swimlanes by root, orphan lane, the 4-deep blocks
// chain, cross-prefix relations, closed epics, sprint membership. A
// layout regression (missing column, mis-grouped swimlane, drawer
// field dropped) shows up here.
//
// Determinism: React Query cache is preloaded from the JSONL rather
// than fetched. No wall-clock assertions — the one component that
// reads "now" (EpicCard's relative-time footer) is not asserted on
// here, so fake timers are not needed and in fact break
// waitFor()'s polling.

import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { BoardPage } from '../BoardPage';
import { workItemsKeys } from '@/hooks/useWorkItems';
import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';
import { HotkeyRegistry, HotkeysContext } from '@/hotkeys';
import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';

const FIXTURE_PATH = path.resolve(
  __dirname,
  '../../../../testing/fixtures/project-canonical.jsonl'
);

function loadFixture(): WorkItem[] {
  const raw = readFileSync(FIXTURE_PATH, 'utf8');
  return raw
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .map((line, i) => {
      try {
        return JSON.parse(line) as WorkItem;
      } catch (err) {
        throw new Error(
          `project-canonical.jsonl: invalid JSON on line ${i + 1}: ${(err as Error).message}`
        );
      }
    });
}

const caps: CapabilitiesResponse = {
  work_plane: {
    adaptor_name: 'fake',
    adaptor_version: '0.1.0',
    protocol_version: '0.1.0',
    transport: 'api',
    state_map: { open: 'unstarted', in_progress: 'started', closed: 'completed' },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
  },
  orchestration_plane: null,
};

function mountBoard(items: WorkItem[], initialEntry = '/board') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(workItemsKeys.list(), items);
  // Preload the per-id detail cache too — the EpicDrawer's useWorkItem(id)
  // fetches /api/work-items/{id} otherwise and the test harness has no
  // backing server. Populating it from the same fixture keeps the
  // drawer test hermetic.
  for (const it of items) {
    client.setQueryData(workItemsKeys.detail(it.id), it);
  }
  const registry = new HotkeyRegistry();
  const ui: ReactNode = (
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps}>
          <HotkeysContext.Provider value={registry}>
            <Routes>
              <Route path="/board" element={<BoardPage />} />
              <Route path="/board/*" element={<BoardPage />} />
            </Routes>
          </HotkeysContext.Provider>
        </CapabilitiesProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
  return render(ui);
}

describe('Board regression — project-canonical fixture', () => {
  it('fixture parses into ~30 WorkItems covering every coverage axis', () => {
    const items = loadFixture();
    expect(items.length).toBeGreaterThanOrEqual(25);
    expect(items.length).toBeLessThanOrEqual(40);
    // Every core state_category represented at least once.
    const byState = new Map<StateCategory, number>();
    for (const cat of STATE_CATEGORIES) byState.set(cat, 0);
    for (const it of items) byState.set(it.state_category, (byState.get(it.state_category) ?? 0) + 1);
    for (const cat of STATE_CATEGORIES) {
      expect(byState.get(cat), `state_category ${cat} missing from fixture`).toBeGreaterThan(0);
    }
    // Every priority P0–P3 represented.
    const priorities = new Set<number>();
    for (const it of items) if (typeof it.priority === 'number') priorities.add(it.priority);
    for (const p of [0, 1, 2, 3]) {
      expect(priorities.has(p), `priority P${p} missing from fixture`).toBe(true);
    }
    // Evidence items (>=3), sprint membership (>=1), custom beads:* (>=2),
    // close_reason (>=1), markdown description (>=1), DoD attached (>=1).
    const withEvidence = items.filter((i) => (i.evidence ?? []).length > 0);
    expect(withEvidence.length).toBeGreaterThanOrEqual(3);
    expect(items.some((i) => !!i.sprint_id)).toBe(true);
    const beadsCustom = items.filter((i) =>
      Object.keys(i.custom ?? {}).some((k) => k.startsWith('beads:'))
    );
    expect(beadsCustom.length).toBeGreaterThanOrEqual(2);
    expect(items.some((i) => typeof i.custom?.['close_reason'] === 'string')).toBe(true);
    expect(items.some((i) => (i.description ?? '').length > 200)).toBe(true);
    expect(items.some((i) => !!i.dod)).toBe(true);
    // Cross-prefix relation: at least 2 items with an id prefix that
    // differs from the prefix on one end of a relationship.
    const crossPrefix = items.filter((i) =>
      (i.relationships ?? []).some((r) => r.from.split('/')[0] !== r.to.split('/')[0])
    );
    expect(crossPrefix.length).toBeGreaterThanOrEqual(2);
    // Blocks-chain depth ≥ 4: follow 'blocks' edges from some head.
    const blocksByFrom = new Map<string, string[]>();
    for (const it of items) {
      for (const r of it.relationships ?? []) {
        if (r.kind !== 'blocks') continue;
        const list = blocksByFrom.get(r.from) ?? [];
        list.push(r.to);
        blocksByFrom.set(r.from, list);
      }
    }
    const longestBlocksChain = (start: string): number => {
      const seen = new Set<string>([start]);
      let best = 1;
      const walk = (cur: string, depth: number): void => {
        best = Math.max(best, depth);
        for (const next of blocksByFrom.get(cur) ?? []) {
          if (seen.has(next)) continue;
          seen.add(next);
          walk(next, depth + 1);
          seen.delete(next);
        }
      };
      walk(start, 1);
      return best;
    };
    const deepestBlocks = Math.max(
      0,
      ...Array.from(blocksByFrom.keys()).map(longestBlocksChain)
    );
    expect(deepestBlocks).toBeGreaterThanOrEqual(4);
  });

  it('renders an Epic swimlane per top-level root + an orphan lane', async () => {
    const items = loadFixture();
    mountBoard(items);
    await waitFor(() => expect(screen.getByTestId('board-epic')).toBeTruthy());
    // pc-root is a real top-level root; pc-orphan is a separate
    // top-level root; no synthetic ORPHAN_ROOT_ID is needed because
    // every epic with a parent_child edge points at an epic that IS
    // in the fixture.
    expect(screen.getByTestId('board-epic-swimlane-demo/pc-root')).toBeTruthy();
    expect(screen.getByTestId('board-epic-swimlane-demo/pc-orphan')).toBeTruthy();
  });

  it('renders the root-epic banner listing every top-level root', () => {
    const items = loadFixture();
    mountBoard(items);
    const banner = screen.getByTestId('board-root-epic-banner');
    expect(banner).toBeTruthy();
    // Both top-level epics appear as banner chips.
    expect(screen.getByTestId('board-root-epic-demo/pc-root')).toBeTruthy();
    expect(screen.getByTestId('board-root-epic-demo/pc-orphan')).toBeTruthy();
  });

  it('board-epic-cell-* counts match the fixture per-state-category breakdown', () => {
    const items = loadFixture();
    mountBoard(items);
    // Per-root expected counts of epics (kind==='epic') by state_category.
    const epicsByRoot: Record<string, Partial<Record<StateCategory, number>>> = {
      'demo/pc-root': { backlog: 1, started: 1, unstarted: 1, completed: 1 },
      'demo/pc-orphan': { backlog: 1 },
    };
    // pc-e4 is a child epic under pc-e1, which rolls up into pc-root's
    // swimlane — add it to pc-root's unstarted bucket.
    epicsByRoot['demo/pc-root'].unstarted = (epicsByRoot['demo/pc-root'].unstarted ?? 0) + 1;
    for (const [rootID, byState] of Object.entries(epicsByRoot)) {
      for (const [cat, expected] of Object.entries(byState)) {
        const cell = screen.getByTestId(`board-epic-cell-${rootID}-${cat}`);
        const cards = cell.querySelectorAll('[data-epic-card="true"]');
        expect(cards.length, `${rootID}/${cat} card count`).toBe(expected);
      }
    }
  });

  it('deep-link /board/:epicId opens the drawer for a representative fixture bead', async () => {
    const items = loadFixture();
    mountBoard(items, '/board/demo/pc-e3');
    await waitFor(() => expect(screen.getByTestId('epic-drawer-content')).toBeTruthy());
    // The drawer exposes the epic id, title, state, child list, and the
    // description block. Every one of these axes must be present or we
    // lost a drawer field somewhere along the way.
    expect(screen.getByTestId('epic-drawer-id').textContent).toBe('demo/pc-e3');
    // Title appears both on the root-epic banner chip and in the
    // drawer header — scope the assertion to the drawer.
    const drawer = screen.getByTestId('epic-drawer-content');
    expect(drawer.textContent).toContain('Phase 3: Closed epic');
    expect(screen.getByTestId('epic-section-state')).toBeTruthy();
    expect(screen.getByTestId('epic-section-description')).toBeTruthy();
    expect(screen.getByTestId('epic-section-children')).toBeTruthy();
    // pc-e3 has pc-11, pc-12, pc-13, pc-14 as children. All four
    // should show up under the appropriate state buckets.
    for (const childID of ['demo/pc-11', 'demo/pc-12', 'demo/pc-13', 'demo/pc-14']) {
      expect(
        screen.getByText(childID, { selector: 'span' }),
        `missing child row for ${childID}`
      ).toBeTruthy();
    }
  });

  it('opening an Epic from the banner navigates to its drawer route', async () => {
    const items = loadFixture();
    mountBoard(items);
    fireEvent.click(screen.getByTestId('board-root-epic-demo/pc-orphan'));
    await waitFor(() => expect(screen.getByTestId('epic-drawer-content')).toBeTruthy());
    expect(screen.getByTestId('epic-drawer-id').textContent).toBe('demo/pc-orphan');
  });
});

describe('Board regression — degraded adaptor', () => {
  it('renders board-error when the adaptor reports degraded', async () => {
    // Return a 503 carrying the adaptor_degraded envelope — the ApiError
    // wrapper recognizes this as non-retryable so the hook surfaces
    // isError immediately instead of queuing a retry (useWorkItems.ts).
    const degradedResponse = () =>
      new Response(
        JSON.stringify({ error: 'adaptor_degraded', message: 'backend offline' }),
        { status: 503, headers: { 'Content-Type': 'application/json' } }
      );
    const fetchSpy = vi.fn().mockImplementation(() => Promise.resolve(degradedResponse()));
    vi.stubGlobal('fetch', fetchSpy);
    try {
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      const registry = new HotkeyRegistry();
      render(
        <MemoryRouter initialEntries={['/board']}>
          <QueryClientProvider client={client}>
            <CapabilitiesProvider initial={caps}>
              <HotkeysContext.Provider value={registry}>
                <Routes>
                  <Route path="/board" element={<BoardPage />} />
                </Routes>
              </HotkeysContext.Provider>
            </CapabilitiesProvider>
          </QueryClientProvider>
        </MemoryRouter>
      );
      // Board ends up in its error state (isError=true) and the body
      // carries the board-error testid. The AdaptorBanner itself is
      // driven by /api/adaptors which ALSO returns 503 here, so it
      // silently stays absent — that's intentional per
      // AdaptorBanner.tsx: it only renders when /api/adaptors parses
      // cleanly.
      await waitFor(() => expect(screen.getByTestId('board-error')).toBeTruthy());
      expect(screen.getByText(/Could not load beads/i)).toBeTruthy();
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
