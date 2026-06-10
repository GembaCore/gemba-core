// EscalationsPage tests (gm-e11.8.1 / gm-e11.8.3 / gm-e11.8.4 / gm-e11.8.5).
//
// Covers:
//   - severity grouping and within-section ordering
//   - resolve flow happy path: open modal, pick kind, confirm,
//     POST body + X-GEMBA-Confirm header
//   - empty state when query returns []
//   - loading skeleton during pending state
//   - originating-link rendering: agent + workitem chips when present,
//     hidden when absent
//
// gm-e11.8.3 additions:
//   - selecting a card shows the toolbar with the right count
//   - clicking Dismiss with N selected calls useRespondEscalation N times
//     with kind: 'defer'
//   - toolbar disappears when selection is cleared
//   - Move-to-walk hidden when no walk active; visible when one is
//   - section-level select-all toggles only that section
//
// gm-e11.8.4 additions:
//   - clicking Hand-off opens the handoff modal
//   - pick persona, confirm → POST /api/consults with the right body
//   - escalation is NOT resolved after hand-off (no respond call)
//   - Cancel closes modal without mutation
//   - success banner shown after confirm; escalation card remains
//
// gm-e11.8.5 additions (filters + search):
//   - Kind filter reduces visible set to matching kinds only
//   - Severity filter reduces visible set to matching severities only
//   - Free-text search matches on title and prompt
//   - Multi-category compose: kind + severity ANDs the filters
//   - Filtered-empty state shown when no items match; Clear filters resets

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { EscalationsPage } from '../EscalationsPage';
import type { EscalationRequest } from '@/api/escalations';
import { RhpProvider } from '@/components/rhp/RhpContext';

// ── Walk context stub ─────────────────────────────────────────────────────────
// We stub the walk context module to control `active` and spy on `addItem`.
// The real WalkProvider requires a QueryClient and a localStorage walk ID, so
// we replace the hook with a controllable stub here.

type WalkStub = { active: boolean; addItem: ReturnType<typeof vi.fn> };
const walkStub: WalkStub = { active: false, addItem: vi.fn() };

vi.mock('@/components/walk/WalkContext', () => ({
  useWalk: () => walkStub,
}));

// ── Helpers ───────────────────────────────────────────────────────────────────

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
        <MemoryRouter>
          <RhpProvider>{children}</RhpProvider>
        </MemoryRouter>
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

// canned roster for the Hand-off modal's persona dropdown
// (gm-e11.8.7). Tests that drive the modal need /api/v1/personas to
// return at least one entry, otherwise the dropdown shows the empty
// state and Confirm stays disabled.
function personasResponse(): Response {
  return jsonResponse({
    personas: [
      {
        id: 'project-manager',
        name: 'Project Manager',
        role: 'PM',
        variety: 'coach',
        scope: { kind: 'project' },
        skills: ['epic_order', 'escalation_handoff'],
      },
    ],
    total: 1,
  });
}

