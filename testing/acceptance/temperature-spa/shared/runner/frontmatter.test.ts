// Unit tests for frontmatter parser (gm-root.27.2 / gm-root.27.28).

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { parseFrontmatter, stripFrontmatter } from './frontmatter.ts';

describe('parseFrontmatter', () => {
  it('returns empty when description is undefined', () => {
    const fm = parseFrontmatter(undefined);
    assert.equal(fm.template, undefined);
    assert.equal(fm.testid, undefined);
    assert.equal(fm.files, undefined);
    assert.deepEqual(fm.extras, {});
  });

  it('returns empty when there is no frontmatter block', () => {
    const fm = parseFrontmatter('# Goal\nNo frontmatter here.');
    assert.equal(fm.template, undefined);
    assert.deepEqual(fm.extras, {});
  });

  it('parses template / testid / files', () => {
    const desc = `---
template: write-component
testid: temperature-table, row-{c}
files: src/TemperatureTable.tsx
---

# Goal
Implement the table.
`;
    const fm = parseFrontmatter(desc);
    assert.equal(fm.template, 'write-component');
    assert.deepEqual(fm.testid, ['temperature-table', 'row-{c}']);
    assert.deepEqual(fm.files, ['src/TemperatureTable.tsx']);
  });

  it('captures unknown keys in extras', () => {
    const desc = `---
template: noop
priority: high
custom-thing: yes
---
body
`;
    const fm = parseFrontmatter(desc);
    assert.equal(fm.template, 'noop');
    assert.equal(fm.extras.priority, 'high');
    assert.equal(fm.extras['custom-thing'], 'yes');
  });

  it('tolerates blank / malformed lines', () => {
    const desc = `---

template: build

bare-line-without-colon
files: a.ts, b.ts
---
body
`;
    const fm = parseFrontmatter(desc);
    assert.equal(fm.template, 'build');
    assert.deepEqual(fm.files, ['a.ts', 'b.ts']);
  });

  it('keys are case-insensitive', () => {
    const desc = `---
TEMPLATE: noop
Files: a.ts
---
body
`;
    const fm = parseFrontmatter(desc);
    assert.equal(fm.template, 'noop');
    assert.deepEqual(fm.files, ['a.ts']);
  });
});

describe('stripFrontmatter', () => {
  it('returns the body without the leading block', () => {
    const desc = `---
template: noop
---

# Goal
The body.`;
    assert.equal(stripFrontmatter(desc), '# Goal\nThe body.');
  });

  it('returns the description unchanged when no block', () => {
    assert.equal(stripFrontmatter('# Goal'), '# Goal');
  });

  it('returns empty string for undefined', () => {
    assert.equal(stripFrontmatter(undefined), '');
  });
});
