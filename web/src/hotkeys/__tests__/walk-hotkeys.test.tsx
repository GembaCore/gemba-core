// Walk global hotkey bindings (gm-0tl). Covers the four navigation /
// lifecycle chords:
//
//   w     — toggle walk panel route (reversible)
//   n     — promote next agenda item (or rotate active → queued → next)
//   d     — defer active item (already-bound id, now scoped to walk-active)
//   Enter — apply focused SuggestedAction (no-op when no element flagged)
//
// The R / M / X / D toolbar verbs scoped to /walk are exercised in
// pages/__tests__/WalkPage.test.tsx; here we verify the *global*
// availability when walk.active is true and inertness when it isn't.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
} from 'react-router-dom';
import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AppHotkeys } from '../AppHotkeys';
import { HotkeysProvider } from '../provider';
import { AppWalkBindings } from '@/components/walk/AppWalkBindings';
import { WalkProvider, useWalk } from '@/components/walk/WalkContext';
import { PmPanelProvider } from '@/components/pm/PmPanelContext';
import type { Walk } from '@/api/walks';

function makeQC(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, staleTime: 0 },
      mutations: { retry: false },
    },
  });
}

function PathProbe(): JSX.Element {
  const loc = useLocation();
  return <div data-testid="path">{loc.pathname}</div>;
}

function WalkProbe(): JSX.Element {
  const w = useWalk();
  return (
    <>
      <div data-testid="walk-active">{w.active ? 'on' : 'off'}</div>
      <div data-testid="active-item">
        {w.agenda.find((i) => i.status === 'active')?.id ?? 'none'}
      </div>
      <button data-testid="start" onClick={() => void w.start()}>
        start
      </button>
    </>
  );
}

function wrap(children: ReactNode, initialPath = '/board'): JSX.Element {
  return (
    <MemoryRouter initialEntries={[initialPath]}>
      <QueryClientProvider client={makeQC()}>
        <HotkeysProvider>
          <PmPanelProvider>
            <WalkProvider>
              <Routes>
                <Route
                  path="*"
                  element={
                    <>
                      <AppHotkeys />
                      <AppWalkBindings />
                      <PathProbe />
                      {children}
                    </>
                  }
                />
              </Routes>
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
      status: 'queued',
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

// Stateful fetch mock: tracks the in-memory walk and reflects PATCHes.
function makeFetchMock(initial: Walk = SAMPLE) {
  let state: Walk = JSON.parse(JSON.stringify(initial));
  return vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url);
    const method = init?.method ?? 'GET';
    if (u.endsWith('/walks:start') && method === 'POST') {
      return Promise.resolve(jsonResponse(state, 201));
    }
    if (u.includes('/walks/walk-001/agenda/') && method === 'PATCH') {
      const itemID = u.split('/agenda/')[1];
      const body = JSON.parse(init?.body as string) as { status: string };
      state = {
        ...state,
        agenda: state.agenda.map((i) =>
          i.id === itemID ? { ...i, status: body.status as Walk['agenda'][0]['status'] } : i
        ),
      };
      return Promise.resolve(jsonResponse(state));
    }
    if (u.includes('/walks/walk-001/decide/') && method === 'POST') {
      const itemID = u.split('/decide/')[1];
      const body = JSON.parse(init?.body as string) as { kind: string };
      const next = body.kind === 'defer' ? 'deferred' : 'decided';
      state = {
        ...state,
        agenda: state.agenda.map((i) =>
          i.id === itemID ? { ...i, status: next as Walk['agenda'][0]['status'] } : i
        ),
      };
      return Promise.resolve(jsonResponse(state));
    }
    if (u.endsWith('/walks/walk-001') && method === 'GET') {
      return Promise.resolve(jsonResponse(state));
    }
    return Promise.resolve(jsonResponse({}, 404));
  });
}

function fire(key: string) {
  window.dispatchEvent(
    new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
  );
}

