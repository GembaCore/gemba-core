import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ensureInteraction, sendInteractionTurn } from '../interactions';

describe('interactions API client', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('POSTs ensure and converts snake_case wire fields', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          id: 'pm_consult:workitem:gm-1',
          kind: 'pm_consult',
          status: 'waiting_on_operator',
          ui_host: 'rhp',
          runtime_host: 'gastown_crew',
          runtime_label: 'Gas Town crew',
          scope: { type: 'workitem', id: 'gm-1', title: 'Implement converter' },
          messages: [{ id: 'm1', role: 'assistant', body: 'Ready' }],
          quick_replies: [{ id: 'looks-good', label: 'Looks good', message: 'Looks good.' }],
          suggested_actions: [
            {
              id: 'dispatch',
              label: 'Dispatch runtime',
              description: 'Request a crew session.',
              disabled_reason: 'No plane.',
            },
          ],
          capabilities: ['transcript.peek'],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    );

    const result = await ensureInteraction({
      kind: 'pm_consult',
      scope: { type: 'workitem', id: 'gm-1' },
    });

    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/v1/interactions:ensure');
    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit;
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({
      kind: 'pm_consult',
      scope: { type: 'workitem', id: 'gm-1' },
    });
    expect(result.runtimeHost).toBe('gastown_crew');
    expect(result.runtimeLabel).toBe('Gas Town crew');
    expect(result.suggestedActions[0].disabledReason).toBe('No plane.');
    expect(result.quickReplies?.[0].label).toBe('Looks good');
  });

  it('POSTs interactive turns and returns the updated session', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          id: 'pm_consult:bootstrap:001-auth',
          kind: 'pm_consult',
          status: 'waiting_on_operator',
          ui_host: 'rhp',
          runtime_host: 'server_persona',
          runtime_label: 'Server persona',
          scope: { type: 'bootstrap', id: '001-auth', title: 'Login Recovery' },
          messages: [
            { id: 'm1', role: 'operator', body: 'split this' },
            { id: 'm2', role: 'assistant', body: 'Captured as batch-shaping guidance.' },
          ],
          suggested_actions: [],
          capabilities: ['input.send'],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    );

    const result = await sendInteractionTurn({
      id: 'pm_consult:bootstrap:001-auth',
      message: 'split this',
    });

    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/v1/interactions:turn');
    expect(JSON.parse((fetchSpy.mock.calls[0]?.[1] as RequestInit).body as string)).toEqual({
      id: 'pm_consult:bootstrap:001-auth',
      message: 'split this',
    });
    expect(result.messages).toHaveLength(2);
  });
});
