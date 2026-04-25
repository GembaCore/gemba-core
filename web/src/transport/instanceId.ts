// instanceId — per-process boot stamp guard (gm-6m60).
//
// Capabilities are startup-immutable on the server: they're configured
// during gemba serve boot and never mutate during the process lifetime.
// Rather than maintain a capabilities-changed channel, the SPA fetches
// once with staleTime: Infinity. The instance_id stamped on every
// startup-immutable response (currently /api/capabilities and
// /api/adaptors snapshot + SSE) is the SPA's restart sentinel.
//
// observe() stashes the first id it sees and triggers a hard reload
// the first time a later id differs. The reload reboots the SPA so it
// fetches a fresh capabilities manifest and any cached gates that
// depend on it. Idempotent: once we've started a reload we suppress
// further triggers so a flurry of mismatched frames doesn't queue
// multiple reloads.
//
// All callers go through observe(); there is intentionally no setter
// or reset, because a real id change always means "the server we
// connected to is gone."

let observedId: string | null = null;
let reloading = false;

/**
 * Triggers a full-page reload. Replaced in tests via __setReloader for
 * deterministic assertions without spinning up a fake jsdom navigation.
 */
let reloader: () => void = () => {
  if (typeof window !== 'undefined') {
    window.location.reload();
  }
};

export function observeInstanceId(id: string | undefined | null): void {
  if (!id || reloading) return;
  if (observedId === null) {
    observedId = id;
    return;
  }
  if (observedId !== id) {
    reloading = true;
    reloader();
  }
}

export function currentInstanceId(): string | null {
  return observedId;
}

// __resetForTest and __setReloader are test-only escape hatches — kept
// out of production paths by virtue of nobody else importing them.
export function __resetForTest(): void {
  observedId = null;
  reloading = false;
}

export function __setReloader(fn: () => void): void {
  reloader = fn;
}
