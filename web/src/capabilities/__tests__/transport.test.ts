import { describe, expect, it } from 'vitest';
import { endpointFor } from '@/capabilities';

// Transport routing is centralised so a future direct-to-mcp browser
// transport lands in one place (gm-root DD-12). These tests pin the
// current invariant: every plane, every transport, flows through
// /api/*. If that ever changes the failures here will drag the
// negotiation logic into view.

describe('endpointFor', () => {
  it('prefixes /api regardless of plane or transport', () => {
    for (const plane of ['work', 'orchestration'] as const) {
      for (const t of ['api', 'jsonl', 'mcp'] as const) {
        expect(endpointFor(plane, t, '/beads')).toBe('/api/beads');
      }
    }
  });

  it('tolerates paths with or without a leading slash', () => {
    expect(endpointFor('work', 'api', 'beads')).toBe('/api/beads');
    expect(endpointFor('work', 'api', '/beads')).toBe('/api/beads');
  });
});
