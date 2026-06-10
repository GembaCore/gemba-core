// usePersistedFilter tests (gm-e12.3.2). Assert the init-priority
// ordering (hash > localStorage > defaults), hash + storage writes
// on update, and hashchange re-read for browser back/forward.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { act, render, renderHook } from '@testing-library/react';
import { usePersistedFilter } from '../usePersistedFilter';
import { DEFAULT_BACKLOG_FILTER } from '@/lib/backlogFilter';

const STORAGE_KEY = 'gemba.test.filter';

function resetWorld() {
  window.localStorage.removeItem(STORAGE_KEY);
  window.history.replaceState(null, '', '/');
}

describe('usePersistedFilter', () => {
  beforeEach(resetWorld);
  afterEach(resetWorld);

  it('falls back to defaults when no hash and no storage', () => {
    const { result } = renderHook(() => usePersistedFilter(STORAGE_KEY));
    expect(result.current[0]).toEqual(DEFAULT_BACKLOG_FILTER);
  });

  it('prefers URL hash over localStorage on mount', () => {
    // localStorage says "completed only"; hash says "started only".
    // Hash wins — shared link trumps sticky state.
    window.localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ state_category: ['completed'], kind: [], search: '' })
    );
    window.history.replaceState(null, '', '/#state_category=started');

    const { result } = renderHook(() => usePersistedFilter(STORAGE_KEY));
    expect(result.current[0].state_category).toEqual(['started']);
  });

  it('falls back to localStorage when hash is empty', () => {
    window.localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ state_category: ['canceled'], kind: ['bug'], search: 'q' })
    );

    const { result } = renderHook(() => usePersistedFilter(STORAGE_KEY));
    expect(result.current[0]).toEqual({
      state_category: ['canceled'],
      kind: ['bug'],
      search: 'q',
    });
  });

  it('setFilter writes both localStorage and the URL hash', () => {
    const { result } = renderHook(() => usePersistedFilter(STORAGE_KEY));

    act(() => {
      result.current[1]({ state_category: ['started'], kind: ['task'], search: 'foo' });
    });

    expect(window.location.hash).toContain('state_category=started');
    expect(window.location.hash).toContain('kind=task');
    expect(window.location.hash).toContain('search=foo');
    expect(JSON.parse(window.localStorage.getItem(STORAGE_KEY)!)).toEqual({
      state_category: ['started'],
      kind: ['task'],
      search: 'foo',
    });
  });

  it('hashchange event replays into state (browser back/forward)', () => {
    const { result } = renderHook(() => usePersistedFilter(STORAGE_KEY));

    act(() => {
      window.history.replaceState(null, '', '/#state_category=completed');
      window.dispatchEvent(new HashChangeEvent('hashchange'));
    });

    expect(result.current[0].state_category).toEqual(['completed']);
  });

  it('clearing the hash resets to defaults on hashchange', () => {
    window.history.replaceState(null, '', '/#state_category=completed');
    const { result } = renderHook(() => usePersistedFilter(STORAGE_KEY));
    expect(result.current[0].state_category).toEqual(['completed']);

    act(() => {
      window.history.replaceState(null, '', '/');
      window.dispatchEvent(new HashChangeEvent('hashchange'));
    });
    expect(result.current[0]).toEqual(DEFAULT_BACKLOG_FILTER);
  });

  it('setFilter with empty filter clears the hash', () => {
    const { result } = renderHook(() => usePersistedFilter(STORAGE_KEY));
    act(() => {
      result.current[1]({ state_category: [], kind: [], search: '' });
    });
    expect(window.location.hash).toBe('');
  });
});

// Dummy render-consumer to satisfy the "render returns something" lint
// downstream if we ever convert the hook tests to component tests.
// Keeps this file importable even if the renderHook helper is swapped.
export function _smoke() {
  return render(<div />);
}
