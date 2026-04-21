export function normalizeKey(e: KeyboardEvent): string {
  const parts: string[] = [];
  if (e.metaKey || e.ctrlKey) parts.push('Mod');
  if (e.altKey) parts.push('Alt');
  const key = e.key;
  // For letters / symbols, Shift is already baked into `key` (e.g. `G`, `?`, `*`).
  // Only prefix `Shift+` for named keys like `Shift+ArrowUp` or `Shift+Escape`.
  if (e.shiftKey && key.length > 1) parts.push('Shift');
  parts.push(key === ' ' ? 'Space' : key);
  return parts.join('+');
}

export function isEditableTarget(target: EventTarget | null): boolean {
  if (!target || typeof HTMLElement === 'undefined') return false;
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (target.isContentEditable) return true;
  return false;
}

export function formatKeys(keys: string[]): string {
  return keys
    .map((k) =>
      k
        .replace(/\bMod\+/g, navigator.platform.includes('Mac') ? '⌘' : 'Ctrl+')
        .replace(/\bShift\+/g, '⇧')
        .replace(/\bAlt\+/g, navigator.platform.includes('Mac') ? '⌥' : 'Alt+')
        .replace(/^Space$/, '␣')
        .replace(/^Escape$/, 'Esc')
    )
    .join(' ');
}
