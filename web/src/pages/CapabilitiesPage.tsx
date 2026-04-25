// CapabilitiesPage (gm-e12.14). Per-adaptor browser of every wire-format
// detail the SPA negotiates on: transport, version, declared
// capabilities, state map, edge / field / relationship extensions, and
// conformance flags. Read-only diagnostic surface — pulls the same data
// /api/capabilities serves to the rest of the SPA, plus live health
// from the /api/adaptors/stream cache the AdaptorBanner already
// maintains.
//
// Designed as a "tab per plane" so a workspace with both work and
// orchestration adaptors gets balanced billing without the
// orchestration view stealing the spotlight when the work plane is the
// thing degrading.
//
// No mutations on this page. Operators looking to redeploy or restart
// adaptors do that out-of-band; this view is forensic.

import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useCapabilities } from '@/capabilities/context-internal';
import {
  workPlaneFlags,
  FLAG_NAMES,
  type Flags,
  type FlagName,
  type WorkPlaneManifest,
  type OrchestrationManifest,
  type EdgeExtension,
  type FieldExtension,
  type RelationshipExtension,
} from '@/capabilities/types';
import { cn } from '@/lib/utils';

type AdaptorStatus = {
  name: string;
  plane: 'work' | 'orchestration';
  healthy: boolean;
  reason?: string;
};

type AdaptorsResponse = {
  adaptors: AdaptorStatus[];
};

// useAdaptorsHealth reads the cache the AdaptorBanner's SSE pump
// already feeds. We don't own the subscription — just consume what's
// there. Returns undefined when the banner hasn't run yet (rare in
// practice; the banner mounts in the app shell).
function useAdaptorsHealth(): AdaptorStatus[] | undefined {
  const { data } = useQuery<AdaptorsResponse>({
    queryKey: ['adaptors'],
    enabled: false, // SSE pump is the writer; we just read.
  });
  return data?.adaptors;
}

type Plane = 'work' | 'orchestration';

export default function CapabilitiesPage() {
  const { workPlane, orchestrationPlane, loading, error } = useCapabilities();
  const adaptors = useAdaptorsHealth();
  const [activePlane, setActivePlane] = useState<Plane>('work');

  // Default-select the first plane that's actually present so a
  // single-plane workspace doesn't stare at "no adaptor" until the
  // operator clicks.
  const initialPlane: Plane = workPlane
    ? 'work'
    : orchestrationPlane
      ? 'orchestration'
      : 'work';
  const plane = activePlane === 'work' && !workPlane && orchestrationPlane
    ? 'orchestration'
    : activePlane === 'orchestration' && !orchestrationPlane && workPlane
      ? 'work'
      : (activePlane ?? initialPlane);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
        <h1 className="text-xl font-semibold tracking-tight">Adaptor capabilities</h1>
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          Capability manifests for every registered adaptor. Read-only
          diagnostic view; matches what /api/capabilities serves to the
          rest of the SPA.
        </p>
      </header>

      {error ? (
        <div
          className="m-6 rounded-md bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300"
          data-testid="capabilities-error"
        >
          {error.message}
        </div>
      ) : loading ? (
        <div className="p-6 text-sm text-neutral-500" data-testid="capabilities-loading">
          Loading…
        </div>
      ) : !workPlane && !orchestrationPlane ? (
        <div
          className="m-6 rounded-md border border-dashed border-neutral-300 p-8 text-center text-sm text-neutral-500 dark:border-neutral-700"
          data-testid="capabilities-empty"
        >
          No adaptors registered. Wire up a WorkPlane or
          OrchestrationPlane via <code>gemba serve</code> flags.
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-auto p-6">
          <PlaneTabs
            plane={plane}
            workPlane={workPlane}
            orchestrationPlane={orchestrationPlane}
            onChange={setActivePlane}
          />

          {plane === 'work' && workPlane ? (
            <WorkPlaneCard manifest={workPlane} health={findHealth(adaptors, workPlane.adaptor_name, 'work')} />
          ) : null}

          {plane === 'orchestration' && orchestrationPlane ? (
            <OrchestrationPlaneCard manifest={orchestrationPlane} health={findHealth(adaptors, orchestrationPlane.adaptor_id, 'orchestration')} />
          ) : null}

          {plane === 'work' && !workPlane ? (
            <PlaneAbsent label="WorkPlane" />
          ) : null}
          {plane === 'orchestration' && !orchestrationPlane ? (
            <PlaneAbsent label="OrchestrationPlane" />
          ) : null}
        </div>
      )}
    </div>
  );
}

