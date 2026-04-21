import { describe, expect, it } from 'vitest';
import { normalizeKey, isEditableTarget } from '../keys';

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
