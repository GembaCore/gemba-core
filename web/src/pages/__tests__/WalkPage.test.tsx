// /walk page tests (gm-i65). Covers the three-pane layout, R/M/X/D
// hotkeys via the registered scope, and PM-panel takeover sync. The
// backend /api/v1/walks/* endpoints are mocked at the fetch layer.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WalkPage } from '../WalkPage';
import { ActiveWalkBanner } from '@/components/walk/ActiveWalkBanner';
import { AppWalkBindings } from '@/components/walk/AppWalkBindings';
import { WalkProvider, useWalk } from '@/components/walk/WalkContext';
import { PmPanelProvider, usePmPanel } from '@/components/pm/PmPanelContext';
import { HotkeysProvider } from '@/hotkeys';
import type { Walk } from '@/api/walks';

function makeQC(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, staleTime: 0 },
      mutations: { retry: false },
    },
  });
}

function wrap(children: ReactNode): JSX.Element {
  return (
    <MemoryRouter initialEntries={['/walk']}>
      <QueryClientProvider client={makeQC()}>
        <HotkeysProvider>
          <PmPanelProvider>
            <WalkProvider>
              <AppWalkBindings />
              {children}
            </WalkProvider>
          </PmPanelProvider>
        </HotkeysProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
}

const SAMPLE: Walk = {
  id: 'walk-001',
  workspace: 'ws-x',
  initiated_by: 'operator',
  primary_persona: 'project-manager',
  started_at: '2026-04-27T12:00:00Z',
  status: 'active',
  agenda: [
    {
      id: 'a',
      topic: 'first',
      source: { kind: 'escalation', ref: 'e1' },
      priority: 450,
      status: 'active',
      added_at: '2026-04-27T12:00:00Z',
    },
    {
      id: 'b',
      topic: 'second',
      source: { kind: 'hitl', ref: 's1' },
      priority: 300,
      status: 'queued',
      added_at: '2026-04-27T12:00:00Z',
    },
    {
      id: 'c',
      topic: 'third',
      source: { kind: 'bead_filed', ref: 'gm-1' },
      priority: 100,
      status: 'queued',
      added_at: '2026-04-27T12:00:00Z',
    },
  ],
  cost: { tokens_in: 0, tokens_out: 0, dollars: 0 },
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// Backend simulator: reads the current agenda from the in-memory
// walk and produces the expected next state for each mutation. Lets
// us assert UI behaviour through real API call sequences.
function makeFetchMock(initial: Walk = SAMPLE) {
  let state: Walk = JSON.parse(JSON.stringify(initial));
  const updateItemStatus = (itemID: string, status: string): Walk => {
    state = {
      ...state,
      agenda: state.agenda.map((i) =>
        i.id === itemID ? { ...i, status: status as Walk['agenda'][0]['status'] } : i
      ),
    };
    return state;
  };
  const decide = (itemID: string, kind: string): Walk => {
    const newStatus = kind === 'defer' ? 'deferred' : 'decided';
    state = {
      ...state,
      agenda: state.agenda.map((i) =>
        i.id === itemID ? { ...i, status: newStatus as Walk['agenda'][0]['status'] } : i
      ),
      decisions: [
        ...(state.decisions ?? []),
        {
          agenda_item_id: itemID,
          kind: kind as 'ratify' | 'modify' | 'reject' | 'defer' | 'handoff',
          decided_at: '2026-04-27T12:01:00Z',
          decided_by: 'operator',
        },
      ],
    };
    return state;
  };

  return vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url);
    const method = init?.method ?? 'GET';
    if (u.endsWith('/walks:start') && method === 'POST') {
      return Promise.resolve(jsonResponse(state, 201));
    }
    if (u.includes('/walks/walk-001/agenda/') && method === 'PATCH') {
      const itemID = u.split('/agenda/')[1];
      const body = JSON.parse(init?.body as string) as { status: string };
      return Promise.resolve(jsonResponse(updateItemStatus(itemID, body.status)));
    }
    if (u.includes('/walks/walk-001/decide/') && method === 'POST') {
      const itemID = u.split('/decide/')[1];
      const body = JSON.parse(init?.body as string) as { kind: string };
      return Promise.resolve(jsonResponse(decide(itemID, body.kind)));
    }
    if (u.endsWith('/walks/walk-001/end') && method === 'POST') {
      state = { ...state, status: 'completed', ended_at: '2026-04-27T13:00:00Z' };
      return Promise.resolve(jsonResponse(state));
    }
    if (u.endsWith('/walks/walk-001') && method === 'GET') {
      return Promise.resolve(jsonResponse(state));
    }
    if (u.endsWith('/escalations') && method === 'GET') {
      return Promise.resolve(jsonResponse({ escalations: [], total: 0 }));
    }
    return Promise.resolve(jsonResponse({}, 404));
  });
}