// makeHandoffFetch returns a fetchSpy implementation that resolves
// /api/v1/personas with a canned roster, /api/consults POSTs with a
// canned ConsultSummary, and every other path with the supplied
// escalations envelope. Centralized so the four Hand-off tests stay
// terse.
function makeHandoffFetch(
  escalations: EscalationRequest[],
  consultBody?: Record<string, unknown>
): (url: string, init?: RequestInit) => Promise<Response> {
  return (url, init) => {
    if (String(url).includes('/api/v1/personas')) {
      return Promise.resolve(personasResponse());
    }
    if (init?.method === 'POST' && String(url).includes('/consults')) {
      return Promise.resolve(
        jsonResponse(
          consultBody ?? {
            id: 'consult-1',
            persona_id: 'project-manager',
            skill_id: 'escalation_handoff',
            workspace: 'default',
            status: 'running',
            started_at: new Date().toISOString(),
            line_count: 0,
            line_error_count: 0,
          }
        )
      );
    }
    return Promise.resolve(jsonResponse({ escalations, total: escalations.length }));
  };
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('EscalationsPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
    walkStub.active = false;
    walkStub.addItem.mockReset();
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

    // Section order: critical → high → medium (low absent — no items).
    // Narrowed to <section> elements so the select-toggle buttons
    // (data-testid="escalations-section-{sev}-select-toggle") don't
    // pollute the result.
    const sections = [
      ...document.querySelectorAll('section[data-testid^="escalations-section-"]'),
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

  // ── gm-e11.8.3: multi-select + bulk triage ──────────────────────────────

  it('selecting a card shows the bulk toolbar with the correct count', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-1', title: 'First' }),
      esc({ id: 'esc-2', title: 'Second' }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-1', undefined, { timeout: 3000 });

    // Toolbar absent before any selection.
    expect(screen.queryByTestId('escalations-bulk-toolbar')).toBeNull();

    // Select first card.
    fireEvent.click(screen.getByTestId('escalation-card-esc-1-checkbox'));
    expect(screen.getByTestId('escalations-bulk-toolbar')).toBeTruthy();
    expect(screen.getByTestId('escalations-bulk-count').textContent).toContain('1');

    // Select second card.
    fireEvent.click(screen.getByTestId('escalation-card-esc-2-checkbox'));
    expect(screen.getByTestId('escalations-bulk-count').textContent).toContain('2');
  });

  it('clicking Clear in the toolbar removes the toolbar', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-1', title: 'First' }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-1', undefined, { timeout: 3000 });
    fireEvent.click(screen.getByTestId('escalation-card-esc-1-checkbox'));
    expect(screen.getByTestId('escalations-bulk-toolbar')).toBeTruthy();

    fireEvent.click(screen.getByTestId('escalations-bulk-clear'));
    expect(screen.queryByTestId('escalations-bulk-toolbar')).toBeNull();
  });

  it('bulk Dismiss calls useRespondEscalation with kind=defer for each selected id', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-1', title: 'First' }),
      esc({ id: 'esc-2', title: 'Second' }),
    ];

    // First GET returns the two open escalations; POST /respond returns resolved.
    fetchSpy.mockImplementation((_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        const url = String(_url);
        const id = url.includes('esc-1') ? 'esc-1' : 'esc-2';
        return Promise.resolve(
          jsonResponse({
            id,
            source: 'permission_prompt',
            urgency: 'blocking',
            title: id === 'esc-1' ? 'First' : 'Second',
            prompt: '',
            state: 'resolved',
            created_at: '2026-04-25T12:00:00Z',
          })
        );
      }
      return Promise.resolve(jsonResponse({ escalations, total: escalations.length }));
    });

    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-1', undefined, { timeout: 3000 });

    fireEvent.click(screen.getByTestId('escalation-card-esc-1-checkbox'));
    fireEvent.click(screen.getByTestId('escalation-card-esc-2-checkbox'));
    fireEvent.click(screen.getByTestId('escalations-bulk-dismiss'));

    await waitFor(() => {
      const postCalls = fetchSpy.mock.calls.filter(
        (c) => (c[1] as RequestInit | undefined)?.method === 'POST'
      );
      expect(postCalls.length).toBe(2);
      for (const call of postCalls) {
        const body = JSON.parse(String((call[1] as RequestInit).body));
        expect(body).toEqual({ kind: 'defer' });
      }
    }, { timeout: 5000 });
  });

  it('Move-to-walk button is absent when no walk is active', async () => {
    walkStub.active = false;
    const escalations: EscalationRequest[] = [esc({ id: 'esc-1', title: 'First' })];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-1', undefined, { timeout: 3000 });
    fireEvent.click(screen.getByTestId('escalation-card-esc-1-checkbox'));

    // Button exists but should be disabled when no walk is active.
    const moveBtn = screen.getByTestId('escalations-bulk-move-to-walk');
    expect((moveBtn as HTMLButtonElement).disabled).toBe(true);
  });

  it('Move-to-walk button is enabled when a walk is active', async () => {
    walkStub.active = true;
    const escalations: EscalationRequest[] = [esc({ id: 'esc-1', title: 'First' })];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-1', undefined, { timeout: 3000 });
    fireEvent.click(screen.getByTestId('escalation-card-esc-1-checkbox'));

    const moveBtn = screen.getByTestId('escalations-bulk-move-to-walk');
    expect((moveBtn as HTMLButtonElement).disabled).toBe(false);
  });

  it('section-level select-all toggles only that section', async () => {
    const escalations: EscalationRequest[] = [
      // critical
      esc({ id: 'esc-crit', source: 'permission_prompt', urgency: 'blocking', title: 'Critical' }),
      // high
      esc({ id: 'esc-high', source: 'witness_finding', urgency: 'advisory', title: 'High' }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-crit', undefined, { timeout: 3000 });

    // Click select-all for critical section only.
    fireEvent.click(screen.getByTestId('escalations-section-critical-select-toggle'));

    // Critical card is checked; high card is not.
    expect((screen.getByTestId('escalation-card-esc-crit-checkbox') as HTMLInputElement).checked).toBe(true);
    expect((screen.getByTestId('escalation-card-esc-high-checkbox') as HTMLInputElement).checked).toBe(false);

    // Count = 1 (only critical section).
    expect(screen.getByTestId('escalations-bulk-count').textContent).toContain('1');

    // Toggle again = select-none for that section.
    fireEvent.click(screen.getByTestId('escalations-section-critical-select-toggle'));
    expect((screen.getByTestId('escalation-card-esc-crit-checkbox') as HTMLInputElement).checked).toBe(false);
    expect(screen.queryByTestId('escalations-bulk-toolbar')).toBeNull();
  });

  // ── gm-e11.8.4: per-card Hand-off ─────────────────────────────────────────

  it('hand-off: clicking Hand-off opens the handoff modal', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-H', title: 'Should I proceed?' }),
    ];
    fetchSpy.mockImplementation(makeHandoffFetch(escalations));
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-H-handoff', undefined, { timeout: 3000 });
    fireEvent.click(screen.getByTestId('escalation-card-esc-H-handoff'));

    expect(screen.getByTestId('escalation-handoff-modal')).toBeTruthy();
    // Modal has the escalation title
    expect(screen.getByTestId('escalation-handoff-modal').textContent).toContain('Should I proceed?');
    // Persona picker is rendered (after /api/v1/personas resolves)
    await screen.findByTestId('escalation-handoff-persona', undefined, { timeout: 3000 });
  });

  it('hand-off: empty persona registry shows the empty-state message and disables Confirm', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-empty', title: 'No personas' }),
    ];
    fetchSpy.mockImplementation((url) => {
      if (String(url).includes('/api/v1/personas')) {
        return Promise.resolve(jsonResponse({ personas: [], total: 0 }));
      }
      return Promise.resolve(jsonResponse({ escalations, total: escalations.length }));
    });
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-empty-handoff', undefined, { timeout: 3000 });
    fireEvent.click(screen.getByTestId('escalation-card-esc-empty-handoff'));

    await screen.findByTestId('escalation-handoff-personas-empty', undefined, { timeout: 3000 });
    const confirm = screen.getByTestId('escalation-handoff-confirm') as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
  });

  it('hand-off: pick persona + confirm → POSTs /api/consults with correct body; escalation NOT resolved', async () => {
    const escalations: EscalationRequest[] = [
      esc({
        id: 'esc-HH',
        title: 'Approve risky write',
        prompt: 'The agent wants to delete all logs.',
        assignment_id: 'sess-XY',
      }),
    ];

    fetchSpy.mockImplementation(makeHandoffFetch(escalations));

    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-HH-handoff', undefined, { timeout: 3000 });
    fireEvent.click(screen.getByTestId('escalation-card-esc-HH-handoff'));
    expect(screen.getByTestId('escalation-handoff-modal')).toBeTruthy();
    // Wait for the persona dropdown so personaId has been defaulted
    // before we click Confirm; otherwise the button is disabled.
    await screen.findByTestId('escalation-handoff-persona', undefined, { timeout: 3000 });

    fireEvent.click(screen.getByTestId('escalation-handoff-confirm'));

    await waitFor(() => {
      const consultPost = fetchSpy.mock.calls.find(
        (c) =>
          (c[1] as RequestInit | undefined)?.method === 'POST' &&
          String(c[0]).includes('/consults')
      );
      expect(consultPost).toBeTruthy();
      expect(String(consultPost?.[0])).toMatch(/\/api\/consults$/);
      const body = JSON.parse(String((consultPost?.[1] as RequestInit).body)) as Record<string, unknown>;
      expect(body.persona_id).toBeTruthy();
      expect(body.skill_id).toBe('escalation_handoff');
      expect(body.workspace).toBe('default');
      expect(body.raw_input).toMatchObject({
        title: 'Approve risky write',
        assignment_id: 'sess-XY',
        escalation_id: 'esc-HH',
      });
      const headers = (consultPost?.[1] as RequestInit).headers as Record<string, string>;
      expect(headers['X-GEMBA-Confirm']).toBeTruthy();
    });

    // Escalation is NOT resolved — no POST to /api/escalations/*/respond.
    const respondCalls = fetchSpy.mock.calls.filter(
      (c) =>
        (c[1] as RequestInit | undefined)?.method === 'POST' &&
        String(c[0]).includes('/respond')
    );
    expect(respondCalls).toHaveLength(0);
  });

  it('hand-off: success shows confirmation banner; escalation card remains in DOM', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-S', title: 'Should we proceed?' }),
    ];
    fetchSpy.mockImplementation(
      makeHandoffFetch(escalations, {
        id: 'consult-ok',
        persona_id: 'project-manager',
        skill_id: 'escalation_handoff',
        workspace: 'default',
        status: 'running',
        started_at: new Date().toISOString(),
        line_count: 0,
        line_error_count: 0,
      })
    );

    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-S-handoff', undefined, { timeout: 3000 });
    fireEvent.click(screen.getByTestId('escalation-card-esc-S-handoff'));
    await screen.findByTestId('escalation-handoff-persona', undefined, { timeout: 3000 });
    fireEvent.click(screen.getByTestId('escalation-handoff-confirm'));

    await screen.findByTestId('escalation-handoff-success', undefined, { timeout: 3000 });
    expect(screen.getByTestId('escalation-handoff-success').textContent).toContain('Handed off');

    // Escalation card is still in the DOM (not removed by a resolve).
    expect(screen.getByTestId('escalation-card-esc-S')).toBeTruthy();
  });

  it('hand-off: Cancel closes modal without POSTing', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-C', title: 'Cancel me' }),
    ];
    fetchSpy.mockImplementation(makeHandoffFetch(escalations));
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-C-handoff', undefined, { timeout: 3000 });
    fireEvent.click(screen.getByTestId('escalation-card-esc-C-handoff'));
    expect(screen.getByTestId('escalation-handoff-modal')).toBeTruthy();

    fireEvent.click(screen.getByTestId('escalation-handoff-cancel'));
    expect(screen.queryByTestId('escalation-handoff-modal')).toBeNull();

    // No POST should have been made.
    const postCalls = fetchSpy.mock.calls.filter(
      (c) => (c[1] as RequestInit | undefined)?.method === 'POST'
    );
    expect(postCalls).toHaveLength(0);
  });

  // ── gm-e11.8.5: filters + search ─────────────────────────────────────────

  it('filter: Kind pill reduces visible escalations to matching kind only', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-blocker', source: 'blocker', urgency: 'advisory', title: 'A blocker' }),
      esc({ id: 'esc-question', source: 'question', urgency: 'advisory', title: 'A question' }),
      esc({ id: 'esc-witness', source: 'witness_finding', urgency: 'advisory', title: 'Witness' }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-blocker', undefined, { timeout: 3000 });

    // Open the Kind popover and select 'blocker'.
    fireEvent.click(screen.getByTestId('escalations-filter-kind-trigger'));
    fireEvent.click(screen.getByTestId('escalations-filter-kind-option-blocker'));

    // Only the blocker card should be visible.
    expect(screen.getByTestId('escalation-card-esc-blocker')).toBeTruthy();
    expect(screen.queryByTestId('escalation-card-esc-question')).toBeNull();
    expect(screen.queryByTestId('escalation-card-esc-witness')).toBeNull();
  });

  it('filter: Severity pill reduces visible escalations to matching severity only', async () => {
    const escalations: EscalationRequest[] = [
      // critical: permission_prompt + blocking
      esc({ id: 'esc-crit', source: 'permission_prompt', urgency: 'blocking', title: 'Critical one' }),
      // medium: question + advisory
      esc({ id: 'esc-medium', source: 'question', urgency: 'advisory', title: 'Medium one' }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-crit', undefined, { timeout: 3000 });

    // Open Severity popover and select 'medium'.
    fireEvent.click(screen.getByTestId('escalations-filter-severity-trigger'));
    fireEvent.click(screen.getByTestId('escalations-filter-severity-option-medium'));

    // Only medium card visible.
    expect(screen.getByTestId('escalation-card-esc-medium')).toBeTruthy();
    expect(screen.queryByTestId('escalation-card-esc-crit')).toBeNull();
  });

  it('filter: free-text search matches on title and prompt', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-title-match', source: 'question', urgency: 'advisory', title: 'Approve deployment', prompt: 'unrelated' }),
      esc({ id: 'esc-prompt-match', source: 'question', urgency: 'advisory', title: 'unrelated', prompt: 'Should we deploy now?' }),
      esc({ id: 'esc-no-match', source: 'question', urgency: 'advisory', title: 'Something else', prompt: 'Nothing here' }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-title-match', undefined, { timeout: 3000 });

    // Type search term that appears in both a title and a prompt.
    fireEvent.change(screen.getByTestId('escalations-filter-search'), {
      target: { value: 'deploy' },
    });

    expect(screen.getByTestId('escalation-card-esc-title-match')).toBeTruthy();
    expect(screen.getByTestId('escalation-card-esc-prompt-match')).toBeTruthy();
    expect(screen.queryByTestId('escalation-card-esc-no-match')).toBeNull();
  });

  it('filter: multi-category compose — kind=question AND severity=medium keeps only matching items', async () => {
    const escalations: EscalationRequest[] = [
      // question + advisory → medium
      esc({ id: 'esc-q-med', source: 'question', urgency: 'advisory', title: 'Q medium' }),
      // blocker + advisory → medium (blocker doesn't map to high without blocking urgency)
      // Actually blocker+advisory → low. Use witness_finding for high.
      esc({ id: 'esc-witness-high', source: 'witness_finding', urgency: 'advisory', title: 'Witness high' }),
      // question + blocking → high
      esc({ id: 'esc-q-high', source: 'question', urgency: 'blocking', title: 'Q high' }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-q-med', undefined, { timeout: 3000 });

    // Set kind=question
    fireEvent.click(screen.getByTestId('escalations-filter-kind-trigger'));
    fireEvent.click(screen.getByTestId('escalations-filter-kind-option-question'));

    // Set severity=medium
    fireEvent.click(screen.getByTestId('escalations-filter-severity-trigger'));
    fireEvent.click(screen.getByTestId('escalations-filter-severity-option-medium'));

    // Only esc-q-med should be visible (question AND medium).
    expect(screen.getByTestId('escalation-card-esc-q-med')).toBeTruthy();
    expect(screen.queryByTestId('escalation-card-esc-witness-high')).toBeNull();
    expect(screen.queryByTestId('escalation-card-esc-q-high')).toBeNull();
  });

  it('filter: filtered-empty state shown when no items match; Clear filters restores view', async () => {
    const escalations: EscalationRequest[] = [
      esc({ id: 'esc-q', source: 'question', urgency: 'advisory', title: 'A question' }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations, total: escalations.length }))
    );
    const Wrapper = wrapper();
    render(<Wrapper><EscalationsPage /></Wrapper>);

    await screen.findByTestId('escalation-card-esc-q', undefined, { timeout: 3000 });

    // Set kind=blocker — no blocker in the seed, so everything disappears.
    fireEvent.click(screen.getByTestId('escalations-filter-kind-trigger'));
    fireEvent.click(screen.getByTestId('escalations-filter-kind-option-blocker'));

    expect(screen.getByTestId('escalations-filtered-empty')).toBeTruthy();
    expect(screen.queryByTestId('escalation-card-esc-q')).toBeNull();

    // Clear filters button restores the view.
    fireEvent.click(screen.getByTestId('escalations-filtered-empty-clear'));

    expect(screen.queryByTestId('escalations-filtered-empty')).toBeNull();
    expect(screen.getByTestId('escalation-card-esc-q')).toBeTruthy();
  });
});
