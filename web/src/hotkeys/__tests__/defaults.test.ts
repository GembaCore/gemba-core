import { describe, expect, it } from 'vitest';
import { DEFAULT_HOTKEYS } from '../defaults';
import { canonicalChord, normalizeKey } from '../keys';

function kb(init: KeyboardEventInit): KeyboardEvent {
  return new KeyboardEvent('keydown', init);
}

describe('DEFAULT_HOTKEYS', () => {
  it('registers at least the 15 shortcuts required by gm-7hj DoD', () => {
    expect(DEFAULT_HOTKEYS.length).toBeGreaterThanOrEqual(15);
  });

  it('has unique ids', () => {
    const ids = DEFAULT_HOTKEYS.map((h) => h.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('has unique key sequences within each scope (gm-0tl)', () => {
    // Chord uniqueness is per-scope rather than global so the same
    // chord can have a different meaning when a scope is active. e.g.
    // `n` is `new` in global and `walk-next` under 'walk-active';
    // `d` is `walk-defer` under 'walk-active' but does nothing
    // globally; `Enter` is `walk-suggested-apply` under 'walk-active'
    // and `graph-open` under 'graph'.
    const byScope = new Map<string, string[]>();
    for (const hk of DEFAULT_HOTKEYS) {
      const scope = hk.scope ?? 'global';
      const seq = hk.keys.join(' ');
      const list = byScope.get(scope) ?? [];
      list.push(seq);
      byScope.set(scope, list);
    }
    for (const [scope, seqs] of byScope) {
      expect(new Set(seqs).size, `duplicate chord in scope ${scope}`).toBe(seqs.length);
    }
  });

  it('covers the Foolery pattern set plus Gemba additions', () => {
    const ids = new Set(DEFAULT_HOTKEYS.map((h) => h.id));
    // Foolery-pattern
    for (const must of [
      'row-down',
      'row-up',
      'goto-top',
      'goto-bottom',
      'focus-search',
      'select-toggle',
      'select-all',
      'select-invert',
      'bulk-edit',
      'bulk-done',
      'bulk-delete',
      'bulk-label',
      'new',
      'clone',
      'help',
    ]) {
      expect(ids.has(must), `missing shortcut: ${must}`).toBe(true);
    }
    // Gemba-specific additions (per bead)
    for (const must of ['workspace-switch', 'capability-browser', 'drift-view']) {
      expect(ids.has(must), `missing gemba shortcut: ${must}`).toBe(true);
    }
  });

  it('every Mod+Shift+<letter> chord matches its expected KeyboardEvent (gm-jvl8)', () => {
    // Regression guard: the normalizer drops Shift+ for 1-char keys
    // and the matcher folds via canonicalChord. Verify the round-trip
    // for the two chords that previously silently never fired.
    const cases: Array<{ chord: string; event: KeyboardEventInit }> = [
      { chord: 'Mod+Shift+S', event: { key: 'S', metaKey: true, shiftKey: true } },
      { chord: 'Mod+W', event: { key: 'W', metaKey: true, shiftKey: true } },
    ];
    for (const { chord, event } of cases) {
      // Confirm each chord is actually registered so the test stays
      // honest if defaults.ts ever drops one of the surfaces.
      const seq = DEFAULT_HOTKEYS.flatMap((h) => h.keys);
      expect(seq, `chord ${chord} expected in DEFAULT_HOTKEYS`).toContain(chord);
      expect(canonicalChord(chord)).toBe(canonicalChord(normalizeKey(kb(event))));
    }
  });

  it('does not conflict with common browser/OS shortcuts', () => {
    // No hotkey should bind Mod+r, Mod+t, Mod+w, etc.
    const banned = ['Mod+r', 'Mod+t', 'Mod+w', 'Mod+n', 'Mod+q'];
    for (const hk of DEFAULT_HOTKEYS) {
      for (const seq of hk.keys) {
        expect(banned, `${hk.id} uses ${seq}`).not.toContain(seq);
      }
    }
  });
});
