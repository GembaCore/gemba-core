// EscalationsPage tests (gm-e11.8.1).
//
// Covers:
//   - severity grouping and within-section ordering
//   - resolve flow happy path: open modal, pick kind, confirm,
//     POST body + X-GEMBA-Confirm header
//   - empty state when query returns []
//   - loading skeleton during pending state
//   - originating-link rendering: agent + workitem chips when present,
//     hidden when absent

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { EscalationsPage } from '../EscalationsPage';
import type { EscalationRequest } from '@/api/escalations';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function wrapper(): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <MemoryRouter>{children}</MemoryRouter>
      </QueryClientProvider>
    );
  };
}

function esc(over: Partial<EscalationRequest>): EscalationRequest {
  return {
    id: 'esc-x',
    source: 'permission_prompt',
    urgency: 'blocking',
    title: 'default title',
    prompt: 'default prompt',
    state: 'open',
    created_at: '2026-04-25T12:00:00Z',
    ...over,
  };
}

describe('EscalationsPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders empty state when the API returns []', async () => {
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations: [], total: 0 }))
    );
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <EscalationsPage />
      </Wrapper>
    );
    expect(
      await screen.findByTestId('escalations-empty', undefined, { timeout: 3000 })
    ).toBeTruthy();
  });

  it('shows the loading skeleton while the initial fetch is pending', () => {
    // Never-resolving promise so the query stays in pending state.
    fetchSpy.mockImplementation(() => new Promise(() => {}));
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <EscalationsPage />
      </Wrapper>
    );
    expect(screen.getByTestId('escalations-loading')).toBeTruthy();
  });

  it('groups by severity in critical→high→medium→low order, newest first within each section', async () => {
    const escalations: EscalationRequest[] = [
      // critical (beads_degraded)
      esc({
        id: 'esc-crit-1',
        source: 'beads_degraded',
        urgency: 'advisory',
        title: 'Beads adaptor degraded',
        created_at: '2026-04-25T10:00:00Z',
      }),
      // critical (permission_prompt + blocking) — newer than esc-crit-1
      esc({
        id: 'esc-crit-2',
        source: 'permission_prompt',
        urgency: 'blocking',
        title: 'Approve write?',
        created_at: '2026-04-25T11:00:00Z',
      }),
      // high (witness_finding regardless of urgency)
      esc({
        id: 'esc-high-1',
        source: 'witness_finding',
        urgency: 'advisory',
        title: 'Witness disagrees',
        created_at: '2026-04-25T09:00:00Z',
      }),
      // medium (advisory non-witness/refinery)
      esc({
        id: 'esc-med-1',
        source: 'question',
        urgency: 'advisory',
        title: 'Just curious',
        created_at: '2026-04-25T08:00:00Z',
      }),
      // resolved — must NOT appear
      esc({
        id: 'esc-resolved',
        source: 'permission_prompt',
        urgency: 'blocking',
        state: 'resolved',
        title: 'Already done',
        created_at: '2026-04-25T07:00:00Z',
      }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <EscalationsPage />
      </Wrapper>
    );

    // Wait for first card to appear before assertions.
    await screen.findByTestId('escalation-card-esc-crit-2', undefined, { timeout: 3000 });

    // Resolved escalations are excluded.
    expect(screen.queryByTestId('escalation-card-esc-resolved')).toBeNull();

    // Section order: critical → high → medium (low absent — no items)
    const sections = [
      ...document.querySelectorAll('[data-testid^="escalations-section-"]'),
    ] as HTMLElement[];
    const sectionIds = sections.map((s) => s.getAttribute('data-testid'));
    expect(sectionIds).toEqual([
      'escalations-section-critical',
      'escalations-section-high',
      'escalations-section-medium',
    ]);

    // Within critical: newer (esc-crit-2 @ 11:00) BEFORE older (esc-crit-1 @ 10:00).
    // Query just the top-level card containers (ids of the form
    // "escalation-card-<id>", not the nested sub-testids inside them).
    const criticalSection = screen.getByTestId('escalations-section-critical');
    const cardIds = [...criticalSection.querySelectorAll('[data-kind]')].map(
      (el) => el.getAttribute('data-testid')
    );
    expect(cardIds).toEqual(['escalation-card-esc-crit-2', 'escalation-card-esc-crit-1']);
  });

  it('renders agent + workitem chips when present, hides them when absent', async () => {
    const escalations: EscalationRequest[] = [
      esc({
        id: 'esc-with-links',
        assignment_id: 'sess-A',
        agent_id: 'gemba/polecats/obsidian',
        work_item_id: 'gm-99',
        title: 'With links',
      }),
      esc({
        id: 'esc-no-links',
        title: 'No links',
        // no assignment_id / no work_item_id
      }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <EscalationsPage />
      </Wrapper>
    );

    const linked = await screen.findByTestId('escalation-card-esc-with-links');
    const agent = within(linked).getByTestId('escalation-card-esc-with-links-agent');
    expect(agent.getAttribute('href')).toBe('/sessions/sess-A');
    const wi = within(linked).getByTestId('escalation-card-esc-with-links-workitem');
    expect(wi.getAttribute('href')).toBe('/board?bead=gm-99');

    const bare = screen.getByTestId('escalation-card-esc-no-links');
    expect(within(bare).queryByTestId('escalation-card-esc-no-links-agent')).toBeNull();
    expect(within(bare).queryByTestId('escalation-card-esc-no-links-workitem')).toBeNull();
  });

  it('resolve flow: open modal, pick deny, confirm → POSTs respond with kind=deny + nonce', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-A', title: 'Approve write?' }),
    ];

    fetchSpy.mockImplementation((_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return Promise.resolve(
          jsonResponse({
            id: 'esc-A',
            source: 'permission_prompt',
            urgency: 'blocking',
            title: 'Approve write?',
            prompt: '',
            state: 'resolved',
            created_at: '2026-04-25T12:00:00Z',
          })
        );
      }
      return Promise.resolve(jsonResponse({ escalations, total: escalations.length }));
    });

    const Wrapper = wrapper();
    render(
      <Wrapper>
        <EscalationsPage />
      </Wrapper>
    );

    fireEvent.click(
      await screen.findByTestId('escalation-card-esc-A-resolve', undefined, { timeout: 3000 })
    );
    expect(screen.getByTestId('escalation-resolve-modal')).toBeTruthy();

    // Pick deny
    fireEvent.click(within(screen.getByTestId('escalation-resolve-option-deny')).getByRole('radio'));
    fireEvent.click(screen.getByTestId('escalation-resolve-confirm'));

    await waitFor(() => {
      const postCall = fetchSpy.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === 'POST'
      );
      expect(postCall).toBeTruthy();
      expect(String(postCall?.[0])).toContain('/api/escalations/esc-A/respond');
      const init = postCall?.[1] as RequestInit;
      expect(JSON.parse(String(init.body))).toEqual({ kind: 'deny' });
      const headers = init.headers as Record<string, string>;
      expect(headers['X-GEMBA-Confirm']).toBeTruthy();
    });
  });

  it('resolve flow: modify requires a value → confirm POSTs kind=modify with the typed value', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-M', title: 'Pick a number' }),
    ];

    fetchSpy.mockImplementation((_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return Promise.resolve(
          jsonResponse({
            id: 'esc-M',
            source: 'permission_prompt',
            urgency: 'blocking',
            title: 'Pick a number',
            prompt: '',
            state: 'resolved',
            created_at: '2026-04-25T12:00:00Z',
          })
        );
      }
      return Promise.resolve(jsonResponse({ escalations, total: escalations.length }));
    });

    const Wrapper = wrapper();
    render(
      <Wrapper>
        <EscalationsPage />
      </Wrapper>
    );

    fireEvent.click(
      await screen.findByTestId('escalation-card-esc-M-resolve', undefined, { timeout: 3000 })
    );
    fireEvent.click(within(screen.getByTestId('escalation-resolve-option-modify')).getByRole('radio'));

    // Confirm should be disabled while value is empty.
    expect(
      (screen.getByTestId('escalation-resolve-confirm') as HTMLButtonElement).disabled
    ).toBe(true);

    fireEvent.change(screen.getByTestId('escalation-resolve-modify-value'), {
      target: { value: '42' },
    });
    fireEvent.click(screen.getByTestId('escalation-resolve-confirm'));

    await waitFor(() => {
      const postCall = fetchSpy.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === 'POST'
      );
      expect(postCall).toBeTruthy();
      const init = postCall?.[1] as RequestInit;
      expect(JSON.parse(String(init.body))).toEqual({ kind: 'modify', value: '42' });
    });
  });
});
