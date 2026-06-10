import type { Hotkey } from './types';
import { canonicalChord, normalizeKey } from './keys';

export type MatchResult = { type: 'none' } | { type: 'prefix' } | { type: 'match'; hotkey: Hotkey };

export class ChordMatcher {
  private buffer: string[] = [];
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(private readonly timeoutMs = 1000) {}

  reset(): void {
    this.buffer = [];
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
  }

  bufferSnapshot(): string[] {
    return [...this.buffer];
  }

  consume(e: KeyboardEvent, active: Hotkey[]): MatchResult {
    const key = normalizeKey(e);
    const next = [...this.buffer, key];

    // Compare via canonicalChord on both sides so a registered
    // 'Mod+Shift+S' folds to the same shape as the runtime-normalized
    // 'Mod+S' for the matching keystroke (gm-jvl8). The hotkey's
    // original keys[] stays intact for HelpOverlay display.
    const matches = (hk: Hotkey, idx: number, k: string) =>
      canonicalChord(hk.keys[idx]) === canonicalChord(k);

    const exact = active.find(
      (hk) => hk.keys.length === next.length && hk.keys.every((_, i) => matches(hk, i, next[i]))
    );
    if (exact) {
      this.reset();
      return { type: 'match', hotkey: exact };
    }

    const prefix = active.some(
      (hk) =>
        hk.keys.length > next.length &&
        hk.keys.slice(0, next.length).every((_, i) => matches(hk, i, next[i]))
    );
    if (prefix) {
      this.buffer = next;
      if (this.timer !== null) clearTimeout(this.timer);
      this.timer = setTimeout(() => this.reset(), this.timeoutMs);
      return { type: 'prefix' };
    }

    this.reset();
    return { type: 'none' };
  }
}
