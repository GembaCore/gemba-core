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

// canonicalChord folds the two ways operators write a shifted-letter
// chord into a single representation so the matcher can compare them
// equal (gm-jvl8). Two writing styles:
//
//   - Bake the Shift into the letter case: 'G' for Shift+g, 'Mod+W'
//     for Mod+Shift+w. This matches what [normalizeKey] emits for a
//     KeyboardEvent because e.key already carries the shifted form.
//   - Spell out the modifier: 'Shift+G', 'Mod+Shift+S'. Operators
//     reach for this when they want the chord to read literally —
//     gm-native.15's 'new-session' does this.
//
// Both styles must match the same KeyboardEvent. The chord
// `Mod+Shift+S` and the chord `Mod+S` both want to fire on Cmd-Shift-S
// (which produces e.key='S' with metaKey + shiftKey true). Without
// folding, the explicit-Shift form never matched anything because the
// runtime normalizer drops the Shift modifier for 1-char keys.
//
// The canonical form is "Shift baked into the letter" because that's
// what KeyboardEvent.key gives us — it's the cheaper conversion. The
// matcher applies this on both sides at compare time; HelpOverlay
// continues to use the operator's original chord string so the
// display stays explicit (`⌘⇧S` rather than `⌘S`).
export function canonicalChord(chord: string): string {
  if (!chord.includes('Shift+')) return chord;
  const parts = chord.split('+');
  const last = parts[parts.length - 1];
  // Only fold when the trailing token is a single character — for
  // multi-char named keys like ArrowUp / Escape the Shift modifier
  // is meaningful and stays as-is.
  if (last === undefined || last.length !== 1) return chord;
  const shiftIdx = parts.indexOf('Shift');
  if (shiftIdx < 0) return chord;
  parts.splice(shiftIdx, 1);
  // Force the trailing letter into the shifted (uppercase) form so
  // 'Mod+Shift+s' and 'Mod+Shift+S' both fold to 'Mod+S'.
  parts[parts.length - 1] = last.toUpperCase();
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
