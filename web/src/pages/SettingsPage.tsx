// SettingsPage (gm-bi5). v1 surface for the
// organization-vs-execution split. Two top-level tabs that mirror the
// conceptual sections on the Capability browser:
//
//   * Organization — team-level/org-level tools shared across projects
//     (what your team already does)
//   * Execution — per-workspace tools doing the actual work here
//     (what happens in this workspace)
//
// v1 content is informational: each tab lists the adaptors that the
// classifier slots into its surface, derived from the same
// /api/capabilities data the rest of the SPA uses. There are no
// mutation affordances — wiring up "Add integration" / "Disconnect"
// is a follow-up.

import { useMemo, useState } from 'react';
import { useCapabilities } from '@/capabilities/context-internal';
import {
  type WorkPlaneManifest,
  type OrchestrationManifest,
} from '@/capabilities/types';
import { classifyAdaptor, type SurfaceKind } from '@/lib/surfaceKind';
import { cn } from '@/lib/utils';

type Tab = SurfaceKind;

interface AdaptorRow {
  name: string;
  plane: 'work' | 'orchestration';
  surface: SurfaceKind;
  manifest: WorkPlaneManifest | OrchestrationManifest;
}

const ORGANIZATION_SUBTITLE = 'Organization surface: what your team already does.';
const EXECUTION_SUBTITLE = 'Execution surface: what happens in this workspace.';

export default function SettingsPage() {
  const { workPlane, orchestrationPlane, loading, error } = useCapabilities();
  const [tab, setTab] = useState<Tab>('organization');

  const rows = useMemo<AdaptorRow[]>(() => {
    const out: AdaptorRow[] = [];
    if (workPlane) {
      out.push({
        name: workPlane.adaptor_name,
        plane: 'work',
        surface: classifyAdaptor(workPlane.adaptor_name),
        manifest: workPlane,
      });
    }
    if (orchestrationPlane) {
      out.push({
        name: orchestrationPlane.adaptor_id,
        plane: 'orchestration',
        surface: classifyAdaptor(orchestrationPlane.adaptor_id),
        manifest: orchestrationPlane,
      });
    }
    return out;
  }, [workPlane, orchestrationPlane]);

  const filtered = rows.filter((r) => r.surface === tab);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
        <h1 className="text-xl font-semibold tracking-tight">Settings</h1>
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          Organization-level tools live alongside the execution-level
          tools that run inside this workspace. Adaptors are derived
          from /api/capabilities and shown read-only in v1.
        </p>
      </header>

      <nav
        className="flex gap-2 border-b border-neutral-200 px-6 dark:border-neutral-800"
        data-testid="settings-tabs"
        role="tablist"
      >
        <TabButton
          active={tab === 'organization'}
          onClick={() => setTab('organization')}
          testid="settings-tab-organization"
        >
          Organization
        </TabButton>
        <TabButton
          active={tab === 'execution'}
          onClick={() => setTab('execution')}
          testid="settings-tab-execution"
        >
          Execution
        </TabButton>
      </nav>

      <div className="min-h-0 flex-1 overflow-auto p-6">
        {error ? (
          <div
            className="rounded-md bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300"
            data-testid="settings-error"
          >
            {error.message}
          </div>
        ) : loading ? (
          <div className="text-sm text-neutral-500" data-testid="settings-loading">
            Loading…
          </div>
        ) : (
          <SurfaceTabPanel surface={tab} rows={filtered} />
        )}
      </div>
    </div>
  );
}

interface TabButtonProps {
  active: boolean;
  testid: string;
  onClick: () => void;
  children: React.ReactNode;
}

function TabButton({ active, testid, onClick, children }: TabButtonProps) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      data-testid={testid}
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-2 border-b-2 px-3 py-2 text-sm font-medium transition-colors',
        active
          ? 'border-neutral-900 text-neutral-900 dark:border-neutral-100 dark:text-neutral-100'
          : 'border-transparent text-neutral-500 hover:text-neutral-700 dark:hover:text-neutral-300'
      )}
    >
      {children}
    </button>
  );
}

interface SurfaceTabPanelProps {
  surface: SurfaceKind;
  rows: AdaptorRow[];
}

function SurfaceTabPanel({ surface, rows }: SurfaceTabPanelProps) {
  const subtitle =
    surface === 'organization' ? ORGANIZATION_SUBTITLE : EXECUTION_SUBTITLE;
  const testid = `settings-panel-${surface}`;
  return (
    <div data-testid={testid} role="tabpanel">
      <section className="rounded-md border border-neutral-200 bg-white p-5 shadow-sm dark:border-neutral-800 dark:bg-neutral-900">
        <h2 className="text-base font-semibold tracking-tight">
          {surface === 'organization' ? 'Organization' : 'Execution'}
        </h2>
        <p className="mt-1 text-xs text-neutral-500 dark:text-neutral-400">
          {subtitle}
        </p>
        <div className="mt-4">
          {rows.length === 0 ? (
            <div
              className="rounded-md border border-dashed border-neutral-300 p-6 text-center text-sm text-neutral-500 dark:border-neutral-700"
              data-testid={`settings-empty-${surface}`}
            >
              {surface === 'organization'
                ? 'No organization adaptors registered.'
                : 'No execution adaptors registered.'}
            </div>
          ) : (
            <ul className="divide-y divide-neutral-200 dark:divide-neutral-800">
              {rows.map((row) => (
                <li
                  key={`${row.plane}:${row.name}`}
                  className="flex items-center gap-3 py-2"
                  data-testid={`settings-adaptor-${row.name}`}
                >
                  <span className="font-mono text-sm">{row.name}</span>
                  <span className="rounded-sm bg-neutral-100 px-1.5 py-0.5 text-[10px] font-mono uppercase text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
                    {row.plane === 'work' ? 'work plane' : 'orchestration plane'}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>
    </div>
  );
}