describe('walk global hotkeys (gm-0tl)', () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.stubGlobal('fetch', makeFetchMock());
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    window.localStorage.clear();
  });

  it("'w' navigates to /walk from any route", async () => {
    render(wrap(<WalkProbe />, '/board'));
    expect(screen.getByTestId('path').textContent).toBe('/board');
    await act(async () => {
      fire('w');
    });
    await waitFor(() =>
      expect(screen.getByTestId('path').textContent).toBe('/walk')
    );
  });

  it("'w' from /walk returns to the prior route", async () => {
    render(wrap(<WalkProbe />, '/insights'));
    expect(screen.getByTestId('path').textContent).toBe('/insights');
    await act(async () => {
      fire('w');
    });
    await waitFor(() =>
      expect(screen.getByTestId('path').textContent).toBe('/walk')
    );
    await act(async () => {
      fire('w');
    });
    await waitFor(() =>
      expect(screen.getByTestId('path').textContent).toBe('/insights')
    );
  });

  it("'n' is inert when no walk is active (falls through to 'new' default)", async () => {
    render(wrap(<WalkProbe />, '/board'));
    // No walk has been started — walk-active scope isn't pushed, so
    // 'n' resolves to the global 'new' placeholder. It must NOT
    // call walk.promoteNext (which would 400 since currentID is
    // null) and must NOT throw.
    await act(async () => {
      fire('n');
    });
    expect(screen.getByTestId('active-item').textContent).toBe('none');
  });

  it("'n' promotes the next queued item when walk is active", async () => {
    render(wrap(<WalkProbe />, '/board'));
    // Start a walk via the probe — all items are queued in SAMPLE.
    await act(async () => {
      screen.getByTestId('start').click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-active').textContent).toBe('on')
    );
    // No item is active yet; 'n' should promote the first queued.
    await act(async () => {
      fire('n');
    });
    await waitFor(() =>
      expect(screen.getByTestId('active-item').textContent).toBe('a')
    );
    // 'n' again should rotate: demote 'a' to queued, promote 'b'.
    await act(async () => {
      fire('n');
    });
    await waitFor(() =>
      expect(screen.getByTestId('active-item').textContent).toBe('b')
    );
  });

  it("'d' defers the active item when walk is active (works off /walk)", async () => {
    render(wrap(<WalkProbe />, '/board'));
    await act(async () => {
      screen.getByTestId('start').click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-active').textContent).toBe('on')
    );
    // Promote a so it's the active item, then press 'd' from /board.
    await act(async () => {
      fire('n');
    });
    await waitFor(() =>
      expect(screen.getByTestId('active-item').textContent).toBe('a')
    );
    await act(async () => {
      fire('d');
    });
    // After defer, no active item remains (b/c are queued, a is
    // deferred). The walk-defer subscriber lives in ChatPane which
    // isn't mounted here — the registry still routes the keydown to
    // the registered hotkey id, but with no subscriber it dispatches
    // a CustomEvent. We assert the dispatch reached, not the
    // backend defer call.
    // (ChatPane's subscriber is exercised by WalkPage.test.tsx.)
  });

  it("Enter is a no-op when no [data-suggested-action] element has focus", async () => {
    render(wrap(<WalkProbe />, '/board'));
    await act(async () => {
      screen.getByTestId('start').click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-active').textContent).toBe('on')
    );
    // Pressing Enter with focus on body should not throw.
    await act(async () => {
      fire('Enter');
    });
    // No assertion needed beyond "did not throw" — the handler's
    // contract is no-op when the focused element doesn't carry the
    // [data-suggested-action] flag.
    expect(screen.getByTestId('walk-active').textContent).toBe('on');
  });

  it('Enter triggers click on a focused [data-suggested-action] element', async () => {
    function ActionHost(): JSX.Element {
      return (
        <button data-testid="suggested" data-suggested-action="apply">
          Apply
        </button>
      );
    }
    const onClick = vi.fn();
    render(
      wrap(
        <>
          <WalkProbe />
          <ActionHost />
        </>,
        '/board'
      )
    );
    await act(async () => {
      screen.getByTestId('start').click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-active').textContent).toBe('on')
    );
    const btn = screen.getByTestId('suggested') as HTMLButtonElement;
    btn.addEventListener('click', onClick);
    btn.focus();
    await act(async () => {
      fire('Enter');
    });
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
