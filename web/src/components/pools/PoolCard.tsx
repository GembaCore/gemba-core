// PoolCard — one (scope, persona) row in the editor's pools list.
// `scopeAxis` controls whether the card surfaces a scope dropdown
// (gt) or hides it (native). gm-s47n.16 spec §5/§6.

import type { PoolEntryRow } from '@/api/poolConfig';

export type ScopeAxis = 'hidden' | 'rig';

export interface PoolCardData {
  scope: string;
  persona: string;
  entry: PoolEntryRow;
}

export interface ValidationIssue {
  severity: 'error' | 'warn';
  message: string;
}

interface Props {
  data: PoolCardData;
  scopeAxis: ScopeAxis;
  scopes: string[];
  personas: string[];
  agentTypes: string[];
  maxParallel: number;
  reservedForManual: number;
  onChange: (next: PoolCardData) => void;
  onRemove: () => void;
  issues: ValidationIssue[];
}

export function PoolCard({
  data,
  scopeAxis,
  scopes,
  personas,
  agentTypes,
  maxParallel,
  reservedForManual,
  onChange,
  onRemove,
  issues,
}: Props) {
  const cap = maxParallel > 0 ? maxParallel - reservedForManual : -1;
  const willClamp = cap >= 0 && data.entry.size > cap;

  function update<K extends keyof PoolEntryRow>(key: K, value: PoolEntryRow[K]) {
    onChange({ ...data, entry: { ...data.entry, [key]: value } });
  }

  return (
    <div
      data-testid={`pool-card-${data.scope}-${data.persona}`}
      className="rounded-md border border-neutral-200 bg-white p-4 shadow-sm dark:border-neutral-800 dark:bg-neutral-900"
    >
      <div className="flex flex-wrap items-center gap-3">
        {scopeAxis === 'rig' && (
          <label className="flex flex-col text-xs">
            <span>Rig</span>
            <select
              data-testid="pool-card-scope"
              value={data.scope}
              onChange={(e) => onChange({ ...data, scope: e.target.value })}
              className="rounded-sm border px-2 py-1"
            >
              {scopes.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
        )}
        <label className="flex flex-col text-xs">
          <span>Persona</span>
          <select
            data-testid="pool-card-persona"
            value={data.persona}
            onChange={(e) => onChange({ ...data, persona: e.target.value })}
            className="rounded-sm border px-2 py-1"
          >
            {personas.length === 0 && <option value="">(no personas)</option>}
            {personas.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col text-xs">
          <span>Agent type</span>
          <select
            data-testid="pool-card-agent-type"
            value={data.entry.agent_type ?? 'claude'}
            onChange={(e) => update('agent_type', e.target.value)}
            className="rounded-sm border px-2 py-1"
          >
            {agentTypes.length === 0 ? (
              <option value="claude">claude</option>
            ) : (
              agentTypes.map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))
            )}
          </select>
        </label>
        <label className="flex flex-col text-xs">
          <span>Size</span>
          <input
            data-testid="pool-card-size"
            type="number"
            min={0}
            value={data.entry.size}
            onChange={(e) => update('size', parseInt(e.target.value, 10) || 0)}
            className="w-20 rounded-sm border px-2 py-1"
          />
        </label>
        <label className="flex flex-col text-xs">
          <span>Floor</span>
          <input
            data-testid="pool-card-floor"
            type="number"
            step={0.1}
            min={0}
            max={1}
            value={data.entry.floor}
            onChange={(e) => update('floor', parseFloat(e.target.value) || 0)}
            className="w-20 rounded-sm border px-2 py-1"
          />
        </label>
        <label className="flex flex-col text-xs">
          <span>Recycle</span>
          <input
            data-testid="pool-card-recycle"
            type="number"
            min={0}
            value={data.entry.recycle_after_beads}
            onChange={(e) =>
              update('recycle_after_beads', parseInt(e.target.value, 10) || 0)
            }
            className="w-20 rounded-sm border px-2 py-1"
          />
        </label>
        <label className="flex flex-col text-xs">
          <span>Idle (min)</span>
          <input
            data-testid="pool-card-idle"
            type="number"
            min={0}
            value={data.entry.idle_ceiling_minutes}
            onChange={(e) =>
              update('idle_ceiling_minutes', parseInt(e.target.value, 10) || 0)
            }
            className="w-20 rounded-sm border px-2 py-1"
          />
        </label>
        <label className="flex flex-col text-xs">
          <span>Min interval (s)</span>
          <input
            data-testid="pool-card-min-interval"
            type="number"
            min={0}
            value={data.entry.min_interval_per_session_seconds}
            onChange={(e) =>
              update(
                'min_interval_per_session_seconds',
                parseInt(e.target.value, 10) || 0
              )
            }
            className="w-24 rounded-sm border px-2 py-1"
          />
        </label>
        <label className="flex flex-col text-xs">
          <span>Max concurrent</span>
          <input
            data-testid="pool-card-max-concurrent"
            type="number"
            min={0}
            value={data.entry.max_concurrent}
            onChange={(e) =>
              update('max_concurrent', parseInt(e.target.value, 10) || 0)
            }
            className="w-20 rounded-sm border px-2 py-1"
          />
        </label>
        <button
          type="button"
          data-testid="pool-card-remove"
          onClick={onRemove}
          className="ml-auto rounded-sm border border-red-300 px-2 py-1 text-xs text-red-700 hover:bg-red-50 dark:border-red-700 dark:text-red-300"
        >
          Remove
        </button>
      </div>
      {willClamp && (
        <div
          data-testid="pool-card-clamp-warning"
          className="mt-2 text-xs text-amber-700 dark:text-amber-400"
        >
          MaxParallel={maxParallel} → effective size will clamp to {cap}
        </div>
      )}
      {issues.length > 0 && (
        <ul className="mt-2 space-y-1">
          {issues.map((iss, i) => (
            <li
              key={i}
              data-testid={`pool-card-issue-${iss.severity}`}
              className={
                iss.severity === 'error'
                  ? 'text-xs text-red-700 dark:text-red-300'
                  : 'text-xs text-amber-700 dark:text-amber-400'
              }
            >
              {iss.message}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
