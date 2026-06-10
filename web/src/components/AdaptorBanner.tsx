import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { observeInstanceId } from '../transport/instanceId';
import { getAdaptors } from '@/api/adaptors';
import {
  ADAPTOR_OPERATION_FAILED_EVENT,
  type AdaptorOperationFailedDetail,
} from '@/api/client';

// AdaptorBanner — gm-b1 / gm-root.7. Reactive fault banner for adaptor
// failures. It deliberately does not poll and does not subscribe to the
// adaptor stream: apiFetch emits a local event after an adaptor-shaped
// operation failure, then this component runs exactly one fresh
// /api/adaptors?refresh=1 heartbeat. The banner appears only when that
// heartbeat also reports/fails unhealthy.
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

// useReactiveAdaptorHeartbeat updates the shared ['adaptors'] cache
// only after a real operation failure. Status/Capabilities may read the
// same cache, but this component is the only place that turns failures
// into the global banner.
function useReactiveAdaptorHeartbeat(
  onHeartbeat: (data: AdaptorsResponse) => void
): void {
  const qc = useQueryClient();

  useEffect(() => {
    let closed = false;
    let seq = 0;

    const onFailure = (ev: Event) => {
      const detail = (ev as CustomEvent<AdaptorOperationFailedDetail>).detail;
      const current = ++seq;
      void refreshAfterFailure(detail, current);
    };

    const refreshAfterFailure = async (
      detail: AdaptorOperationFailedDetail | undefined,
      current: number
    ) => {
      try {
        const data = await getAdaptors({ refresh: true });
        if (closed || current !== seq) return;
        observeInstanceId(data.instance_id);
        qc.setQueryData<AdaptorsResponse>(['adaptors'], data);
        onHeartbeat(data);
      } catch (e) {
        if (closed || current !== seq) return;
        const msg = e instanceof Error ? e.message : String(e);
        const op = detail?.code ? ` after ${detail.code}` : '';
        const data: AdaptorsResponse = {
          adaptors: [
            {
              name: 'health check',
              plane: 'work',
              healthy: false,
              reason: `adaptor heartbeat failed${op}: ${msg}`,
            },
          ],
        };
        qc.setQueryData<AdaptorsResponse>(['adaptors'], data);
        onHeartbeat(data);
      }
    };

    window.addEventListener(ADAPTOR_OPERATION_FAILED_EVENT, onFailure);

    return () => {
      closed = true;
      window.removeEventListener(ADAPTOR_OPERATION_FAILED_EVENT, onFailure);
    };
  }, [onHeartbeat, qc]);
}

export function AdaptorBanner() {
  const [data, setData] = useState<AdaptorsResponse | null>(null);
  useReactiveAdaptorHeartbeat(setData);

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