function findHealth(
  adaptors: AdaptorStatus[] | undefined,
  name: string,
  plane: 'work' | 'orchestration'
): AdaptorStatus | undefined {
  return adaptors?.find((a) => a.plane === plane && a.name === name);
}

interface PlaneTabsProps {
  plane: Plane;
  workPlane: WorkPlaneManifest | null;
  orchestrationPlane: OrchestrationManifest | null;
  onChange: (p: Plane) => void;
}

function PlaneTabs({ plane, workPlane, orchestrationPlane, onChange }: PlaneTabsProps) {
  return (
    <nav
      className="mb-4 flex gap-2 border-b border-neutral-200 dark:border-neutral-800"
      data-testid="capabilities-plane-tabs"
    >
      <TabButton
        active={plane === 'work'}
        disabled={!workPlane}
        onClick={() => onChange('work')}
        testid="tab-work-plane"
      >
        Work plane{workPlane ? <PaneTag>{workPlane.adaptor_name}</PaneTag> : null}
      </TabButton>
      <TabButton
        active={plane === 'orchestration'}
        disabled={!orchestrationPlane}
        onClick={() => onChange('orchestration')}
        testid="tab-orchestration-plane"
      >
        Orchestration plane
        {orchestrationPlane ? <PaneTag>{orchestrationPlane.adaptor_id}</PaneTag> : null}
      </TabButton>
    </nav>
  );
}

interface TabButtonProps {
  active: boolean;
  disabled?: boolean;
  testid: string;
  onClick: () => void;
  children: React.ReactNode;
}

function TabButton({ active, disabled, testid, onClick, children }: TabButtonProps) {
  return (
    <button
      type="button"
      data-testid={testid}
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'inline-flex items-center gap-2 border-b-2 px-3 py-2 text-sm font-medium transition-colors',
        active
          ? 'border-neutral-900 text-neutral-900 dark:border-neutral-100 dark:text-neutral-100'
          : 'border-transparent text-neutral-500 hover:text-neutral-700 dark:hover:text-neutral-300',
        disabled && 'cursor-not-allowed text-neutral-300 hover:text-neutral-300 dark:text-neutral-700 dark:hover:text-neutral-700'
      )}
    >
      {children}
    </button>
  );
}

function PaneTag({ children }: { children: React.ReactNode }) {
  return (
    <span className="rounded-sm bg-neutral-100 px-1.5 py-0.5 text-[10px] font-mono uppercase text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
      {children}
    </span>
  );
}

function PlaneAbsent({ label }: { label: string }) {
  return (
    <div
      className="rounded-md border border-dashed border-neutral-300 p-8 text-center text-sm text-neutral-500 dark:border-neutral-700"
      data-testid="capabilities-plane-absent"
    >
      No {label} adaptor registered.
    </div>
  );
}

interface WorkPlaneCardProps {
  manifest: WorkPlaneManifest;
  health?: AdaptorStatus;
}

function WorkPlaneCard({ manifest, health }: WorkPlaneCardProps) {
  const flags = useMemo<Flags | null>(() => workPlaneFlags(manifest), [manifest]);
  return (
    <section
      className="rounded-md border border-neutral-200 bg-white shadow-sm dark:border-neutral-800 dark:bg-neutral-900"
      data-testid="work-plane-card"
    >
      <CardHeader
        title={manifest.adaptor_name}
        subtitle={`v${manifest.adaptor_version} · protocol ${manifest.protocol_version} · transport ${manifest.transport}`}
        health={health}
        readOnly={Boolean(manifest.read_only)}
      />
      <div className="space-y-6 px-5 py-4">
        <Section title="State map" testid="state-map">
          <KVTable
            rows={Object.entries(manifest.state_map).sort((a, b) => a[0].localeCompare(b[0]))}
            keyHeader="Native status"
            valueHeader="Core category"
          />
        </Section>

        {manifest.edge_extensions?.length ? (
          <Section title="Edge extensions" testid="edge-extensions">
            <EdgeExtensionsTable rows={manifest.edge_extensions} />
          </Section>
        ) : null}

        {manifest.field_extensions?.length ? (
          <Section title="Field extensions" testid="field-extensions">
            <FieldExtensionsTable rows={manifest.field_extensions} />
          </Section>
        ) : null}

        {manifest.relationship_extensions?.length ? (
          <Section title="Relationship extensions" testid="relationship-extensions">
            <RelationshipExtensionsTable rows={manifest.relationship_extensions} />
          </Section>
        ) : null}

        <Section title="Conformance flags" testid="conformance-flags">
          <FlagsGrid flags={flags} />
        </Section>

        {manifest.description_format ? (
          <Section title="Description format" testid="description-format">
            <code className="rounded bg-neutral-100 px-1.5 py-0.5 text-xs dark:bg-neutral-800">
              {manifest.description_format}
            </code>
          </Section>
        ) : null}
      </div>
    </section>
  );
}

