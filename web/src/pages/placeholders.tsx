import { useQuery } from '@tanstack/react-query';
import type { ReactNode } from 'react';

function Page({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="p-8">
      <h1 className="mb-2 text-2xl font-semibold tracking-tight">{title}</h1>
      <p className="mb-6 text-sm text-neutral-500 dark:text-neutral-400">
        Placeholder view. Real content ships with its own bead.
      </p>
      {children}
    </div>
  );
}

// InsightsPage moved to web/src/pages/InsightsPage.tsx (gm-e12.17.1).
// EscalationsPage moved to web/src/pages/EscalationsPage.tsx (gm-e11.8.1).
export function MailPage() {
  return <Page title="Mail" />;
}

async function fetchJSON<T>(path: string): Promise<T> {
  const r = await fetch(path);
  if (!r.ok) throw new Error(`${path}: ${r.status}`);
  return r.json();
}

export function HealthPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['health'],
    queryFn: () => fetchJSON<{ status: string }>('/api/health'),
  });
  return (
    <Page title="API health">
      {isLoading && <div className="text-neutral-500">loading…</div>}
      {error && (
        <div className="text-red-500">
          {(error as Error).message}
          <p className="mt-2 text-sm text-neutral-500">
            Is <code>gemba serve</code> running? In dev, Vite proxies to :7666 by default.
          </p>
        </div>
      )}
      {data && (
        <pre className="rounded-md bg-neutral-100 p-4 text-neutral-800 dark:bg-neutral-900 dark:text-neutral-200">
          {JSON.stringify(data, null, 2)}
        </pre>
      )}
    </Page>
  );
}

export function NotFoundPage() {
  return (
    <div className="p-8" data-testid="not-found-page">
      <h1 className="mb-2 text-2xl font-semibold tracking-tight">Not found</h1>
      <p className="text-sm text-neutral-500">This route does not exist.</p>
    </div>
  );
}
