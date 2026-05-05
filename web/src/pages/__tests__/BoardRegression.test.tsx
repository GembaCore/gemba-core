// Board regression suite (gm-root.2). Drives the canonical project
// fixture at testing/fixtures/project-canonical.jsonl through the full
// Board pane + drawer and asserts DOM shape against the coverage
// matrix the fixture was built to exercise.
//
// Why this test exists: BoardPage.test.tsx covers narrow unit behaviour
// (one scope, one drawer, etc). This file covers the shape of a
// realistic project — default status board, legacy epic swimlanes,
// orphan lane, the 4-deep blocks chain, cross-prefix relations, closed
// epics, sprint membership. A layout regression (missing column,
// mis-grouped legacy swimlane, drawer field dropped) shows up here.
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
import type { BoardColumnID } from '@/components/board/boardColumns';
import { RhpProvider } from '@/components/rhp/RhpContext';
import { RhpPinnedContentProvider } from '@/components/rhp/RhpPinnedContent';
import { RhpShell } from '@/components/rhp/RhpShell';

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
  // Preload the per-id detail cache too — EpicDetail's useWorkItem(id)
  // fetches /api/work-items/{id} otherwise and the test harness has no
  // backing server. Populating it from the same fixture keeps the
  // detail test hermetic.
  for (const it of items) {
    client.setQueryData(workItemsKeys.detail(it.id), it);
  }
  const registry = new HotkeyRegistry();
  const ui: ReactNode = (
    <MemoryRouter initialEntries={[initialEntry]}>
      <RhpProvider>
        <RhpPinnedContentProvider>
          <QueryClientProvider client={client}>
            <CapabilitiesProvider initial={caps}>
              <HotkeysContext.Provider value={registry}>
                <Routes>
                  <Route path="/board" element={<BoardPage />} />
                  <Route path="/board/*" element={<BoardPage />} />
                </Routes>
                <RhpShell />
              </HotkeysContext.Provider>
            </CapabilitiesProvider>
          </QueryClientProvider>
        </RhpPinnedContentProvider>
      </RhpProvider>
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

  it('renders the default status board over the canonical fixture', async () => {
    const items = loadFixture();
    mountBoard(items);
    await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());
    expect(screen.getByTestId('board-column-ready')).toBeTruthy();
    expect(screen.getByTestId('board-column-started')).toBeTruthy();
    expect(screen.getByTestId('board-column-completed')).toBeTruthy();
    expect(screen.queryByTestId('board-column-backlog')).toBeNull();
    expect(screen.getByText('Foundation: embed pipeline')).toBeTruthy();
    expect(screen.getByText('Phase 3: Closed epic')).toBeTruthy();
  });

  it('keeps the legacy Epic layout addressable for top-level roots and orphan lanes', async () => {
    const items = loadFixture();
    mountBoard(items, '/board?layout=epic');
    await waitFor(() => expect(screen.getByTestId('board-epic')).toBeTruthy());
    // pc-root is a real top-level root; pc-orphan is a separate
    // top-level root; no synthetic ORPHAN_ROOT_ID is needed because
    // every epic with a parent_child edge points at an epic that IS
    // in the fixture.
    expect(screen.getByTestId('board-epic-swimlane-demo/pc-root')).toBeTruthy();
    expect(screen.getByTestId('board-epic-swimlane-demo/pc-orphan')).toBeTruthy();
  });

  it('scope picker enumerates every top-level root in its dropdown', () => {
    const items = loadFixture();
    mountBoard(items);
    // gm-uekk: the old root-epic banner is gone; root epics now
    // surface as picker options. Open the picker and assert the
    // canonical roots are present.
    fireEvent.click(screen.getByTestId('board-scope-trigger'));
    expect(screen.getByTestId('board-scope-option-demo/pc-root')).toBeTruthy();
    expect(screen.getByTestId('board-scope-option-demo/pc-orphan')).toBeTruthy();
    // "All" option always sits at the top.
    expect(screen.getByTestId('board-scope-option-all')).toBeTruthy();
  });

  it('board-epic-cell-* counts match the fixture per-state-category breakdown', () => {
    const items = loadFixture();
    // gm-5ekd: backlog column is hidden by default; this regression
    // test asserts every state bucket including backlog, so render
    // with the toggle on.
    mountBoard(items, '/board?layout=epic&show_backlog=1');
    // Per-root expected counts of epics (kind==='epic') by visual board
    // column. Ready collapses canonical unstarted + staged.
    const epicsByRoot: Record<string, Partial<Record<BoardColumnID, number>>> = {
      'demo/pc-root': { backlog: 1, started: 1, ready: 2, completed: 1 },
      'demo/pc-orphan': { backlog: 1 },
    };
    // pc-e4 is a child epic under pc-e1, which rolls up into pc-root's
    // swimlane — add it to pc-root's Ready bucket.
    epicsByRoot['demo/pc-root'].ready = (epicsByRoot['demo/pc-root'].ready ?? 0) + 1;
    for (const [rootID, byState] of Object.entries(epicsByRoot)) {
      for (const [cat, expected] of Object.entries(byState)) {
        const cell = screen.getByTestId(`board-epic-cell-${rootID}-${cat}`);
        const cards = cell.querySelectorAll('[data-epic-card="true"]');
        expect(cards.length, `${rootID}/${cat} card count`).toBe(expected);
      }
    }
  });

  it('deep-link /board/:epicId opens the RHP epic detail for a representative fixture bead', async () => {
    const items = loadFixture();
    mountBoard(items, '/board/demo/pc-e3');
    await waitFor(() => expect(screen.getByTestId('epic-detail-id')).toBeTruthy());
    // The detail tab exposes the epic id, title, state, child list, and the
    // description block. Every one of these axes must be present or we
    // lost a field somewhere along the way.
    expect(screen.getByTestId('epic-detail-id').textContent).toBe('demo/pc-e3');
    // Title appears in both the board's EpicCard and the RHP detail
    // header — assert it shows up at least once via getAllByText.
    await waitFor(() =>
      expect(screen.getAllByText('Phase 3: Closed epic').length).toBeGreaterThan(0)
    );
    expect(screen.getByTestId('epic-section-state')).toBeTruthy();
    expect(screen.getByTestId('epic-section-description')).toBeTruthy();
    expect(screen.getByTestId('epic-section-children')).toBeTruthy();
    // pc-e3 has pc-11, pc-12, pc-13, pc-14 as children. All four
    // should show up under the appropriate state buckets.
    for (const childID of ['demo/pc-11', 'demo/pc-12', 'demo/pc-13', 'demo/pc-14']) {
      expect(
        screen.getAllByText(childID, { selector: 'span' }).length,
        `missing child row for ${childID}`
      ).toBeGreaterThan(0);
    }
  });

  it('selecting a scope from the picker narrows the board to that lineage', async () => {
    const items = loadFixture();
    mountBoard(items, '/board?show_backlog=1');
    fireEvent.click(screen.getByTestId('board-scope-trigger'));
    fireEvent.click(screen.getByTestId('board-scope-option-demo/pc-orphan'));
    // The pc-orphan lineage stays visible; pc-root gets filtered out.
    await waitFor(() => {
      expect(screen.getAllByText('Orphan epic (top-level)').length).toBeGreaterThan(0);
      expect(screen.queryByText('Demo project root epic')).toBeNull();
    });
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
          <RhpProvider>
            <RhpPinnedContentProvider>
              <QueryClientProvider client={client}>
                <CapabilitiesProvider initial={caps}>
                  <HotkeysContext.Provider value={registry}>
                    <Routes>
                      <Route path="/board" element={<BoardPage />} />
                    </Routes>
                  </HotkeysContext.Provider>
                </CapabilitiesProvider>
              </QueryClientProvider>
            </RhpPinnedContentProvider>
          </RhpProvider>
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
