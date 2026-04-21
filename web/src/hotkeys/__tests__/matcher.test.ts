import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import { ChordMatcher } from '../matcher';
import type { Hotkey } from '../types';

function kb(init: Partial<KeyboardEventInit> & { key: string }): KeyboardEvent {
  return new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init });
}

const HOTKEYS: Hotkey[] = [
  { id: 'row-down', keys: ['j'], description: '', category: 'navigation' },
  { id: 'goto-top', keys: ['g', 'g'], description: '', category: 'navigation' },
  { id: 'goto-bottom', keys: ['G'], description: '', category: 'navigation' },
  { id: 'select-all', keys: ['*', 'a'], description: '', category: 'selection' },
  { id: 'select-invert', keys: ['*', 'i'], description: '', category: 'selection' },
  { id: 'help', keys: ['?'], description: '', category: 'help' },
];

describe('ChordMatcher', () => {
  let matcher: ChordMatcher;

  beforeEach(() => {
    vi.useFakeTimers();
    matcher = new ChordMatcher(500);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('matches a single-key hotkey', () => {
    const res = matcher.consume(kb({ key: 'j' }), HOTKEYS);
    expect(res.type).toBe('match');
    if (res.type === 'match') expect(res.hotkey.id).toBe('row-down');
  });

  it('matches a two-key chord across two presses', () => {
    expect(matcher.consume(kb({ key: 'g' }), HOTKEYS).type).toBe('prefix');
    const final = matcher.consume(kb({ key: 'g' }), HOTKEYS);
    expect(final.type).toBe('match');
    if (final.type === 'match') expect(final.hotkey.id).toBe('goto-top');
  });

  it('matches a star-prefixed chord', () => {
    expect(matcher.consume(kb({ key: '*' }), HOTKEYS).type).toBe('prefix');
    const final = matcher.consume(kb({ key: 'a' }), HOTKEYS);
    expect(final.type).toBe('match');
    if (final.type === 'match') expect(final.hotkey.id).toBe('select-all');
  });

  it('distinguishes between G (Shift+g) and g g chord', () => {
    const shift = matcher.consume(kb({ key: 'G', shiftKey: true }), HOTKEYS);
    expect(shift.type).toBe('match');
    if (shift.type === 'match') expect(shift.hotkey.id).toBe('goto-bottom');
  });

  it('returns none and clears buffer on unknown sequence', () => {
    matcher.consume(kb({ key: '*' }), HOTKEYS);
    const res = matcher.consume(kb({ key: 'z' }), HOTKEYS);
    expect(res.type).toBe('none');
    expect(matcher.bufferSnapshot()).toEqual([]);
  });

  it('clears chord buffer after timeout', () => {
    matcher.consume(kb({ key: 'g' }), HOTKEYS);
    expect(matcher.bufferSnapshot()).toEqual(['g']);
    vi.advanceTimersByTime(600);
    expect(matcher.bufferSnapshot()).toEqual([]);
  });

  it('matches the ? key directly', () => {
    const res = matcher.consume(kb({ key: '?', shiftKey: true }), HOTKEYS);
    expect(res.type).toBe('match');
    if (res.type === 'match') expect(res.hotkey.id).toBe('help');
  });
});
