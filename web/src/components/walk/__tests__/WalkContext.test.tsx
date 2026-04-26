// WalkContext tests (gm-uipx.2). Covers lifecycle (start/end/toggle),
// agenda decisions (R/M/X/D + auto-promotion), and localStorage
// persistence.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import {
  WalkProvider,
  useWalk,
  decidedCount,
  totalDecidableCount,
} from '../WalkContext';
import type { AgendaItem } from '../types';

function Probe(): JSX.Element {
  const w = useWalk();
  return (
    <div>
      <div data-testid="active">{w.active ? 'on' : 'off'}</div>
      <div data-testid="agenda-len">{w.agenda.length}</div>
      <div data-testid="active-index">{w.activeIndex}</div>
      <div data-testid="decided">{decidedCount(w.agenda)}</div>
      <div data-testid="total">{totalDecidableCount(w.agenda)}</div>
      <button data-testid="start" onClick={w.start}>start</button>
      <button data-testid="end" onClick={w.end}>end</button>
      <button data-testid="toggle" onClick={w.toggle}>toggle</button>
      {w.agenda.map((i) => (
        <div key={i.id} data-testid={`probe-${i.id}`} data-lane={i.lane}>
          {i.lane}
        </div>
      ))}
      <button
        data-testid="ratify-active"
        onClick={() => {
          const a = w.agenda.find((i) => i.lane === 'active');
          if (a) w.decide(a.id, 'ratify');
        }}
      >
        ratify
      </button>
      <button
        data-testid="defer-active"
        onClick={() => {
          const a = w.agenda.find((i) => i.lane === 'active');
          if (a) w.defer(a.id);
        }}
      >
        defer
      </button>
      <button
        data-testid="set-active-2"
        onClick={() => w.setActiveItem('item-2')}
      >
        set active 2
      </button>
    </div>
  );
}

const SAMPLE: AgendaItem[] = [
  { id: 'item-1', source: 'escalation', title: 'one', lane: 'active' },
  { id: 'item-2', source: 'hitl', title: 'two', lane: 'queued' },
  { id: 'item-3', source: 'filed_bead', title: 'three', lane: 'queued' },
];

describe('WalkContext', () => {
  beforeEach(() => window.localStorage.clear());
  afterEach(() => window.localStorage.clear());

  it('starts inactive by default', () => {
    render(
      <WalkProvider>
        <Probe />
      </WalkProvider>
    );
    expect(screen.getByTestId('active').textContent).toBe('off');
  });

  it('start / end / toggle switch the active flag', () => {
    render(
      <WalkProvider>
        <Probe />
      </WalkProvider>
    );
    act(() => screen.getByTestId('start').click());
    expect(screen.getByTestId('active').textContent).toBe('on');
    act(() => screen.getByTestId('end').click());
    expect(screen.getByTestId('active').textContent).toBe('off');
    act(() => screen.getByTestId('toggle').click());
    expect(screen.getByTestId('active').textContent).toBe('on');
  });

  it('persists active state to localStorage across remounts', () => {
    const { unmount } = render(
      <WalkProvider initialActive>
        <Probe />
      </WalkProvider>
    );
    unmount();
    expect(window.localStorage.getItem('gemba.walk.active')).toBe('true');
    render(
      <WalkProvider>
        <Probe />
      </WalkProvider>
    );
    expect(screen.getByTestId('active').textContent).toBe('on');
  });

  it('decide(ratify) marks item decided and auto-promotes the next queued item', () => {
    render(
      <WalkProvider initialAgenda={SAMPLE}>
        <Probe />
      </WalkProvider>
    );
    expect(screen.getByTestId('probe-item-1').dataset.lane).toBe('active');
    act(() => screen.getByTestId('ratify-active').click());
    expect(screen.getByTestId('probe-item-1').dataset.lane).toBe('decided');
    // item-2 was the first queued — should now be active.
    expect(screen.getByTestId('probe-item-2').dataset.lane).toBe('active');
  });

  it('defer moves item to deferred lane and auto-promotes next', () => {
    render(
      <WalkProvider initialAgenda={SAMPLE}>
        <Probe />
      </WalkProvider>
    );
    act(() => screen.getByTestId('defer-active').click());
    expect(screen.getByTestId('probe-item-1').dataset.lane).toBe('deferred');
    expect(screen.getByTestId('probe-item-2').dataset.lane).toBe('active');
  });

  it('setActiveItem demotes the previous active to queued', () => {
    render(
      <WalkProvider initialAgenda={SAMPLE}>
        <Probe />
      </WalkProvider>
    );
    act(() => screen.getByTestId('set-active-2').click());
    expect(screen.getByTestId('probe-item-1').dataset.lane).toBe('queued');
    expect(screen.getByTestId('probe-item-2').dataset.lane).toBe('active');
  });

  it('decided count excludes deferred items from the denominator', () => {
    render(
      <WalkProvider initialAgenda={SAMPLE}>
        <Probe />
      </WalkProvider>
    );
    // 3 items, all non-deferred → total = 3.
    expect(screen.getByTestId('total').textContent).toBe('3');
    act(() => screen.getByTestId('defer-active').click());
    // 1 deferred → total = 2 remaining decidable.
    expect(screen.getByTestId('total').textContent).toBe('2');
    expect(screen.getByTestId('decided').textContent).toBe('0');
    // Ratify the now-active item-2.
    act(() => screen.getByTestId('ratify-active').click());
    expect(screen.getByTestId('decided').textContent).toBe('1');
  });

  it('persists agenda to localStorage so a reload restores progress', () => {
    const { unmount } = render(
      <WalkProvider initialAgenda={SAMPLE}>
        <Probe />
      </WalkProvider>
    );
    act(() => screen.getByTestId('ratify-active').click());
    unmount();
    const stored = window.localStorage.getItem('gemba.walk.agenda');
    expect(stored).toBeTruthy();
    const parsed = JSON.parse(stored ?? '[]') as AgendaItem[];
    const decided = parsed.find((i) => i.id === 'item-1');
    expect(decided?.lane).toBe('decided');
    expect(decided?.decision?.kind).toBe('ratify');
  });
});
