import { describe, expect, it } from 'vitest';
import { canonicalChord, normalizeKey, isEditableTarget } from '../keys';

function kb(init: KeyboardEventInit): KeyboardEvent {
  return new KeyboardEvent('keydown', init);
}

describe('normalizeKey', () => {
  it('returns bare letter for unshifted key', () => {
    expect(normalizeKey(kb({ key: 'j' }))).toBe('j');
  });

  it('preserves shifted letters (uppercase) without Shift+ prefix', () => {
    expect(normalizeKey(kb({ key: 'G', shiftKey: true }))).toBe('G');
  });

  it('preserves shifted symbols (? *) without Shift+ prefix', () => {
    expect(normalizeKey(kb({ key: '?', shiftKey: true }))).toBe('?');
    expect(normalizeKey(kb({ key: '*', shiftKey: true }))).toBe('*');
  });

  it('adds Shift+ prefix for named special keys', () => {
    expect(normalizeKey(kb({ key: 'ArrowUp', shiftKey: true }))).toBe('Shift+ArrowUp');
  });

  it('adds Mod+ prefix for meta or ctrl', () => {
    expect(normalizeKey(kb({ key: 'k', metaKey: true }))).toBe('Mod+k');
    expect(normalizeKey(kb({ key: 'k', ctrlKey: true }))).toBe('Mod+k');
  });

  it('represents space as Space', () => {
    expect(normalizeKey(kb({ key: ' ' }))).toBe('Space');
  });
});

describe('canonicalChord (gm-jvl8)', () => {
  it('round-trips chords without Shift+ unchanged', () => {
    expect(canonicalChord('j')).toBe('j');
    expect(canonicalChord('Mod+k')).toBe('Mod+k');
    expect(canonicalChord('G')).toBe('G');
    expect(canonicalChord('Mod+W')).toBe('Mod+W');
  });

  it('drops Shift+ from 1-char-key chords (folds to shifted-letter form)', () => {
    expect(canonicalChord('Mod+Shift+S')).toBe('Mod+S');
    expect(canonicalChord('Shift+G')).toBe('G');
  });

  it('uppercases the trailing letter so case-insensitive variants fold together', () => {
    expect(canonicalChord('Mod+Shift+s')).toBe('Mod+S');
  });

  it('keeps Shift+ for multi-char named keys', () => {
    expect(canonicalChord('Shift+ArrowUp')).toBe('Shift+ArrowUp');
    expect(canonicalChord('Mod+Shift+Escape')).toBe('Mod+Shift+Escape');
  });

  it('matches normalizeKey output for the same logical keystroke', () => {
    // The whole point: the chord 'Mod+Shift+S' must collapse to the
    // same canonical form as the normalized event for Cmd+Shift+S.
    const chord = canonicalChord('Mod+Shift+S');
    const event = canonicalChord(
      normalizeKey(kb({ key: 'S', metaKey: true, shiftKey: true })),
    );
    expect(chord).toBe(event);
  });
});

describe('isEditableTarget', () => {
  it('returns true for input/textarea/select', () => {
    expect(isEditableTarget(document.createElement('input'))).toBe(true);
    expect(isEditableTarget(document.createElement('textarea'))).toBe(true);
    expect(isEditableTarget(document.createElement('select'))).toBe(true);
  });

  it('returns false for regular elements and null', () => {
    expect(isEditableTarget(document.createElement('div'))).toBe(false);
    expect(isEditableTarget(null)).toBe(false);
  });
});
