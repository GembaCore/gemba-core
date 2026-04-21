import { useCallback, useEffect, useState } from 'react';
import type { Hotkey, HotkeyCategory } from './types';
import { useActiveHotkeys, useHotkey } from './hooks';
import { formatKeys } from './keys';

const CATEGORY_ORDER: HotkeyCategory[] = [
  'navigation',
  'selection',
  'bulk',
  'creation',
  'views',
  'drawer',
  'help',
];

const CATEGORY_LABEL: Record<HotkeyCategory, string> = {
  navigation: 'Navigation',
  selection: 'Selection',
  bulk: 'Bulk',
  creation: 'Create',
  views: 'Views',
  drawer: 'Drawer & panel',
  help: 'Help',
};

function group(hotkeys: Hotkey[]): Record<HotkeyCategory, Hotkey[]> {
  const out = {} as Record<HotkeyCategory, Hotkey[]>;
  for (const cat of CATEGORY_ORDER) out[cat] = [];
  for (const hk of hotkeys) out[hk.category].push(hk);
  return out;
}

export function HelpOverlay() {
  const [open, setOpen] = useState(false);
  const hotkeys = useActiveHotkeys();

  const toggle = useCallback(() => setOpen((o) => !o), []);
  const close = useCallback(() => setOpen(false), []);

  useHotkey('help', toggle);
  useHotkey('drawer-close', close);

  useEffect(() => {
    if (!open) return;
    // Lock body scroll when overlay is up.
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = prev;
    };
  }, [open]);

  if (!open) return null;

  const grouped = group(hotkeys);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Keyboard shortcuts"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      onClick={close}
    >
      <div
        className="max-h-[80vh] w-full max-w-3xl overflow-y-auto rounded-lg border border-neutral-200 bg-white p-6 shadow-xl dark:border-neutral-800 dark:bg-neutral-950"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Keyboard shortcuts</h2>
          <button
            type="button"
            onClick={close}
            aria-label="Close shortcuts"
            className="rounded-md px-2 py-1 text-sm text-neutral-500 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-900"
          >
            Esc
          </button>
        </div>
        <div className="grid grid-cols-1 gap-x-10 gap-y-6 md:grid-cols-2">
          {CATEGORY_ORDER.map((cat) => {
            const list = grouped[cat];
            if (list.length === 0) return null;
            return (
              <section key={cat}>
                <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
                  {CATEGORY_LABEL[cat]}
                </h3>
                <ul className="flex flex-col gap-1.5">
                  {list.map((hk) => (
                    <li key={hk.id} className="flex items-center justify-between gap-4 text-sm">
                      <span className="text-neutral-700 dark:text-neutral-300">
                        {hk.description}
                      </span>
                      <kbd className="rounded bg-neutral-100 px-2 py-0.5 font-mono text-xs text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
                        {formatKeys(hk.keys)}
                      </kbd>
                    </li>
                  ))}
                </ul>
              </section>
            );
          })}
        </div>
      </div>
    </div>
  );
}
