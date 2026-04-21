import type { Hotkey } from './types';
import { normalizeKey } from './keys';

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

    const exact = active.find(
      (hk) => hk.keys.length === next.length && hk.keys.every((k, i) => k === next[i])
    );
    if (exact) {
      this.reset();
      return { type: 'match', hotkey: exact };
    }

    const prefix = active.some(
      (hk) =>
        hk.keys.length > next.length && hk.keys.slice(0, next.length).every((k, i) => k === next[i])
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