interface OrchestrationPlaneCardProps {
  manifest: OrchestrationManifest;
  health?: AdaptorStatus;
}

function OrchestrationPlaneCard({ manifest, health }: OrchestrationPlaneCardProps) {
  return (
    <section
      className="rounded-md border border-neutral-200 bg-white shadow-sm dark:border-neutral-800 dark:bg-neutral-900"
      data-testid="orchestration-plane-card"
    >
      <CardHeader
        title={manifest.adaptor_id}
        subtitle={`v${manifest.adaptor_version} · api ${manifest.orchestration_api_version} · transport ${manifest.transport}`}
        health={health}
      />
      <div className="space-y-6 px-5 py-4">
        <Section title="Workspace kinds" testid="workspace-kinds">
          <ChipList values={manifest.workspace_kinds} highlight={manifest.default_workspace_kind} />
        </Section>

        <Section title="Group modes" testid="group-modes">
          <ChipList values={manifest.group_modes} />
        </Section>

        <Section title="Cost axes" testid="cost-axes">
          <ChipList values={manifest.cost_axes} />
          {manifest.native_cost_unit ? (
            <p className="mt-1 text-xs text-neutral-500">
              native unit: <code className="rounded bg-neutral-100 px-1 dark:bg-neutral-800">{manifest.native_cost_unit}</code>
              {manifest.native_cost_to_dollars
                ? ` (≈ $${manifest.native_cost_to_dollars}/unit)`
                : ''}
            </p>
          ) : null}
        </Section>

        <Section title="Escalation kinds" testid="escalation-kinds">
          <ChipList values={manifest.escalation_kinds} />
        </Section>

        <Section title="Peek modes" testid="peek-modes">
          <ChipList values={manifest.peek_modes} />
        </Section>

        {manifest.event_delivery ? (
          <Section title="Event delivery" testid="event-delivery">
            <code className="rounded bg-neutral-100 px-1.5 py-0.5 text-xs dark:bg-neutral-800">
              {manifest.event_delivery}
            </code>
          </Section>
        ) : null}
      </div>
    </section>
  );
}

interface CardHeaderProps {
  title: string;
  subtitle: string;
  health?: AdaptorStatus;
  readOnly?: boolean;
}

function CardHeader({ title, subtitle, health, readOnly }: CardHeaderProps) {
  return (
    <header className="flex flex-wrap items-center gap-3 border-b border-neutral-200 bg-neutral-50 px-5 py-3 dark:border-neutral-800 dark:bg-neutral-900/50">
      <h2 className="text-base font-semibold tracking-tight">{title}</h2>
      <span className="text-xs text-neutral-500">{subtitle}</span>
      <span className="ml-auto flex items-center gap-2">
        {readOnly ? <Tag tone="neutral">read-only</Tag> : null}
        {health ? (
          <Tag tone={health.healthy ? 'green' : 'red'} testid="adaptor-health">
            {health.healthy ? 'healthy' : 'degraded'}
          </Tag>
        ) : (
          <Tag tone="neutral">status unknown</Tag>
        )}
      </span>
    </header>
  );
}

interface SectionProps {
  title: string;
  testid?: string;
  children: React.ReactNode;
}

function Section({ title, testid, children }: SectionProps) {
  return (
    <div data-testid={testid}>
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider text-neutral-500">
        {title}
      </h3>
      {children}
    </div>
  );
}

interface KVTableProps {
  rows: [string, string][];
  keyHeader: string;
  valueHeader: string;
}

