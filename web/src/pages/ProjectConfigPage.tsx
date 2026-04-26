// /project/config (gm-uipx.8). Single-scroll page with a sticky
// nav listing 11 sections per ui-spec §5.16. Each section renders
// as an anchored heading + body so the sticky nav's deep links
// resolve to a known scroll position.

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ExternalLink, FileText, Lock, Pencil } from 'lucide-react';
import { listRepositories } from '@/api/repositories';
import { StickyNav } from '@/components/projectconfig/StickyNav';
import {
  ValuesEditor,
  useProjectValues,
} from '@/components/projectconfig/ValuesEditor';
import { type SectionID } from '@/components/projectconfig/types';
import type { Repository } from '@/types/core.gen';

export function ProjectConfigPage(): JSX.Element {
  const [valuesOpen, setValuesOpen] = useState(false);

  return (
    <div data-testid="project-config-page" className="flex h-full min-h-0">
      <StickyNav />
      <main className="min-w-0 flex-1 overflow-y-auto px-6 py-4">
        <header className="mb-4 border-b border-neutral-200 pb-3 dark:border-neutral-800">
          <h1 className="text-lg font-semibold">Project config</h1>
          <p className="text-xs text-neutral-500">
            Single-scroll workspace settings. Per ui-spec §5.16; the
            sticky nav on the left jumps to each section.
          </p>
        </header>
        <ValuesSection onEdit={() => setValuesOpen(true)} />
        <PlaceholderSection id="guardrails" title="Guardrails">
          Capability allow-lists and budget guardrails. Edited in{' '}
          <code>.gemba/workspace.toml</code>.
        </PlaceholderSection>
        <PlaceholderSection id="goals" title="Goals">
          Active workspace goals — surfaces what the team is steering
          toward when the planner suggests work.
        </PlaceholderSection>
        <PlaceholderSection id="personas" title="Personas">
          Coach / Manager roster. Full editor lands in the Personas
          surface (gm-uipx parent epic).
        </PlaceholderSection>
        <AdaptorsSection />
        <PlaceholderSection id="packs" title="Packs">
          Installed Skill packs. Marketplace lives at the Packs
          browser (ui-spec §5.17).
        </PlaceholderSection>
        <PlaceholderSection id="integrations" title="Integrations">
          Outbound integrations — Slack / GitHub / mail.
        </PlaceholderSection>
        <PlaceholderSection id="mode" title="Mode">
          Workspace mode (autonomy posture). Configured via
          <code>.gemba/workspace.toml</code>.
        </PlaceholderSection>
        <PlaceholderSection id="subscriptions" title="Subscriptions">
          Token-spending subscriptions and budget caps.
        </PlaceholderSection>
        <WorkspaceReposSection />
        <PlaceholderSection id="advanced" title="Advanced">
          Diagnostic flags, capability overrides, and other operator-
          rare knobs.
        </PlaceholderSection>
      </main>
      <ValuesEditor open={valuesOpen} onClose={() => setValuesOpen(false)} />
    </div>
  );
}

function SectionHeader({ id, title }: { id: SectionID; title: string }): JSX.Element {
  return (
    <header
      id={`section-${id}`}
      className="scroll-mt-4 border-b border-neutral-200 pb-2 dark:border-neutral-800"
    >
      <h2 className="text-base font-semibold capitalize">{title}</h2>
    </header>
  );
}

function PlaceholderSection({
  id,
  title,
  children,
}: {
  id: SectionID;
  title: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <section
      data-testid={`project-config-section-${id}`}
      data-section={id}
      className="mb-8"
    >
      <SectionHeader id={id} title={title} />
      <p className="mt-3 text-sm text-neutral-600 dark:text-neutral-400">
        {children}
      </p>
    </section>
  );
}

function ValuesSection({ onEdit }: { onEdit: () => void }): JSX.Element {
  const v = useProjectValues();
  const preview = v.values.slice(0, 3);
  return (
    <section
      data-testid="project-config-section-values"
      data-section="values"
      className="mb-8"
    >
      <SectionHeader id="values" title="Values" />
      <div className="mt-3 flex items-center justify-between">
        <p className="text-sm text-neutral-600 dark:text-neutral-400">
          Priority-ranked statements that anchor team decisions. The
          full list lives in the editor; the top three show below.
        </p>
        <button
          type="button"
          data-testid="project-config-values-edit"
          onClick={onEdit}
          className="inline-flex items-center gap-1 rounded border border-neutral-300 bg-white px-2 py-1 text-xs hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
        >
          <Pencil className="h-3 w-3" /> Edit
        </button>
      </div>
      <ol className="mt-3 space-y-1.5 text-sm">
        {preview.length === 0 ? (
          <li
            data-testid="project-config-values-empty"
            className="rounded border border-dashed border-neutral-300 px-3 py-3 text-center text-xs italic text-neutral-500 dark:border-neutral-700"
          >
            No values configured. Click Edit to add the first.
          </li>
        ) : (
          preview.map((value) => (
            <li
              key={value.id}
              data-testid={`project-config-values-preview-${value.id}`}
              className="flex gap-3 rounded border border-neutral-200 bg-white px-3 py-2 dark:border-neutral-800 dark:bg-neutral-950"
            >
              <span className="font-mono text-xs text-neutral-500">{value.rank}</span>
              <span className="flex-1 text-xs">{value.statement}</span>
            </li>
          ))
        )}
      </ol>
    </section>
  );
}

