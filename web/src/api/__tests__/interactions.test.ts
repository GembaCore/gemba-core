import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ensureInteraction } from '../interactions';

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
  });
});

