// testing/acceptance/temperature-spa/shared/helpers/escalation.ts
//
// gm-root.27.5 — synthetic escalation injector for the triage step.
//
// The triage step (gm-root.27.11) needs to exercise the
// escalation-respond UI between M2 and M3 regardless of agent mode.
// MockAgentRunner doesn't naturally produce escalations; real-claude
// might, but we don't want test outcomes to depend on chance LLM
// requests for permission. So we synthesize one.
//
// CURRENT BACKEND REALITY (2026-04-30): there is no public
// `POST /api/escalations` create endpoint. Escalations are minted
// inside the OrchestrationPlane adaptor when sessions emit
// `escalation.opened` events. To inject one for test purposes we
// have three options:
//
//   1. Backend test-mode endpoint (preferred long-term, not yet built).
//   2. Drive the orchestration plane to emit an escalation event
//      directly. Possible only with a fake/test orchestration plane,
//      which doesn't exist for the gemba-server REST surface today.
//   3. The injector is a no-op until (1) lands; the triage step
//      asserts the absence and files a "backend-incomplete" bug bead
//      so the test doesn't silently pass when triage is unexercised.
//
// We ship option 3 today. The TypeScript surface is the contract the
// future test-mode endpoint will conform to so the triage step
// (gm-root.27.11) can be written against this signature today and
// "just work" once the backend lands. A follow-up bead is filed
// alongside this commit to track the backend work.
//
// References:
//   - D15 docs/design/acceptance-temperature-spa.md §11.6 (escalation
//     respond UI completeness — same kind of "partial today" caveat)
//   - internal/server/escalations.go (the GET + POST /respond
//     handlers; no create endpoint)
//   - internal/adapter/native/escalations.go (escalation index;
//     mints from OrchestrationEvent)

/**
 * Subset of EscalationRequest the triage step consumes. Mirrored from
 * core.EscalationRequest but trimmed to fields the test actually uses.
 */
export type AcceptanceEscalation = {
  id: string;
  kind: string;
  urgency: 'blocking' | 'advisory';
  state: 'open' | 'resolved' | 'canceled';
  target?: string;
  summary?: string;
  created_at: string;
};

/**
 * Shape used when (eventually) POST'ing to the test-mode endpoint.
 * The backend bead can use this exact shape to keep wire-format
 * stability across the test/backend boundary.
 */
export type SyntheticEscalationSpec = {
  /**
   * Escalation kind. Matches core.EscalationKind values surfaced by
   * the orchestration adaptors (`hitl_approval`, `permission_prompt`,
   * `blocker`, `question`, etc.). Default: `hitl_approval`.
   */
  kind?: string;
  /** `blocking` (suspends sessions) or `advisory` (does not). Default: `blocking`. */
  urgency?: 'blocking' | 'advisory';
  /** Workitem id this escalation targets. Required so the triage UI can show context. */
  target: string;
  /** One-line operator-facing summary. Required. */
  summary: string;
  /**
   * Optional attribution. The acceptance harness writes
   * `acceptance-test:{runId}` so beads-side filters can disambiguate
   * synthetic escalations from real ones in postmortems.
   */
  source?: string;
};

export type InjectedEscalation = {
  /** The id of the minted escalation, useable with respondEscalation(). */
  id: string;
  /** Source kind/urgency/target round-tripped from the request. */
  spec: SyntheticEscalationSpec;
  /** True if the injector created a real backend escalation. */
  injected: boolean;
  /**
   * Human-readable explanation of how the injection landed (or why
   * it didn't). Threaded into the test report by the report writer.
   */
  note: string;
};

export type InjectError =
  | { kind: 'backend-not-supported'; status: number }
  | { kind: 'network'; message: string }
  | { kind: 'unexpected'; message: string };

/**
 * Inject a synthetic escalation. Returns the injection record on
 * success. On failure, returns an InjectError describing why so the
 * triage step can decide between (a) hard-fail the test or (b) file
 * a "backend-incomplete" bug bead and skip.
 *
 * When the backend test-mode endpoint lands, this helper will start
 * returning `injected: true` records; until then every call returns
 * `{kind: 'backend-not-supported', status: 404}` and the triage
 * step's fallback path runs.
 */
export async function injectEscalation(
  baseURL: string,
  spec: SyntheticEscalationSpec
): Promise<{ ok: true; value: InjectedEscalation } | { ok: false; err: InjectError }> {
  const body = {
    kind: spec.kind ?? 'hitl_approval',
    urgency: spec.urgency ?? 'blocking',
    target: spec.target,
    summary: spec.summary,
    source: spec.source ?? 'acceptance-test',
  };
  // The expected future endpoint. When backend-side this lands we
  // bump and revisit the fallback below.
  const url = `${baseURL}/api/v1/test/escalations`;
  let res: Response;
  try {
    res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  } catch (err) {
    return {
      ok: false,
      err: { kind: 'network', message: (err as Error).message },
    };
  }
  if (res.status === 404) {
    return {
      ok: false,
      err: { kind: 'backend-not-supported', status: 404 },
    };
  }
  if (!res.ok) {
    return {
      ok: false,
      err: {
        kind: 'unexpected',
        message: `${res.status} ${await res.text().catch(() => '')}`,
      },
    };
  }
  const parsed = (await res.json()) as { id: string };
  return {
    ok: true,
    value: {
      id: parsed.id,
      spec,
      injected: true,
      note: 'injected via /api/v1/test/escalations',
    },
  };
}

/**
 * Poll /api/escalations until an escalation matching the predicate
 * appears (or transitions to the desired state). Useful both for
 * waitForOpen (after injection) and waitForResolved (after the UI
 * approves).
 */
export async function waitForEscalation(
  baseURL: string,
  predicate: (esc: AcceptanceEscalation) => boolean,
  opts: { timeoutMs?: number; intervalMs?: number } = {}
): Promise<AcceptanceEscalation> {
  const timeoutMs = opts.timeoutMs ?? 30_000;
  const intervalMs = opts.intervalMs ?? 1_000;
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${baseURL}/api/escalations`);
      if (res.ok) {
        const body = (await res.json()) as
          | { escalations?: AcceptanceEscalation[] }
          | AcceptanceEscalation[];
        const all = Array.isArray(body) ? body : body.escalations ?? [];
        const hit = all.find(predicate);
        if (hit) return hit;
      }
    } catch {
      // Treat transient errors as continued wait.
    }
    await sleep(intervalMs);
  }
  throw new Error(
    `waitForEscalation: no matching escalation within ${timeoutMs}ms`
  );
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
