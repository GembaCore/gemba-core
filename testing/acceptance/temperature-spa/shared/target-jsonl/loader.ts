// testing/acceptance/temperature-spa/shared/target-jsonl/loader.ts
//
// gm-root.27.4 — JSONL pack loader.
//
// The static .jsonl.tmpl files in this directory carry `{{PREFIX}}`
// placeholders for bead ids. The loader substitutes the placeholder
// with the runtime bead-id prefix (e.g. `e2e0`) and returns paths
// suitable for `bd import`.
//
// Why placeholders: realServer.ts initializes bd with a per-worker
// prefix (`e2e${workerIndex}`); if the JSONL committed bead ids
// like `tspa-m1`, `bd import` would either reject them (prefix
// mismatch — `bd import` checks `--force` is required) or pollute
// the project's namespace. Substituting the runtime prefix keeps
// the imported beads consistent with everything bd later creates.
//
// References:
//   - D17 docs/design/acceptance-temperature-spa.md §5
//   - testing/e2e/fixtures/realServer.ts (prefix derivation)

import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const PACK_DIR = dirname(fileURLToPath(import.meta.url));

export type Milestone = 'm1' | 'm2' | 'm3';

export type LoadedPack = {
  /** Filesystem path to the rendered JSONL ready for `bd import`. */
  path: string;
  /** The milestone this pack covers, or 'decisions' for the internal-decision pack. */
  kind: Milestone | 'decisions';
  /** Bead-id prefix applied to every line in the pack. */
  prefix: string;
};

/**
 * Render a pack template with the given prefix and write the result
 * to `outputDir/<kind>.jsonl`. Returns the rendered file path. The
 * acceptance test then calls `bd import <path>` (or the equivalent
 * REST endpoint when bd-bridge supports it).
 */
export function renderPack(
  kind: Milestone | 'decisions',
  prefix: string,
  outputDir: string
): LoadedPack {
  const tmplPath = join(PACK_DIR, `${kind}.jsonl.tmpl`);
  const raw = readFileSync(tmplPath, 'utf8');
  if (raw.includes('{{PREFIX}}\\') || /\{\{[A-Z_]+\}\}/.test(raw.replace(/\{\{PREFIX\}\}/g, ''))) {
    // Defensive: catch typos / un-substituted placeholders that
    // aren't {{PREFIX}}. Listing the actual unknown tokens helps the
    // pack author when they add a new field.
    const unknown = (raw.match(/\{\{[A-Z_]+\}\}/g) ?? []).filter(
      (t) => t !== '{{PREFIX}}'
    );
    if (unknown.length > 0) {
      throw new Error(
        `renderPack ${kind}: unknown placeholders ${[...new Set(unknown)].join(', ')}`
      );
    }
  }
  const rendered = raw.replace(/\{\{PREFIX\}\}/g, prefix);
  const outPath = join(outputDir, `${kind}.jsonl`);
  writeFileSync(outPath, rendered, 'utf8');
  return { path: outPath, kind, prefix };
}

/**
 * Convenience: render every pack into outputDir and return them in
 * the canonical import order — decisions first, then m1, m2, m3.
 * The order matters because m2.jsonl contains a `blocks: m1` edge,
 * etc. — bd import handles forward references in a single file but
 * cross-file ordering is up to the caller.
 */
export function renderAllPacks(prefix: string, outputDir: string): LoadedPack[] {
  return [
    renderPack('decisions', prefix, outputDir),
    renderPack('m1', prefix, outputDir),
    renderPack('m2', prefix, outputDir),
    renderPack('m3', prefix, outputDir),
  ];
}
