// surfaceKind classifier tests (gm-bi5). Pins which adaptors fall under
// the organization vs execution surfaces. The default fallback path is
// load-bearing — unknown adaptors are 'execution', not 'organization'
// (most adaptors today live inside the workspace).

import { describe, expect, it } from 'vitest';
import { classifyAdaptor } from '../surfaceKind';

describe('classifyAdaptor', () => {
  it('classifies named organization adaptors', () => {
    expect(classifyAdaptor('jira')).toBe('organization');
    expect(classifyAdaptor('xray')).toBe('organization');
    expect(classifyAdaptor('compliance')).toBe('organization');
  });

  it('matches prefix-style organization adaptors', () => {
    expect(classifyAdaptor('lint-eslint')).toBe('organization');
    expect(classifyAdaptor('lint-go')).toBe('organization');
    expect(classifyAdaptor('policy-soc2')).toBe('organization');
    expect(classifyAdaptor('policy-rbac')).toBe('organization');
  });

  it('classifies execution adaptors', () => {
    expect(classifyAdaptor('beads')).toBe('execution');
    expect(classifyAdaptor('gastown')).toBe('execution');
    expect(classifyAdaptor('gas-town')).toBe('execution');
    expect(classifyAdaptor('langgraph')).toBe('execution');
    expect(classifyAdaptor('noop')).toBe('execution');
    expect(classifyAdaptor('dolt')).toBe('execution');
    expect(classifyAdaptor('bd')).toBe('execution');
  });

  it('is case-insensitive', () => {
    expect(classifyAdaptor('JIRA')).toBe('organization');
    expect(classifyAdaptor('Beads')).toBe('execution');
    expect(classifyAdaptor('Lint-Eslint')).toBe('organization');
  });

  it('defaults unknown adaptors to execution', () => {
    // The default-fallback path: most adaptors today live inside the
    // workspace, so unknown names should not silently leak into the
    // organization surface.
    expect(classifyAdaptor('mystery-tool')).toBe('execution');
    expect(classifyAdaptor('some-new-runner')).toBe('execution');
    expect(classifyAdaptor('')).toBe('execution');
  });
});
