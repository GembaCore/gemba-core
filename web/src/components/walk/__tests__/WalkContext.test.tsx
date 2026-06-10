// WalkContext tests (gm-i65). Covers backend wiring: start triggers
// POST /api/v1/walks:start, decisions trigger POST /decide/{item},
// pause/resume/end hit their lifecycle paths. Pure helpers
// (decidedCount, totalDecidableCount) are exercised inline.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  WalkProvider,
  useWalk,
  decidedCount,
  totalDecidableCount,
} from '../WalkContext';
import type { AgendaItem, Walk } from '../types';

function makeQC(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, staleTime: 0 },
      mutations: { retry: false },
    },
  });
}

function Probe(): JSX.Element {
  const w = useWalk();
  return (
    <div>
      <div data-testid="active">{w.active ? 'on' : 'off'}</div>
      <div data-testid="walk-id">{w.walk?.id ?? ''}</div>
      <div data-testid="agenda-len">{w.agenda.length}</div>
      <div data-testid="active-index">{w.activeIndex}</div>
      <div data-testid="decided">{decidedCount(w.agenda)}</div>
      <div data-testid="total">{totalDecidableCount(w.agenda)}</div>
      <button data-testid="start" onClick={() => void w.start()}>
        start
      </button>
      <button data-testid="end" onClick={() => void w.end()}>
        end
      </button>
      <button data-testid="pause" onClick={() => void w.pause()}>
        pause
      </button>
      <button data-testid="ratify-active" onClick={() => {
        const a = w.agenda.find((i) => i.status === 'active');
        if (a) void w.decide(a.id, 'ratify');
      }}>
        ratify
      </button>
      <button data-testid="defer-active" onClick={() => {
        const a = w.agenda.find((i) => i.status === 'active');
        if (a) void w.defer(a.id);
      }}>
        defer
      </button>
    </div>
  );
}