describe('WalkPage', () => {
  let fetchSpy: ReturnType<typeof makeFetchMock>;

  beforeEach(() => {
    window.localStorage.clear();
    fetchSpy = makeFetchMock();
    vi.stubGlobal('fetch', fetchSpy);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    window.localStorage.clear();
  });

  it('renders the three-pane layout + bottom bar', async () => {
    render(wrap(<WalkPage />));
    await waitFor(() => expect(screen.getByTestId('walk-page')).toBeTruthy());
    expect(screen.getByTestId('walk-agenda-pane')).toBeTruthy();
    expect(screen.getByTestId('walk-chat-pane')).toBeTruthy();
    expect(screen.getByTestId('walk-right-pane')).toBeTruthy();
    expect(screen.getByTestId('walk-bottom-bar')).toBeTruthy();
  });

  it('agenda pane shows all four lanes', async () => {
    render(wrap(<WalkPage />));
    await waitFor(() => expect(screen.getByTestId('walk-agenda-lane-queued')).toBeTruthy());
    for (const lane of ['queued', 'active', 'decided', 'deferred']) {
      expect(screen.getByTestId(`walk-agenda-lane-${lane}`)).toBeTruthy();
    }
  });

  it('agenda items render with the source-kind icon and the active card highlights', async () => {
    render(wrap(<WalkPage />));
    await waitFor(() => expect(screen.getByTestId('walk-agenda-item-a')).toBeTruthy());
    const card = screen.getByTestId('walk-agenda-item-a');
    expect(card.dataset.lane).toBe('active');
    expect(screen.getByTestId('walk-agenda-item-a-urgency')).toBeTruthy();
  });

  it('R hotkey ratifies the active item and auto-promotes the next', async () => {
    render(wrap(<WalkPage />));
    await waitFor(() => expect(screen.getByTestId('walk-agenda-item-a')).toBeTruthy());
    expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('active');
    await act(async () => {
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'r', bubbles: true, cancelable: true })
      );
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('decided')
    );
    await waitFor(() =>
      expect(screen.getByTestId('walk-agenda-item-b').dataset.lane).toBe('active')
    );
  });

  it('D hotkey defers the active item', async () => {
    render(wrap(<WalkPage />));
    // Wait for item-a to actually be in the active lane before
    // dispatching `d` — the hotkey only defers the *active* item, so
    // firing it before promotion completes is a no-op (silently
    // produced 'active' instead of 'deferred' in CI when the runner
    // was under load). Mirrors the R-hotkey test pattern above.
    await waitFor(() =>
      expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('active')
    );
    await act(async () => {
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'd', bubbles: true, cancelable: true })
      );
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('deferred')
    );
  });

  it('clicking a queued card promotes it to active', async () => {
    render(wrap(<WalkPage />));
    // findByTestId retries with the testing-library timeout; the
    // sync getByTestId variant raced React's effect schedule on CI
    // (passed locally, intermittent fail on slower runners).
    const card = await screen.findByTestId('walk-agenda-item-b');
    await act(async () => {
      card.click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-agenda-item-b').dataset.lane).toBe('active')
    );
    await waitFor(() =>
      expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('queued')
    );
  });

  it('mounting /walk starts the walk and flips PM panel walkActive', async () => {
    function PmProbe(): JSX.Element {
      const pm = usePmPanel();
      return <div data-testid="pm-walk-active">{pm.walkActive ? 'on' : 'off'}</div>;
    }
    render(
      wrap(
        <>
          <WalkPage />
          <PmProbe />
        </>
      )
    );
    await waitFor(() =>
      expect(screen.getByTestId('pm-walk-active').textContent).toBe('on')
    );
  });

  it('banner reports decision progress as items are decided', async () => {
    render(
      wrap(
        <>
          <ActiveWalkBanner />
          <WalkPage />
        </>
      )
    );
    await waitFor(() => expect(screen.getByTestId('walk-banner')).toBeTruthy());
    expect(screen.getByTestId('walk-banner').textContent).toMatch(/0 of 3/);
    await act(async () => {
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'r', bubbles: true, cancelable: true })
      );
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-banner').textContent).toMatch(/1 of 3/)
    );
  });

  it('walk hotkeys do not fire outside the walk page', async () => {
    function Probe(): JSX.Element {
      const w = useWalk();
      return (
        <div data-testid="probe-active-lane">
          {w.agenda.find((i) => i.status === 'active')?.id ?? 'none'}
        </div>
      );
    }
    // Render Probe + a separate WalkPage to drive the start, then
    // unmount only the page so the walk-scope leaves and the hotkey
    // becomes a no-op on the remaining surface.
    const { rerender } = render(wrap(<><WalkPage /><Probe /></>));
    await waitFor(() =>
      expect(screen.getByTestId('probe-active-lane').textContent).toBe('a')
    );
    rerender(wrap(<Probe />));
    // R should be a no-op outside /walk.
    await act(async () => {
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'r', bubbles: true, cancelable: true })
      );
    });
    expect(screen.getByTestId('probe-active-lane').textContent).toBe('a');
  });
});
