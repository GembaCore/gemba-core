// RhpDetail — Detail-tab content registry + body renderer (gm-root.22.4).
//
// Per docs/design/rhp.md, kind owners (drawer migrations gm-root.22.5
// /.6/.7) register a content component, an icon, and a label per kind:
//
//   useEffect(() => registerDetailContent({
//     kind: 'workitem',
//     icon: FileText,
//     label: 'Work item',
//     render: (id) => <WorkItemDetail id={id} />,
//   }), []);
//
// The registry is shared via the RhpDetailRegistry context (defined in
// RhpContext.tsx). This file ships:
//
//   - registerDetailContent — the imperative registration function the
//     kind owners call from useEffect. It's a thin shim that takes the
//     same shape as the existing registry context's `register` method
//     so the contract reads naturally at the call site.
//   - useRegisterDetailContent — a React hook variant for components
//     that want to register on mount. Avoids the "remember to return
//     the unregister" boilerplate.
//   - RhpDetailContent — renders the active detail tab's content by
//     looking up the kind in the registry. Falls back to a placeholder
//     ("no content registered for kind X") if the kind isn't
//     registered.
//   - RhpFallbackRegistration — mounts inside the provider tree to
//     register the sentinel fallback icon, so the rail always has a
//     valid icon for unregistered kinds.

import { useEffect, useRef } from 'react';
import { Square } from 'lucide-react';
import {
  RHP_FALLBACK_KIND,
  useRhp,
  useRhpDetailRegistry,
  type RhpDetailContentRegistration,
} from './RhpContext';

/**
 * Register a detail-tab content component for a kind.
 *
 * Returns an unregister function. Typically called from a
 * `useEffect` so the registration is paired with the component's
 * lifetime.
 *
 * Note: the icon and label are owned by the kind owner per design
 * doc § "Detail tab kind icon registry" (refined in gm-root.22.4 to
 * keep kind metadata in one place rather than baking a kind→icon map
 * into the RHP itself).
 */
export function registerDetailContent(
  registry: ReturnType<typeof useRhpDetailRegistry>,
  reg: RhpDetailContentRegistration
): () => void {
  return registry.register(reg);
}

/**
 * Hook variant: register a detail-content kind for the lifetime of the
 * calling component. The registration is keyed by `reg.kind` only — the
 * `render`/`icon`/`label` fields are read through a ref each time the
 * registered render fn is invoked, so callers can safely pass an inline
 * `reg` object with a fresh closure on every render. (Depending on
 * `reg.render` would re-register every render and trigger a
 * bumpDetailReg loop.)
 */
export function useRegisterDetailContent(reg: RhpDetailContentRegistration): void {
  const registry = useRhpDetailRegistry();
  const regRef = useRef(reg);
  regRef.current = reg;
  useEffect(() => {
    return registry.register({
      kind: reg.kind,
      icon: regRef.current.icon,
      label: regRef.current.label,
      render: (id) => regRef.current.render(id),
    });
  }, [registry, reg.kind]);
}

/**
 * Renders the active detail tab's content.
 *
 * Resolves the active tab's kind via useRhp() + the registry. Pinned
 * tabs are not handled here — the shell routes them to their own
 * content (Help in v1).
 *
 * If no tab is active, returns null. If the active tab is pinned,
 * returns null (caller should branch on `tab.pinned` and render the
 * pinned content). If the active tab is a detail tab whose kind isn't
 * registered, renders a placeholder body.
 */
export function RhpDetailContent() {
  const { tabs, activeTabId } = useRhp();
  const registry = useRhpDetailRegistry();

  const active = tabs.find((t) => t.id === activeTabId) ?? null;
  if (!active || active.pinned) return null;

  // The tab's id is `kind:id`; split on the first ':' to recover the
  // detail id (which may itself contain ':' or '/').
  const colon = active.id.indexOf(':');
  if (colon <= 0) return null;
  const kind = active.id.slice(0, colon);
  const detailId = active.id.slice(colon + 1);

  const reg = registry.resolve(kind);
  if (!reg) {
    return (
      <div data-testid="rhp-detail-placeholder" className="text-sm text-neutral-600 dark:text-neutral-400">
        <p>No content registered for kind <code>{kind}</code>.</p>
        <p className="mt-2 text-xs text-neutral-500">
          id: <code>{detailId}</code>
        </p>
      </div>
    );
  }

  return <>{reg.render(detailId)}</>;
}

/**
 * Mount-once side-effect component that registers the fallback icon
 * (used by the rail for kinds that haven't been registered yet) and
 * any other RHP-owned defaults. Drop this inside the RhpProvider tree
 * — RhpShell mounts it automatically.
 */
export function RhpFallbackRegistration() {
  const registry = useRhpDetailRegistry();
  useEffect(() => {
    return registry.register({
      kind: RHP_FALLBACK_KIND,
      icon: Square,
      label: 'Detail',
      // Fallback render is unused — the placeholder branch in
      // RhpDetailContent handles unregistered kinds. This render is
      // here only to satisfy the registration shape; if a real
      // detail tab ever resolves to the fallback (a programmer
      // error), we surface a clear message.
      render: () => (
        <div className="text-sm text-neutral-500">
          Detail content unavailable.
        </div>
      ),
    });
  }, [registry]);
  return null;
}
