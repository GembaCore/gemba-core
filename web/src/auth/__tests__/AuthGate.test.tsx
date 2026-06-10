import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AuthGate } from '@/auth/AuthGate';

describe('AuthGate', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
    window.history.replaceState(null, '', '/');
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('renders children when health is already authorized', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(new Response('{}', { status: 200 }));

    render(
      <AuthGate>
        <div>app ready</div>
      </AuthGate>
    );

    expect(await screen.findByText('app ready')).toBeTruthy();
  });

  it('exchanges a bearer token for a session cookie before rendering children', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response('{"error":"unauthorized","code":"missing_bearer"}', { status: 401 })
      )
      .mockResolvedValueOnce(new Response('{"status":"ok"}', { status: 200 }));
    globalThis.fetch = fetchMock;

    render(
      <AuthGate>
        <div>app ready</div>
      </AuthGate>
    );

    fireEvent.change(await screen.findByLabelText('Bearer token'), {
      target: { value: 'secret-token' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => expect(screen.getByText('app ready')).toBeTruthy());
    expect(fetchMock).toHaveBeenLastCalledWith('/api/auth/login', {
      method: 'POST',
      headers: {
        Authorization: 'Bearer secret-token',
        Accept: 'application/json',
      },
    });
  });

  it('exchanges a launch URL fragment and removes it from the address bar', async () => {
    window.history.replaceState(null, '', '/#gemba-bootstrap=launch-token');
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response('{"status":"ok"}', { status: 200 }));
    globalThis.fetch = fetchMock;

    render(
      <AuthGate>
        <div>app ready</div>
      </AuthGate>
    );

    expect(await screen.findByText('app ready')).toBeTruthy();
    expect(window.location.hash).toBe('');
    expect(fetchMock).toHaveBeenCalledWith('/api/auth/bootstrap', {
      method: 'POST',
      headers: {
        Authorization: 'Bearer launch-token',
        Accept: 'application/json',
      },
    });
  });
});