function KVTable({ rows, keyHeader, valueHeader }: KVTableProps) {
  return (
    <table className="w-full text-sm">
      <thead className="text-left text-xs font-medium uppercase tracking-wider text-neutral-500">
        <tr>
          <th className="px-2 py-1">{keyHeader}</th>
          <th className="px-2 py-1">{valueHeader}</th>
        </tr>
      </thead>
      <tbody>
        {rows.map(([k, v]) => (
          <tr key={k} className="border-t border-neutral-100 dark:border-neutral-800">
            <td className="px-2 py-1 font-mono text-xs">{k}</td>
            <td className="px-2 py-1 font-mono text-xs">{v}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function EdgeExtensionsTable({ rows }: { rows: EdgeExtension[] }) {
  return (
    <table className="w-full text-sm">
      <thead className="text-left text-xs font-medium uppercase tracking-wider text-neutral-500">
        <tr>
          <th className="px-2 py-1">Name</th>
          <th className="px-2 py-1">Directed</th>
          <th className="px-2 py-1">Inverse</th>
          <th className="px-2 py-1">Description</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((e) => (
          <tr key={e.name} className="border-t border-neutral-100 dark:border-neutral-800">
            <td className="px-2 py-1 font-mono text-xs">{e.name}</td>
            <td className="px-2 py-1 text-xs">{e.directed ? '→' : '↔'}</td>
            <td className="px-2 py-1 font-mono text-xs">{e.inverse ?? '—'}</td>
            <td className="px-2 py-1 text-xs text-neutral-600 dark:text-neutral-400">
              {e.description ?? ''}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function FieldExtensionsTable({ rows }: { rows: FieldExtension[] }) {
  return (
    <table className="w-full text-sm">
      <thead className="text-left text-xs font-medium uppercase tracking-wider text-neutral-500">
        <tr>
          <th className="px-2 py-1">Name</th>
          <th className="px-2 py-1">Type</th>
          <th className="px-2 py-1">Description</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((f) => (
          <tr key={f.name} className="border-t border-neutral-100 dark:border-neutral-800">
            <td className="px-2 py-1 font-mono text-xs">{f.name}</td>
            <td className="px-2 py-1 font-mono text-xs">{f.type}</td>
            <td className="px-2 py-1 text-xs text-neutral-600 dark:text-neutral-400">
              {f.description ?? ''}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function RelationshipExtensionsTable({ rows }: { rows: RelationshipExtension[] }) {
  return (
    <table className="w-full text-sm">
      <thead className="text-left text-xs font-medium uppercase tracking-wider text-neutral-500">
        <tr>
          <th className="px-2 py-1">Name</th>
          <th className="px-2 py-1">Type</th>
          <th className="px-2 py-1">Description</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.name} className="border-t border-neutral-100 dark:border-neutral-800">
            <td className="px-2 py-1 font-mono text-xs">{r.name}</td>
            <td className="px-2 py-1 font-mono text-xs">{r.type}</td>
            <td className="px-2 py-1 text-xs text-neutral-600 dark:text-neutral-400">
              {r.description ?? ''}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function FlagsGrid({ flags }: { flags: Flags | null }) {
  if (!flags) {
    return (
      <p className="text-xs text-neutral-500">
        No flags derived (manifest absent).
      </p>
    );
  }
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
      {FLAG_NAMES.map((name) => (
        <FlagCell key={name} name={name} value={flags[name as FlagName]} />
      ))}
    </div>
  );
}

function FlagCell({ name, value }: { name: string; value: boolean }) {
  return (
    <div
      className={cn(
        'flex items-center justify-between rounded border px-2 py-1 text-xs',
        value
          ? 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300'
          : 'border-neutral-200 bg-neutral-50 text-neutral-600 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-400'
      )}
    >
      <span className="truncate font-mono">{name}</span>
      <span className="ml-2 font-semibold">{value ? '✓' : '·'}</span>
    </div>
  );
}

interface ChipListProps {
  values: string[];
  highlight?: string;
}

function ChipList({ values, highlight }: ChipListProps) {
  if (values.length === 0) {
    return <span className="text-xs text-neutral-500">none</span>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {values.map((v) => (
        <span
          key={v}
          className={cn(
            'rounded-sm px-1.5 py-0.5 text-[11px] font-mono',
            v === highlight
              ? 'bg-neutral-900 text-neutral-50 dark:bg-neutral-100 dark:text-neutral-900'
              : 'bg-neutral-100 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300'
          )}
        >
          {v}
          {v === highlight ? ' (default)' : ''}
        </span>
      ))}
    </div>
  );
}

interface TagProps {
  tone: 'green' | 'red' | 'neutral';
  testid?: string;
  children: React.ReactNode;
}

function Tag({ tone, testid, children }: TagProps) {
  return (
    <span
      data-testid={testid}
      className={cn(
        'rounded-sm px-1.5 py-0.5 text-[10px] font-mono uppercase',
        tone === 'green' && 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300',
        tone === 'red' && 'bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300',
        tone === 'neutral' && 'bg-neutral-100 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300'
      )}
    >
      {children}
    </span>
  );
}
