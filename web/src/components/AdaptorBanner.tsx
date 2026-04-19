import { useQuery } from '@tanstack/react-query';

// AdaptorBanner — gm-b1. Polls /api/adaptors and surfaces a banner when
// any adaptor is reporting degraded. The backend shape is stable (see
// internal/server/adaptors.go); keep this type in sync with
// registry.AdaptorStatus on the Go side.
//
// DoD (gm-b1): killing the bd daemon mid-session produces a clear
// banner. Readonly views keep working — this component MUST NOT block
// rendering of the rest of the app on a failed poll.

type AdaptorStatus = {
  name: string;
  plane: 'work' | 'orchestration';
  healthy: boolean;
  reason?: string;
};

type AdaptorsResponse = {
  adaptors: AdaptorStatus[];
};

// Poll cadence is a compromise: fast enough that a daemon death surfaces
// within a few seconds, slow enough that we don't hammer a degraded
// backend. 5s matches the typical Dolt probe timeout budget.
const POLL_MS = 5_000;

async function fetchAdaptors(): Promise<AdaptorsResponse> {
  const r = await fetch('/api/adaptors');
  if (!r.ok) {
    // Deliberately fall through to an empty result — the banner should
    // not add noise when the health endpoint itself is reachable but
    // unexpected. The bigger outage (whole backend down) will be
    // communicated by whatever route is currently rendering.
    throw new Error(`/api/adaptors: ${r.status}`);
  }
  return r.json();
}

export function AdaptorBanner() {
  const { data } = useQuery({
    queryKey: ['adaptors'],
    queryFn: fetchAdaptors,
    refetchInterval: POLL_MS,
    // Keep polling even when the tab is in the background — a silent
    // banner on return is worse than a noisy one.
    refetchIntervalInBackground: true,
    staleTime: 0,
  });

  const degraded = (data?.adaptors ?? []).filter((a) => !a.healthy);
  if (degraded.length === 0) return null;

  return (
    <div
      role="alert"
      className="border-b border-amber-700 bg-amber-950/60 text-amber-100 px-4 py-2 text-sm"
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
