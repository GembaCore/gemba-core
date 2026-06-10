// EscalationPanel tests (gm-native.16). Covers:
//   - renders open escalations from /api/escalations?session_id=…
//   - Approve fires POST /api/escalations/<id>/respond with kind=approve
//   - Custom flow sends kind=modify with the operator's value

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { EscalationPanel } from '../EscalationPanel';

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
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

const escFixture = {
  id: 'esc-1',
  source: 'permission_prompt',
  urgency: 'blocking',
  state: 'open',
  title: 'Bash command',
  prompt: 'May I run rm -rf node_modules?',
  assignment_id: 'gm-foo',
  created_at: '2026-04-24T12:00:00Z',
};

describe('EscalationPanel', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders the prompt text and Approve / Deny / Custom buttons', async () => {
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ escalations: [escFixture], total: 1 }))
    );
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <EscalationPanel sessionId="s-1" open={true} onClose={() => {}} />
      </Wrapper>
    );
    expect(
      await screen.findByText(/rm -rf node_modules/, undefined, { timeout: 3000 })
    ).toBeTruthy();
    expect(screen.getByRole('button', { name: /Approve/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Deny/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Custom/ })).toBeTruthy();
  });

  it('Approve fires POST /api/escalations/<id>/respond with kind=approve', async () => {
    fetchSpy.mockImplementation((_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return Promise.resolve(jsonResponse({ ...escFixture, state: 'resolved' }));
      }
      return Promise.resolve(jsonResponse({ escalations: [escFixture], total: 1 }));
    });
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <EscalationPanel sessionId="s-1" open={true} onClose={() => {}} />
      </Wrapper>
    );
    await screen.findByRole('button', { name: /Approve/ }, { timeout: 3000 });
    fireEvent.click(screen.getByRole('button', { name: /Approve/ }));
    await waitFor(() => {
      const post = fetchSpy.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === 'POST'
      );
      expect(post).toBeTruthy();
      expect(String(post?.[0])).toBe('/api/escalations/esc-1/respond');
      const body = JSON.parse((post?.[1] as RequestInit).body as string);
      expect(body).toEqual({ kind: 'approve' });
    });
  });

  it('Custom flow opens textarea, then submits kind=modify with the value', async () => {
    fetchSpy.mockImplementation((_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return Promise.resolve(jsonResponse({ ...escFixture, state: 'resolved' }));
      }
      return Promise.resolve(jsonResponse({ escalations: [escFixture], total: 1 }));
    });
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <EscalationPanel sessionId="s-1" open={true} onClose={() => {}} />
      </Wrapper>
    );
    await screen.findByRole('button', { name: /Custom/ }, { timeout: 3000 });
    fireEvent.click(screen.getByRole('button', { name: /Custom/ }));
    const textarea = await screen.findByPlaceholderText(/Custom reply/);
    fireEvent.change(textarea, { target: { value: 'use --dry-run first' } });
    fireEvent.click(screen.getByRole('button', { name: /Send reply/ }));
    await waitFor(() => {
      const post = fetchSpy.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === 'POST'
      );
      expect(post).toBeTruthy();
      const body = JSON.parse((post?.[1] as RequestInit).body as string);
      expect(body).toEqual({ kind: 'modify', value: 'use --dry-run first' });
    });
  });
});
