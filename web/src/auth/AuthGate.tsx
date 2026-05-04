import { useEffect, useState, type FormEvent, type ReactNode } from 'react';

type AuthState = 'checking' | 'ready' | 'needs-token' | 'submitting' | 'error';

export function AuthGate({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>('checking');
  const [token, setToken] = useState('');
  const [message, setMessage] = useState('');

  useEffect(() => {
    let cancelled = false;
    void checkSession().then((next) => {
      if (!cancelled) setState(next);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const trimmed = token.trim();
    if (!trimmed) {
      setMessage('Paste the token printed in the Gemba server logs.');
      return;
    }
    setState('submitting');
    setMessage('');
    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${trimmed}`,
          Accept: 'application/json',
        },
      });
      if (!res.ok) {
        setMessage('That token was not accepted. Check the server logs and try again.');
        setState('needs-token');
        return;
      }
      setToken('');
      setState('ready');
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
      setState('error');
    }
  }

  if (state === 'ready') return <>{children}</>;

  if (state === 'checking') {
    return (
      <FullScreenShell>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">Connecting to Gemba...</p>
      </FullScreenShell>
    );
  }

  return (
    <FullScreenShell>
      <form
        onSubmit={submit}
        className="w-full max-w-sm rounded-lg border border-neutral-200 bg-white p-5 shadow-sm dark:border-neutral-800 dark:bg-neutral-950"
      >
        <div className="mb-4">
          <h1 className="text-base font-semibold text-neutral-950 dark:text-neutral-50">
            Unlock Gemba
          </h1>
          <p className="mt-1 text-sm text-neutral-500 dark:text-neutral-400">
            Paste the bearer token printed by the server when this container started.
          </p>
        </div>
        <label className="block text-xs font-medium text-neutral-600 dark:text-neutral-300">
          Bearer token
          <input
            value={token}
            onChange={(e) => setToken(e.currentTarget.value)}
            type="password"
            autoFocus
            autoComplete="off"
            spellCheck={false}
            className="mt-1 w-full rounded-md border border-neutral-300 bg-white px-3 py-2 font-mono text-sm text-neutral-950 outline-none focus:border-sky-500 focus:ring-2 focus:ring-sky-200 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-50 dark:focus:border-sky-400 dark:focus:ring-sky-950"
          />
        </label>
        {message && (
          <p className="mt-3 text-sm text-red-600 dark:text-red-400" role="alert">
            {message}
          </p>
        )}
        <button
          type="submit"
          disabled={state === 'submitting'}
          className="mt-4 w-full rounded-md bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-sky-500 dark:hover:bg-sky-400"
        >
          {state === 'submitting' ? 'Checking...' : 'Continue'}
        </button>
      </form>
    </FullScreenShell>
  );
}

async function checkSession(): Promise<AuthState> {
  try {
    const res = await fetch('/api/health', { headers: { Accept: 'application/json' } });
    if (res.ok) return 'ready';
    if (res.status === 401) return 'needs-token';
    return 'error';
  } catch {
    return 'error';
  }
}

function FullScreenShell({ children }: { children: ReactNode }) {
  return (
    <main className="flex min-h-screen items-center justify-center bg-neutral-50 p-6 dark:bg-neutral-950">
      {children}
    </main>
  );
}
