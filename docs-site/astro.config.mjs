// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// gm-e14.4: Astro Starlight config for the gemba docs site.
//
// Source markdown lives in ../docs (the in-repo operator-facing tree).
// `pnpm run sync` (scripts/sync-docs.mjs) mirrors that tree into
// src/content/docs/ at build time, prepending a `title:` frontmatter
// derived from the first H1 if missing. The mirrored copy is
// gitignored — the source-of-truth markdown is untouched.
//
// Site / base URL targets GitHub Pages at GembaCore/gemba-core so the
// deployed URL is https://gembacore.github.io/gemba-core/. Override
// with SITE / BASE env vars if hosted elsewhere.

const site = process.env.SITE ?? 'https://gembacore.github.io';
const base = process.env.BASE ?? '/gemba-core/';

export default defineConfig({
  site,
  base,
  trailingSlash: 'ignore',
  integrations: [
    starlight({
      title: 'gemba',
      description:
        'Single-binary work-tracker + agent orchestration UI. Bring a WorkPlane adaptor, optionally a runtime.',
      logo: { src: './src/assets/logo.svg' },
      social: {
        github: 'https://github.com/GembaCore/gemba-core',
      },
      // The mirrored content tree under docs-site/src/content/docs/ is
      // gitignored; the source-of-truth markdown lives in /docs at the
      // repo root, so "edit this page" should send operators there.
      editLink: {
        baseUrl: 'https://github.com/GembaCore/gemba-core/edit/main/docs/',
      },
      sidebar: [
        {
          label: 'Getting started',
          items: [
            { label: 'Overview', link: '/' },
            {
              label: 'Run against your work items',
              link: '/getting-started/running-against-your-work-items/',
            },
            { label: 'Parallelism', link: '/getting-started/parallelism/' },
          ],
        },
        {
          label: 'Concepts',
          items: [
            { label: 'Dispatch vs Planning', link: '/concepts/dispatch-vs-planning/' },
            { label: 'Agents, sessions & agent types', link: '/concepts/agents-and-sessions/' },
            { label: 'WorkPlane', link: '/adaptors/workplane/' },
            { label: 'OrchestrationPlane', link: '/adaptors/orchestration/' },
            { label: 'Gemba walk', link: '/design/gemba-walk/' },
            { label: 'Code analysis', link: '/design/code-analysis/' },
            { label: 'Bead presentation', link: '/design/bead-presentation/' },
            { label: 'CWD constraint', link: '/design/cwd-constraint/' },
            { label: 'Parallelism boundary', link: '/design/parallelism-boundary/' },
            { label: 'Milestone convention', link: '/design/milestone-convention/' },
            { label: 'Decision process', link: '/design/decision-process/' },
          ],
        },
        {
          label: 'Adapters',
          items: [
            { label: 'Beads', link: '/adaptors/beads/' },
            { label: 'Beads conformance', link: '/adaptors/beads-conformance/' },
            { label: 'Gas Town', link: '/adaptors/gastown/' },
            { label: 'Gas Town conformance', link: '/adaptors/gastown-conformance/' },
            { label: 'Native (terminal)', link: '/adaptors/native/' },
          ],
        },
        {
          label: 'Personas',
          items: [
            { label: 'Project Manager', link: '/personas/project-manager/' },
            { label: 'Documentarian', link: '/personas/documentarian/' },
            { label: 'Deployment Engineer', link: '/personas/deployment-engineer/' },
            { label: 'Persona PPPP', link: '/design/persona-pppp/' },
            { label: 'PM skill (change-this)', link: '/design/pm-skill-change-this/' },
          ],
        },
        {
          label: 'API reference',
          items: [
            { label: 'Overview', link: '/api/overview/' },
          ],
        },
        {
          label: 'Design docs',
          autogenerate: { directory: 'design' },
          collapsed: true,
        },
        {
          label: 'Deployment',
          autogenerate: { directory: 'deployment' },
          collapsed: true,
        },
        {
          label: 'Agents',
          autogenerate: { directory: 'agents' },
          collapsed: true,
        },
        {
          label: 'Prior art & research',
          collapsed: true,
          items: [
            { label: 'Foolery', link: '/prior-art/foolery/' },
            { label: 'vLLM evaluation', link: '/research/vllm-evaluation/' },
          ],
        },
      ],
    }),
  ],
});
