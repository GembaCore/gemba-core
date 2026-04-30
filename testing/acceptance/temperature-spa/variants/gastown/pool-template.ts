// gm-root.27.16 — render helper for the gastown variant pool fixture.
//
// The shared configurePool flow (gm-root.27.7) saves pool config through the
// SPA editor; specs round-trip the file by rendering this template, comparing
// it to what the editor wrote, and asserting equality.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const TEMPLATE_PATH = resolve(here, 'fixtures/pool.toml.tmpl');

export function renderGastownPoolToml(rigName: string): string {
  if (!/^[a-zA-Z][a-zA-Z0-9_-]*$/.test(rigName)) {
    throw new Error(
      `renderGastownPoolToml: rig name ${JSON.stringify(rigName)} is not a valid TOML key segment`,
    );
  }
  const tmpl = readFileSync(TEMPLATE_PATH, 'utf8');
  return tmpl.replace(/\{\{\.Rig\}\}/g, rigName);
}

export const GASTOWN_POOL_TEMPLATE_PATH = TEMPLATE_PATH;
