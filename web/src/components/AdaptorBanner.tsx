import { useEffect } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { observeInstanceId } from '../transport/instanceId';

// AdaptorBanner — gm-b1 / gm-root.7. Subscribes to the server-pushed
// /api/adaptors/stream SSE feed so transitions surface in <1 s of the
// next probe tick. The backend shape is stable (see
// internal/server/adaptors.go); keep this type in sync with
// registry.AdaptorStatus on the Go side.
//
// DoD (gm-b1): killing the bd daemon mid-session produces a clear
// banner. Readonly views keep working — this component MUST NOT block
// rendering of the rest of the app on a failed stream.
//
// gm-6m60: every frame carries instance_id (the per-process boot
// stamp). The banner pipes it through observeInstanceId so a server
// restart with new config triggers a full SPA reload — that's how the
// otherwise-cached capabilities manifest gets refreshed.

type AdaptorStatus = {
  name: string;
  plane: 'work' | 'orchestration';
  healthy: boolean;
  reason?: string;
};

type AdaptorsResponse = {
  instance_id?: string;
  adaptors: AdaptorStatus[];
};

const STREAM_URL = '/api/adaptors/stream';
const SNAPSHOT_URL = '/api/adaptors';

// useAdaptorsStream keeps the react-query cache fed from an
// EventSource. Other parts of the SPA can still do
// useQuery(['adaptors']) to read the current value — they don't own
// the subscription, they just read the cache. The query is kept
// disabled so react-query never runs a queryFn of its own; the SSE
// callback is the single writer.
function useAdaptorsStream(): void {
  const qc = useQueryClient();

  useEffect(() => {
    let closed = false;
    let snapshotFallbackFired = false;
    let es: EventSource | null = null;
    const reconnectTimer: ReturnType<typeof setTimeout> | null = null;

    const connect = () => {
      if (closed) return;
      // Guard against environments without EventSource (older jsdom
      // runs); fall back to a single snapshot fetch so the banner at
      // least renders once.
      if (typeof EventSource === 'undefined') {
        void fetchSnapshot();
        return;
      }
      es = new EventSource(STREAM_URL);
      es.onmessage = (ev) => {
        try {
          const parsed = JSON.parse(ev.data) as AdaptorsResponse;
          observeInstanceId(parsed.instance_id);
          qc.setQueryData<AdaptorsResponse>(['adaptors'], parsed);
        } catch {
          // Ignore malformed frames — the next transition will
          // supersede them. A non-JSON `: keepalive` heartbeat never
          // reaches onmessage (EventSource treats it as a comment).
        }
      };
      es.onerror = () => {
        // First-error fallback: if we never got a frame, pull the
        // snapshot so the banner has *something* to render while the
        // browser auto-reconnects the EventSource. Subsequent errors
        // are no-ops; EventSource retries on its own.
        if (!snapshotFallbackFired && qc.getQueryData(['adaptors']) === undefined) {
          snapshotFallbackFired = true;
          void fetchSnapshot();
        }
      };
    };

    const fetchSnapshot = async () => {
      try {
        const r = await fetch(SNAPSHOT_URL);
        if (!r.ok) return;
        const data = (await r.json()) as AdaptorsResponse;
        if (!closed) {
          observeInstanceId(data.instance_id);
          qc.setQueryData<AdaptorsResponse>(['adaptors'], data);
        }
      } catch {
        // Same philosophy as the original poll — the banner silently
        // stays absent when the health surface itself is unreachable.
      }
    };

    connect();

    return () => {
      closed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      es?.close();
    };
  }, [qc]);
}

export function AdaptorBanner() {
  useAdaptorsStream();

  // useQuery with enabled:false still reads the cache populated by
  // the SSE subscription, giving us the same subscribe-to-updates
  // surface the rest of the SPA already uses.
  const { data } = useQuery<AdaptorsResponse>({
    queryKey: ['adaptors'],
    queryFn: async () => ({ adaptors: [] }),
    enabled: false,
  });

  const degraded = (data?.adaptors ?? []).filter((a) => !a.healthy);
  if (degraded.length === 0) return null;

  return (
    <div
      role="alert"
      className="border-b border-amber-700 bg-amber-950 text-amber-100 px-4 py-2 text-sm"
      data-testid="adaptor-banner"
    >
      <div className="max-w-5xl mx-auto flex flex-wrap gap-x-4 gap-y-1 items-baseline">
        <span className="font-semibold">Adaptor degraded</span>
        {degraded.map((a) => (
          <span key={a.name} className="text-amber-200/90">
            <code className="text-amber-100">{a.name}</code>
            <span className="text-amber-400/70"> ({a.plane})</span>
            {a.reason ? <span>: {a.reason}</span> : null}
          </span>
        ))}
        <span className="ml-auto text-xs text-amber-300/70">
          Readonly views keep working; mutations will be rejected until this clears.
        </span>
      </div>
    </div>
  );
}
