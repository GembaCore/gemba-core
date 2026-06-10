import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  __resetForTest,
  __setReloader,
  currentInstanceId,
  observeInstanceId,
} from '../instanceId';

describe('instanceId guard', () => {
  afterEach(() => {
    __resetForTest();
    __setReloader(() => {});
  });

  it('stashes the first id it sees', () => {
    observeInstanceId('a-1');
    expect(currentInstanceId()).toBe('a-1');
  });

  it('does not reload while the id stays stable', () => {
    const reload = vi.fn();
    __setReloader(reload);
    observeInstanceId('a-1');
    observeInstanceId('a-1');
    observeInstanceId('a-1');
    expect(reload).not.toHaveBeenCalled();
  });

  it('reloads exactly once when the id changes', () => {
    const reload = vi.fn();
    __setReloader(reload);
    observeInstanceId('a-1');
    observeInstanceId('b-2');
    observeInstanceId('c-3'); // should not queue a second reload
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('ignores empty/undefined ids (lets pre-stamp transitional frames pass)', () => {
    const reload = vi.fn();
    __setReloader(reload);
    observeInstanceId(undefined);
    observeInstanceId(null);
    observeInstanceId('');
    observeInstanceId('a-1');
    observeInstanceId(undefined); // must not look like a change
    expect(reload).not.toHaveBeenCalled();
    expect(currentInstanceId()).toBe('a-1');
  });
});
