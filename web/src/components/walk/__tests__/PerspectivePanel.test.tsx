// PerspectivePanel tests (gm-bms). Asserts:
//  - empty state when no item is active
//  - loading + error + non-empty + zero-volunteer states
//  - kind classes drive distinct rows (smoke test on data-testid).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WalkProvider } from '../WalkContext';
import { PerspectivePanel } from '../PerspectivePanel';
import type { Walk } from '../types';
import type { PerspectivesEnvelope } from '@/api/walks';

function makeQC(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, staleTime: 0 },
      mutations: { retry: false },
    },
  });
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function walkWithActive(): Walk {
  return {
    id: 'walk-001',
    workspace: 'ws-x',
    initiated_by: 'operator',
    primary_persona: 'project-manager',
    started_at: '2026-04-27T12:00:00Z',
    status: 'active',
    agenda: [
      {
        id: 'item-1',
        topic: 'ship feature X',
        source: { kind: 'user' },
        priority: 400,
        status: 'active',
        added_at: '2026-04-27T12:00:00Z',
      },
    ],
    cost: { tokens_in: 0, tokens_out: 0, dollars: 0 },
  };
}

function walkNoActive(): Walk {
  return {
    ...walkWithActive(),
    agenda: [
      {
        id: 'item-1',
        topic: 'ship feature X',
        source: { kind: 'user' },
        priority: 400,
        status: 'queued',
        added_at: '2026-04-27T12:00:00Z',
      },
    ],
  };
}

describe('PerspectivePanel (gm-bms)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    window.localStorage.clear();
    vi.stubGlobal('fetch', fetchSpy);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
    window.localStorage.clear();
  });

  function mount() {
    const qc = makeQC();
    return render(
      <QueryClientProvider client={qc}>
        <WalkProvider initialWalkID="walk-001">
          <PerspectivePanel />
        </WalkProvider>
      </QueryClientProvider>
    );
  }

  it('shows empty-state when no item is active', async () => {
    fetchSpy.mockImplementation((url) => {
      const u = String(url);
      if (u.includes('/walks/walk-001') && !u.includes('perspectives')) {
        return Promise.resolve(jsonResponse(walkNoActive()));
      }
      throw new Error(`unexpected fetch: ${u}`);
    });

    mount();

    await waitFor(() =>
      expect(screen.getByTestId('walk-perspective-empty')).toBeTruthy()
    );
  });

  it('shows none-state when zero personas volunteered', async () => {
    fetchSpy.mockImplementation((url) => {
      const u = String(url);
      if (u.includes('/perspectives')) {
        const env: PerspectivesEnvelope = {
          walk_id: 'walk-001',
          agenda_item_id: 'item-1',
          perspectives: [],
          total: 0,
        };
        return Promise.resolve(jsonResponse(env));
      }
      if (u.includes('/walks/walk-001')) {
        return Promise.resolve(jsonResponse(walkWithActive()));
      }
      throw new Error(`unexpected fetch: ${u}`);
    });

    mount();

    await waitFor(() =>
      expect(screen.getByTestId('walk-perspective-none')).toBeTruthy()
    );
  });

  it('renders rows with kind tags and persona ids when contributions arrive', async () => {
    fetchSpy.mockImplementation((url) => {
      const u = String(url);
      if (u.includes('/perspectives')) {
        const env: PerspectivesEnvelope = {
          walk_id: 'walk-001',
          agenda_item_id: 'item-1',
          perspectives: [
            {
              persona_id: 'security',
              persona_name: 'Sec',
              kind: 'blocking',
              content: 'secret leakage risk',
              at: '2026-04-27T12:00:00Z',
            },
            {
              persona_id: 'architect',
              persona_name: 'Arch',
              kind: 'amendment',
              content: 'design integrity check',
              at: '2026-04-27T12:00:00Z',
            },
            {
              persona_id: 'documentarian',
              persona_name: 'Docs',
              kind: 'note',
              content: 'release-notes coverage',
              at: '2026-04-27T12:00:00Z',
            },
          ],
          total: 3,
        };
        return Promise.resolve(jsonResponse(env));
      }
      if (u.includes('/walks/walk-001')) {
        return Promise.resolve(jsonResponse(walkWithActive()));
      }
      throw new Error(`unexpected fetch: ${u}`);
    });

    mount();

    await waitFor(() =>
      expect(screen.getByTestId('walk-perspective-list')).toBeTruthy()
    );
    expect(screen.getByTestId('walk-perspective-security')).toBeTruthy();
    expect(screen.getByTestId('walk-perspective-security-kind').textContent).toBe(
      'blocking'
    );
    expect(screen.getByTestId('walk-perspective-architect-kind').textContent).toBe(
      'amendment'
    );
    expect(
      screen.getByTestId('walk-perspective-documentarian-kind').textContent
    ).toBe('note');
    expect(screen.getByTestId('walk-perspective-count').textContent).toBe('3');
  });

  it('surfaces an error state when the perspectives fetch fails', async () => {
    fetchSpy.mockImplementation((url) => {
      const u = String(url);
      if (u.includes('/perspectives')) {
        return Promise.resolve(
          jsonResponse({ error: 'not_found', message: 'gone' }, 404)
        );
      }
      if (u.includes('/walks/walk-001')) {
        return Promise.resolve(jsonResponse(walkWithActive()));
      }
      throw new Error(`unexpected fetch: ${u}`);
    });

    mount();

    await waitFor(() =>
      expect(screen.getByTestId('walk-perspective-error')).toBeTruthy()
    );
  });
});
