import { describe, expect, it } from 'vitest';
import {
  DEFAULT_BACKLOG_FILTER,
  parseFromHash,
  parseFromJSON,
  serializeToHash,
  type BacklogFilter,
} from '../backlogFilter';

describe('serializeToHash / parseFromHash', () => {
  it('round-trips a filled-in filter', () => {
    const input: BacklogFilter = {
      state_category: ['backlog', 'started'],
      kind: ['task', 'bug'],
      search: 'dolt',
    };
    const hash = serializeToHash(input);
    expect(hash.startsWith('#')).toBe(true);
    expect(parseFromHash(hash)).toEqual(input);
  });

  it('omits empty fields for the default-default case', () => {
    expect(serializeToHash({ state_category: [], kind: [], search: '' })).toBe('');
  });

  it('parses repeated params into arrays', () => {
    expect(parseFromHash('#state_category=backlog&state_category=unstarted')).toEqual({
      state_category: ['backlog', 'unstarted'],
      kind: [],
      search: '',
    });
  });

  it('drops unknown state_category values without failing', () => {
    expect(
      parseFromHash('#state_category=backlog&state_category=nonsense&kind=task')
    ).toEqual({
      state_category: ['backlog'],
      kind: ['task'],
      search: '',
    });
  });

  it('returns null for empty / nonsense hashes', () => {
    expect(parseFromHash('')).toBeNull();
    expect(parseFromHash('#')).toBeNull();
    expect(parseFromHash('#unused_param=foo')).toBeNull();
  });
});

describe('parseFromJSON', () => {
  it('round-trips a JSON-serialised filter', () => {
    const raw = JSON.stringify(DEFAULT_BACKLOG_FILTER);
    expect(parseFromJSON(raw)).toEqual(DEFAULT_BACKLOG_FILTER);
  });

  it('returns null for malformed JSON', () => {
    expect(parseFromJSON('not-json')).toBeNull();
    expect(parseFromJSON(null)).toBeNull();
  });

  it('returns null when the shape mismatches', () => {
    expect(parseFromJSON(JSON.stringify({ state_category: 'not-an-array' }))).toBeNull();
    expect(parseFromJSON(JSON.stringify({ kind: ['a'], state_category: 'nope' }))).toBeNull();
  });

  it('drops unknown states and non-string kinds, tolerating partial input', () => {
    const raw = JSON.stringify({
      state_category: ['backlog', 'bogus'],
      kind: ['task', 42],
      search: 'x',
    });
    expect(parseFromJSON(raw)).toEqual({
      state_category: ['backlog'],
      kind: ['task'],
      search: 'x',
    });
  });
});
