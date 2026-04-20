import { useCapabilities } from '@/capabilities';

// CapabilityBrowser renders the currently-negotiated manifests. It's
// the first real consumer of the capability-negotiation layer and
// doubles as a diagnostic surface: if an operator can't tell why a
// Kanban column is hidden, this is the page that answers.
//
// The view MUST NOT claim to show "gemba's" capabilities — everything
// it renders is adaptor-declared. The page title and copy make that
// explicit so the operator doesn't interpret an empty screen as a
// gemba bug (it's almost always a disconnected adaptor).

export function CapabilityBrowser() {
  const { manifests, status, error, refetch } = useCapabilities();
  const { work, orchestration } = manifests;

  return (
    <div className="p-8">
      <div className="mb-6 flex items-baseline justify-between">
        <div>
          <h1 className="mb-1 text-2xl font-semibold tracking-tight">Capability Browser</h1>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Declared by the active work and orchestration adaptors. Controls across the SPA are
            hidden when a capability is missing, not disabled.
          </p>
        </div>
        <button
          type="button"
          onClick={refetch}
          className="rounded-md border border-neutral-200 bg-white px-2 py-1 text-sm text-neutral-700 hover:bg-neutral-100 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800"
        >
          Reload
        </button>
      </div>

      {status === 'loading' && <div className="text-neutral-500">loading…</div>}
      {status === 'error' && error && (
        <div className="mb-4 rounded-md border border-red-300 bg-red-50 p-3 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200">
          Failed to load capabilities: {error}
        </div>
      )}

      <section className="mb-8">
        <h2 className="mb-2 text-lg font-medium">Work plane</h2>
        {work ? <ManifestBlock data={work} /> : <EmptyPlaneNote plane="work" />}
      </section>

      <section>
        <h2 className="mb-2 text-lg font-medium">Orchestration plane</h2>
        {orchestration ? (
          <ManifestBlock data={orchestration} />
        ) : (
          <EmptyPlaneNote plane="orchestration" />
        )}
      </section>
    </div>
  );
}

function EmptyPlaneNote({ plane }: { plane: 'work' | 'orchestration' }) {
  return (
    <div className="rounded-md border border-neutral-200 bg-neutral-50 p-4 text-sm text-neutral-600 dark:border-neutral-800 dark:bg-neutral-950 dark:text-neutral-400">
      No {plane} adaptor is bound. Every {plane}-specific control is hidden until a manifest is
      declared. Start an adaptor and reload to populate this view.
    </div>
  );
}

function ManifestBlock({ data }: { data: unknown }) {
  return (
    <pre
      className="overflow-auto rounded-md bg-neutral-100 p-4 text-xs text-neutral-800 dark:bg-neutral-900 dark:text-neutral-200"
      data-testid="manifest-json"
    >
      {JSON.stringify(data, null, 2)}
    </pre>
  );
}
