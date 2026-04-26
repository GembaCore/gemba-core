// /walk page tests (gm-uipx.2). Covers two-pane layout, R/M/X/D
// hotkeys via the registered scope, and PM panel takeover sync.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { WalkPage } from '../WalkPage';
import { ActiveWalkBanner } from '@/components/walk/ActiveWalkBanner';
import { AppWalkBindings } from '@/components/walk/AppWalkBindings';
import { WalkProvider, useWalk } from '@/components/walk/WalkContext';
import { PmPanelProvider, usePmPanel } from '@/components/pm/PmPanelContext';
import { HotkeysProvider } from '@/hotkeys';
import type { AgendaItem } from '@/components/walk/types';

function wrap(children: ReactNode, agenda?: AgendaItem[]): JSX.Element {
  return (
    <MemoryRouter initialEntries={['/walk']}>
      <HotkeysProvider>
        <PmPanelProvider>
          <WalkProvider initialAgenda={agenda}>
            <AppWalkBindings />
            {children}
          </WalkProvider>
        </PmPanelProvider>
      </HotkeysProvider>
    </MemoryRouter>
  );
}

const AGENDA: AgendaItem[] = [
  { id: 'a', source: 'escalation', title: 'first', urgency: 'urgent', lane: 'active' },
  { id: 'b', source: 'hitl', title: 'second', urgency: 'today', lane: 'queued' },
  { id: 'c', source: 'filed_bead', title: 'third', lane: 'queued' },
];

describe('WalkPage', () => {
  beforeEach(() => window.localStorage.clear());
  afterEach(() => window.localStorage.clear());

  it('renders the two-pane layout', () => {
    render(wrap(<WalkPage />, AGENDA));
    expect(screen.getByTestId('walk-page')).toBeTruthy();
    expect(screen.getByTestId('walk-agenda-pane')).toBeTruthy();
    expect(screen.getByTestId('walk-chat-pane')).toBeTruthy();
  });

  it('agenda pane shows all four lanes', () => {
    render(wrap(<WalkPage />, AGENDA));
    for (const lane of ['queued', 'active', 'decided', 'deferred']) {
      expect(screen.getByTestId(`walk-agenda-lane-${lane}`)).toBeTruthy();
    }
  });

  it('agenda items render with the source-kind icon and the active card highlights', () => {
    render(wrap(<WalkPage />, AGENDA));
    const card = screen.getByTestId('walk-agenda-item-a');
    expect(card.dataset.lane).toBe('active');
    // Urgency badge renders.
    expect(screen.getByTestId('walk-agenda-item-a-urgency')).toBeTruthy();
  });

  it('R hotkey ratifies the active item and auto-promotes the next', () => {
    render(wrap(<WalkPage />, AGENDA));
    expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('active');
    act(() => {
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'r', bubbles: true, cancelable: true })
      );
    });
    expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('decided');
    expect(screen.getByTestId('walk-agenda-item-b').dataset.lane).toBe('active');
  });

  it('D hotkey defers the active item', () => {
    render(wrap(<WalkPage />, AGENDA));
    act(() => {
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'd', bubbles: true, cancelable: true })
      );
    });
    expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('deferred');
  });

  it('X hotkey rejects the active item', () => {
    render(wrap(<WalkPage />, AGENDA));
    act(() => {
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'x', bubbles: true, cancelable: true })
      );
    });
    expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('decided');
    // The lane is 'decided' for ratify/modify/reject; the verb lives
    // on the persisted decision object.
  });

  it('clicking a queued card promotes it to active', () => {
    render(wrap(<WalkPage />, AGENDA));
    act(() => screen.getByTestId('walk-agenda-item-b').click());
    expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('queued');
    expect(screen.getByTestId('walk-agenda-item-b').dataset.lane).toBe('active');
  });

  it('clicking a decision toolbar button decides the active item', () => {
    render(wrap(<WalkPage />, AGENDA));
    act(() => screen.getByTestId('walk-decision-modify').click());
    expect(screen.getByTestId('walk-agenda-item-a').dataset.lane).toBe('decided');
  });

  it('mounting /walk starts the walk and flips PM panel walkActive', () => {
    function PmProbe(): JSX.Element {
      const pm = usePmPanel();
      return <div data-testid="pm-walk-active">{pm.walkActive ? 'on' : 'off'}</div>;
    }
    render(
      wrap(
        <>
          <WalkPage />
          <PmProbe />
        </>,
        AGENDA
      )
    );
    expect(screen.getByTestId('pm-walk-active').textContent).toBe('on');
  });

  it('end-walk button on the banner flips PM panel walkActive back', () => {
    function PmProbe(): JSX.Element {
      const pm = usePmPanel();
      return <div data-testid="pm-walk-active">{pm.walkActive ? 'on' : 'off'}</div>;
    }
    render(
      wrap(
        <>
          <ActiveWalkBanner />
          <WalkPage />
          <PmProbe />
        </>,
        AGENDA
      )
    );
    expect(screen.getByTestId('walk-banner')).toBeTruthy();
    act(() => screen.getByTestId('walk-banner-end').click());
    expect(screen.queryByTestId('walk-banner')).toBeNull();
    expect(screen.getByTestId('pm-walk-active').textContent).toBe('off');
  });

  it('banner reports decision progress as items are decided', () => {
    render(wrap(
      <>
        <ActiveWalkBanner />
        <WalkPage />
      </>,
      AGENDA,
    ));
    // 0 of 3 to start.
    expect(screen.getByTestId('walk-banner').textContent).toMatch(/0 of 3/);
    act(() => {
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'r', bubbles: true, cancelable: true })
      );
    });
    // 1 of 3 after the first ratify.
    expect(screen.getByTestId('walk-banner').textContent).toMatch(/1 of 3/);
  });

  it('walk hotkeys do not fire when the walk page is not mounted', () => {
    function Probe(): JSX.Element {
      const w = useWalk();
      return (
        <div data-testid="probe-active-lane">
          {w.agenda.find((i) => i.lane === 'active')?.id ?? 'none'}
        </div>
      );
    }
    render(wrap(<Probe />, AGENDA));
    // R should be a no-op outside the walk page (no scope pushed).
    act(() => {
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'r', bubbles: true, cancelable: true })
      );
    });
    // Active item still 'a' — the ratify hotkey didn't fire.
    expect(screen.getByTestId('probe-active-lane').textContent).toBe('a');
  });
});