function AdaptorsSection(): JSX.Element {
  return (
    <section
      data-testid="project-config-section-adaptors"
      data-section="adaptors"
      className="mb-8"
    >
      <SectionHeader id="adaptors" title="Adaptors" />
      <div
        data-testid="project-config-adaptors-readonly"
        className="mt-3 flex items-start gap-3 rounded border border-amber-300 bg-amber-50 px-3 py-2 text-xs dark:border-amber-800 dark:bg-amber-950/40"
      >
        <Lock className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-700 dark:text-amber-300" aria-hidden />
        <div>
          <p className="font-semibold text-amber-900 dark:text-amber-200">Read-only</p>
          <p className="mt-1 text-amber-800 dark:text-amber-300/80">
            Adaptor switching is forbidden via UI in v1 (ui-spec §8.1).
            To change adaptors, edit{' '}
            <code className="rounded bg-amber-100 px-1 dark:bg-amber-900/60">
              .gemba/workspace.toml
            </code>{' '}
            and restart <code>gemba serve</code>.
          </p>
          <a
            data-testid="project-config-adaptors-link"
            href="/capabilities"
            className="mt-2 inline-flex items-center gap-1 text-amber-900 underline-offset-2 hover:underline dark:text-amber-200"
          >
            <ExternalLink className="h-3 w-3" />
            View bound adaptors in Capability Browser
          </a>
        </div>
      </div>
    </section>
  );
}

function WorkspaceReposSection(): JSX.Element {
  const q = useQuery({ queryKey: ['repositories'], queryFn: listRepositories });
  return (
    <section
      data-testid="project-config-section-workspace-repos"
      data-section="workspace-repos"
      className="mb-8"
    >
      <SectionHeader id="workspace-repos" title="Workspace repos" />
      <p className="mt-3 text-sm text-neutral-600 dark:text-neutral-400">
        Repositories registered with this workspace. Sourced from{' '}
        <code>.gemba/repositories/&lt;id&gt;.toml</code>.
      </p>
      {q.isLoading ? (
        <p
          data-testid="project-config-repos-loading"
          className="mt-3 text-xs italic text-neutral-500"
        >
          Loading repositories…
        </p>
      ) : null}
      {q.error ? (
        <p
          data-testid="project-config-repos-error"
          className="mt-3 rounded border border-rose-300 bg-rose-50 px-3 py-2 text-xs text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200"
        >
          Failed to load repositories: {String((q.error as Error).message)}
        </p>
      ) : null}
      {q.data?.notice ? (
        <p
          data-testid="project-config-repos-notice"
          className="mt-3 flex items-start gap-2 rounded border border-neutral-200 bg-neutral-50 px-3 py-2 text-xs text-neutral-600 dark:border-neutral-800 dark:bg-neutral-950 dark:text-neutral-400"
        >
          <FileText className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
          {q.data.notice}
        </p>
      ) : null}
      {q.data && q.data.repositories.length > 0 ? (
        <RepoTable repos={q.data.repositories} />
      ) : null}
    </section>
  );
}

function RepoTable({ repos }: { repos: Repository[] }): JSX.Element {
  return (
    <table
      data-testid="project-config-repos-table"
      className="mt-3 w-full table-fixed text-xs"
    >
      <thead>
        <tr className="text-left text-[10px] uppercase tracking-wider text-neutral-500">
          <th className="w-24 px-2 py-1">ID</th>
          <th className="w-20 px-2 py-1">Prefix</th>
          <th className="w-24 px-2 py-1">Branch</th>
          <th className="px-2 py-1">Path</th>
        </tr>
      </thead>
      <tbody>
        {repos.map((r) => (
          <tr
            key={r.id}
            data-testid={`project-config-repo-${r.id}`}
            className="border-t border-neutral-100 font-mono dark:border-neutral-800/60"
          >
            <td className="px-2 py-1.5 text-neutral-800 dark:text-neutral-200">{r.id}</td>
            <td className="px-2 py-1.5 text-neutral-500">{r.bead_prefix}</td>
            <td className="px-2 py-1.5 text-neutral-500">{r.default_branch}</td>
            <td className="px-2 py-1.5 truncate text-neutral-500" title={r.path}>
              {r.path}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
