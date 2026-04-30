// testing/acceptance/temperature-spa/shared/runner/frontmatter.ts
//
// gm-root.27.2 — frontmatter parser.
//
// Each task bead in the target JSONL pack (gm-root.27.4) starts its
// description with a triple-dash YAML-flavored block that names the
// MockAgentRunner template, the testid contract, and the files the
// bead touches:
//
//   ---
//   template: write-component
//   testid: temperature-table, row-{c}
//   files: src/TemperatureTable.tsx
//   ---
//
//   # Goal ...
//
// We don't pull in a full YAML parser; the format is a strict subset
// (only key: value lines, no nesting). Keeps the runner's dependency
// footprint zero.

export type Frontmatter = {
  template?: string;
  testid?: string[];
  files?: string[];
  /** Any additional `key: value` lines that future templates might add. */
  extras: Record<string, string>;
};

/**
 * Parse a bead description and return its frontmatter (if present).
 * If no `---` block leads the description, returns an empty Frontmatter
 * with `extras: {}`. Tolerant of leading whitespace inside the block.
 */
export function parseFrontmatter(description: string | undefined): Frontmatter {
  const empty: Frontmatter = { extras: {} };
  if (!description) return empty;
  // Match a leading frontmatter block. Allow the description to start
  // with the block (no preceding whitespace) — bd description
  // round-trips preserve the leading `---`.
  const match = description.match(/^---\s*\n([\s\S]*?)\n---\s*\n/);
  if (!match) return empty;
  const body = match[1];
  if (!body) return empty;
  const out: Frontmatter = { extras: {} };
  for (const line of body.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const colon = trimmed.indexOf(':');
    if (colon <= 0) continue;
    const key = trimmed.slice(0, colon).trim().toLowerCase();
    const value = trimmed.slice(colon + 1).trim();
    if (!key || !value) continue;
    if (key === 'template') {
      out.template = value;
    } else if (key === 'testid') {
      out.testid = splitCsv(value);
    } else if (key === 'files') {
      out.files = splitCsv(value);
    } else {
      out.extras[key] = value;
    }
  }
  return out;
}

/**
 * Strip the frontmatter block from a description so the body proper
 * (the agent-facing prose) can be presented separately. If no block
 * is present, returns the description unchanged.
 */
export function stripFrontmatter(description: string | undefined): string {
  if (!description) return '';
  return description.replace(/^---\s*\n[\s\S]*?\n---\s*\n/, '');
}

function splitCsv(value: string): string[] {
  return value
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}
