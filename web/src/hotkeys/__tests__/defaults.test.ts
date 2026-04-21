import { describe, expect, it } from 'vitest';
import { DEFAULT_HOTKEYS } from '../defaults';

describe('DEFAULT_HOTKEYS', () => {
  it('registers at least the 15 shortcuts required by gm-7hj DoD', () => {
    expect(DEFAULT_HOTKEYS.length).toBeGreaterThanOrEqual(15);
  });

  it('has unique ids', () => {
    const ids = DEFAULT_HOTKEYS.map((h) => h.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('has unique key sequences (no conflicts)', () => {
    const seqs = DEFAULT_HOTKEYS.map((h) => h.keys.join(' '));
    expect(new Set(seqs).size).toBe(seqs.length);
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
