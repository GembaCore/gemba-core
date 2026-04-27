#!/usr/bin/env node
// gm-e14.4: mirror ../docs/**/*.md into src/content/docs/, prepending a
// `title:` frontmatter derived from the first H1 when absent. The source
// markdown is the source of truth; the mirrored copy is gitignored.
//
// Also synthesizes two pages that don't exist as standalone source
// markdown:
//   - index.md          (the landing page; pulls a blurb from ../README.md)
//   - api/overview.md   (placeholder for the OpenAPI reference; the
//                        actual spec lives at ../docs/api/openapi.json
//                        and is published verbatim under /api/openapi.json)

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, '..', '..');
const srcDocs = path.join(repoRoot, 'docs');
const dstDocs = path.resolve(__dirname, '..', 'src', 'content', 'docs');
const publicDir = path.resolve(__dirname, '..', 'public');

const FRONTMATTER_RE = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/;
const H1_RE = /^#\s+(.+?)\s*$/m;

async function walk(dir, base = dir) {
  const entries = await fs.readdir(dir, { withFileTypes: true });
  const out = [];
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...(await walk(full, base)));
    } else if (entry.isFile()) {
      out.push(path.relative(base, full));
    }
  }
  return out;
}

function ensureTitle(content, fallback) {
  const fmMatch = content.match(FRONTMATTER_RE);
  if (fmMatch) {
    const block = fmMatch[1];
    if (/^title\s*:/m.test(block)) return content;
    const h1 = content.slice(fmMatch[0].length).match(H1_RE);
    const title = (h1?.[1] ?? fallback).replace(/"/g, '\\"');
    const newBlock = `${block}\ntitle: "${title}"`;
    return `---\n${newBlock}\n---\n${content.slice(fmMatch[0].length)}`;
  }
  const h1 = content.match(H1_RE);
  const title = (h1?.[1] ?? fallback).replace(/"/g, '\\"');
  return `---\ntitle: "${title}"\n---\n\n${content}`;
}

async function copyMarkdown(rel) {
  const src = path.join(srcDocs, rel);
  const dst = path.join(dstDocs, rel);
  await fs.mkdir(path.dirname(dst), { recursive: true });
  const raw = await fs.readFile(src, 'utf8');
  const fallback = path
    .basename(rel, path.extname(rel))
    .replace(/[-_]/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase());
  const out = ensureTitle(raw, fallback);
  await fs.writeFile(dst, out, 'utf8');
}

async function copyAsset(rel) {
  const src = path.join(srcDocs, rel);
  const dst = path.join(publicDir, rel);
  await fs.mkdir(path.dirname(dst), { recursive: true });
  await fs.copyFile(src, dst);
}

async function writeLandingPage() {
  const readme = await fs.readFile(path.join(repoRoot, 'README.md'), 'utf8');
  // Use the first ~60 lines of the README as the landing-page body.
  const body = readme.split('\n').slice(0, 80).join('\n');
  const out = `---
title: gemba
description: Single-binary work-tracker + agent orchestration UI.
template: splash
hero:
  tagline: Bring a WorkPlane. Optionally an OrchestrationPlane. Render whatever the two declare.
  actions:
    - text: Get started
      link: /getting-started/running-against-your-work-items/
      icon: right-arrow
      variant: primary
    - text: Source on GitHub
      link: https://github.com/MikeBengtson/gemba
      icon: external
---

${body}

## Continue reading

- [Run gemba against your work items](/getting-started/running-against-your-work-items/)
- [WorkPlane adaptor contract](/adaptors/workplane/)
- [OrchestrationPlane adaptor contract](/adaptors/orchestration/)
- [Beads adaptor](/adaptors/beads/)
- [Gas Town adaptor](/adaptors/gastown/)
`;
  await fs.writeFile(path.join(dstDocs, 'index.md'), out, 'utf8');
}

async function writeApiOverview() {
  const out = `---
title: API reference
description: HTTP API exposed by \`gemba serve\`.
---

The \`gemba serve\` HTTP API is described by an OpenAPI 3 document.

- **Spec**: [\`/api/openapi.json\`](./openapi.json)
- **Source of truth**: \`internal/server/openapi/openapi.json\` in the repo.
- **Lint**: \`make lint-openapi\` (Spectral, ruleset at \`.spectral.yaml\`).
- **Regenerate the typed SPA client**: \`make codegen\`.

> Interactive API explorer (Swagger / Redoc / Stoplight Elements) is
> tracked in **gm-e4.2**. Until that lands, point your tooling
> directly at the JSON above.
`;
  const dst = path.join(dstDocs, 'api', 'overview.md');
  await fs.mkdir(path.dirname(dst), { recursive: true });
  await fs.writeFile(dst, out, 'utf8');
}

async function main() {
  // Reset the mirrored content tree so deletions in ../docs propagate.
  await fs.rm(dstDocs, { recursive: true, force: true });
  await fs.mkdir(dstDocs, { recursive: true });

  // Wipe Astro's data store so the next build always sees a clean
  // content layer. Without this, stale entries from a previous run
  // can shadow the freshly-synced files (Astro persists the store
  // under node_modules/.astro for cross-build incremental sync).
  const astroCache = path.resolve(__dirname, '..', 'node_modules', '.astro');
  await fs.rm(astroCache, { recursive: true, force: true });

  const all = await walk(srcDocs);
  for (const rel of all) {
    if (rel.endsWith('.md') || rel.endsWith('.mdx')) {
      await copyMarkdown(rel);
    } else {
      // Non-markdown assets (openapi.json, images) go to /public so they
      // ship at predictable URLs (e.g. /api/openapi.json).
      await copyAsset(rel);
    }
  }

  await writeLandingPage();
  await writeApiOverview();
  // eslint-disable-next-line no-console
  console.log(`[sync-docs] mirrored ${all.length} files from ${srcDocs}`);
}

main().catch((err) => {
  console.error('[sync-docs] failed:', err);
  process.exit(1);
});