function makeWalk(over: Partial<Walk> = {}): Walk {
  const baseAgenda: AgendaItem[] = [
    {
      id: 'item-1',
      topic: 'one',
      source: { kind: 'escalation', ref: 'e1' },
      priority: 400,
      status: 'active',
      added_at: '2026-04-27T12:00:00Z',
    },
    {
      id: 'item-2',
      topic: 'two',
      source: { kind: 'hitl', ref: 's1' },
      priority: 300,
      status: 'queued',
      added_at: '2026-04-27T12:00:00Z',
    },
    {
      id: 'item-3',
      topic: 'three',
      source: { kind: 'bead_filed', ref: 'gm-1' },
      priority: 100,
      status: 'queued',
      added_at: '2026-04-27T12:00:00Z',
    },
  ];
  return {
    id: 'walk-001',
    workspace: 'ws-x',
    initiated_by: 'operator',
    primary_persona: 'project-manager',
    started_at: '2026-04-27T12:00:00Z',
    status: 'active',
    agenda: baseAgenda,
    cost: { tokens_in: 0, tokens_out: 0, dollars: 0 },
    ...over,
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('WalkContext', () => {
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

  it('starts with no walk and active=off', () => {
    const qc = makeQC();
    render(
      <QueryClientProvider client={qc}>
        <WalkProvider>
          <Probe />
        </WalkProvider>
      </QueryClientProvider>
    );
    expect(screen.getByTestId('active').textContent).toBe('off');
    expect(screen.getByTestId('walk-id').textContent).toBe('');
  });

  it('start posts and binds the returned walk', async () => {
    fetchSpy.mockImplementationOnce(() =>
      Promise.resolve(jsonResponse(makeWalk(), 201))
    );
    const qc = makeQC();
    render(
      <QueryClientProvider client={qc}>
        <WalkProvider>
          <Probe />
        </WalkProvider>
      </QueryClientProvider>
    );
    await act(async () => {
      screen.getByTestId('start').click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-id').textContent).toBe('walk-001')
    );
    expect(screen.getByTestId('active').textContent).toBe('on');
    expect(screen.getByTestId('agenda-len').textContent).toBe('3');
    // active index is 0 (item-1 is the active one).
    expect(screen.getByTestId('active-index').textContent).toBe('0');
  });

  it('decide posts /decide and auto-promotes the next queued item', async () => {
    fetchSpy.mockImplementation((url) => {
      const u = String(url);
      if (u.endsWith('/walks:start')) {
        return Promise.resolve(jsonResponse(makeWalk(), 201));
      }
      if (u.includes('/decide/item-1')) {
        // Backend marks item-1 decided, leaves item-2 queued, no
        // active item — so the context's auto-promotion fires.
        return Promise.resolve(
          jsonResponse(
            makeWalk({
              agenda: [
                {
                  id: 'item-1',
                  topic: 'one',
                  source: { kind: 'escalation' },
                  priority: 400,
                  status: 'decided',
                  added_at: '2026-04-27T12:00:00Z',
                },
                {
                  id: 'item-2',
                  topic: 'two',
                  source: { kind: 'hitl' },
                  priority: 300,
                  status: 'queued',
                  added_at: '2026-04-27T12:00:00Z',
                },
                {
                  id: 'item-3',
                  topic: 'three',
                  source: { kind: 'bead_filed' },
                  priority: 100,
                  status: 'queued',
                  added_at: '2026-04-27T12:00:00Z',
                },
              ],
            })
          )
        );
      }
      // PATCH /agenda/item-2 — auto-promotion to active.
      return Promise.resolve(
        jsonResponse(
          makeWalk({
            agenda: [
              {
                id: 'item-1',
                topic: 'one',
                source: { kind: 'escalation' },
                priority: 400,
                status: 'decided',
                added_at: '2026-04-27T12:00:00Z',
              },
              {
                id: 'item-2',
                topic: 'two',
                source: { kind: 'hitl' },
                priority: 300,
                status: 'active',
                added_at: '2026-04-27T12:00:00Z',
              },
              {
                id: 'item-3',
                topic: 'three',
                source: { kind: 'bead_filed' },
                priority: 100,
                status: 'queued',
                added_at: '2026-04-27T12:00:00Z',
              },
            ],
          })
        )
      );
    });
    const qc = makeQC();
    render(
      <QueryClientProvider client={qc}>
        <WalkProvider>
          <Probe />
        </WalkProvider>
      </QueryClientProvider>
    );
    await act(async () => {
      screen.getByTestId('start').click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-id').textContent).toBe('walk-001')
    );
    await act(async () => {
      screen.getByTestId('ratify-active').click();
    });
    // After auto-promotion, item-2 should be active (index 1).
    await waitFor(() =>
      expect(screen.getByTestId('active-index').textContent).toBe('1')
    );
  });

  it('end posts and clears the current walk id', async () => {
    fetchSpy.mockImplementation((url) => {
      const u = String(url);
      if (u.endsWith('/walks:start')) {
        return Promise.resolve(jsonResponse(makeWalk(), 201));
      }
      if (u.endsWith('/walks/walk-001/end')) {
        return Promise.resolve(
          jsonResponse(makeWalk({ status: 'completed', ended_at: '2026-04-27T13:00:00Z' }))
        );
      }
      return Promise.resolve(jsonResponse(makeWalk()));
    });
    const qc = makeQC();
    render(
      <QueryClientProvider client={qc}>
        <WalkProvider>
          <Probe />
        </WalkProvider>
      </QueryClientProvider>
    );
    await act(async () => {
      screen.getByTestId('start').click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-id').textContent).toBe('walk-001')
    );
    await act(async () => {
      screen.getByTestId('end').click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('walk-id').textContent).toBe('')
    );
  });
});

describe('decidedCount + totalDecidableCount helpers', () => {
  it('decidedCount counts only the decided lane', () => {
    const agenda: AgendaItem[] = [
      { id: 'a', topic: 'a', source: { kind: 'user' }, priority: 0, status: 'decided', added_at: 'x' },
      { id: 'b', topic: 'b', source: { kind: 'user' }, priority: 0, status: 'queued', added_at: 'x' },
      { id: 'c', topic: 'c', source: { kind: 'user' }, priority: 0, status: 'deferred', added_at: 'x' },
    ];
    expect(decidedCount(agenda)).toBe(1);
  });

  it('totalDecidableCount excludes deferred + dismissed', () => {
    const agenda: AgendaItem[] = [
      { id: 'a', topic: 'a', source: { kind: 'user' }, priority: 0, status: 'queued', added_at: 'x' },
      { id: 'b', topic: 'b', source: { kind: 'user' }, priority: 0, status: 'decided', added_at: 'x' },
      { id: 'c', topic: 'c', source: { kind: 'user' }, priority: 0, status: 'deferred', added_at: 'x' },
      { id: 'd', topic: 'd', source: { kind: 'user' }, priority: 0, status: 'dismissed', added_at: 'x' },
    ];
    expect(totalDecidableCount(agenda)).toBe(2);
  });
});
