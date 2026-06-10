// Pool Dispatch editor page (gm-s47n.16, spec §5/§6).
//
// Adaptor-aware: branches on capabilities.orchestrationPlane.adaptor_id.
//   - native     → NativePoolEditor (scope axis hidden)
//   - gastown    → GTPoolEditor (rig axis surfaced; gt-CLI buttons)
//   - undefined  → UnsupportedAdaptorPlaceholder
//
// Routing decision (spec §11 q1): chose the sub-route at /settings/pools
// rather than a tab on /settings, on the spec's slight preference for
// keeping the existing Settings page read-only. The existing tab
// chrome there is shaped for adaptor surface classification, not for
// editing flows; a dedicated route gives this surface room to breathe
// without forcing the existing /settings page into a hybrid mode.

import { useEffect, useState, useMemo, useCallback } from 'react';
import { useCapabilities } from '@/capabilities/context-internal';
import {
  type PoolConfigEnvelope,
  type PoolConfigJSON,
  type PoolEntryRow,
  type PersonaFile,
  type OrchestrationState,
  type SchedulerConfig,
  getPoolConfig,
  putPoolConfig,
  listPersonaFiles,
  getOrchestrationState,
  getSchedulerConfig,
} from '@/api/poolConfig';
import { TOMLPreview } from '@/components/pools/TOMLPreview';
import { PoolCard, type PoolCardData, type ValidationIssue } from '@/components/pools/PoolCard';
import {
  RunCommandModal,
  type ModalField,
} from '@/components/pools/RunCommandModal';

const BEAD_KINDS = ['epic', 'task', 'bug', 'decision', 'feature', 'chore'];
const DEFAULT_AGENT_TYPES = ['claude', 'shell-only'];

function emptyEntry(): PoolEntryRow {
  return {
    size: 1,
    agent_type: 'claude',
    floor: 0,
    recycle_after_beads: 0,
    idle_ceiling_minutes: 0,
    min_interval_per_session_seconds: 0,
    max_concurrent: 0,
  };
}

interface EditorState {
  cfg: PoolConfigJSON;
  cards: PoolCardData[];
}

function envelopeToState(env: PoolConfigEnvelope): EditorState {
  const cards: PoolCardData[] = [];
  if (env.parsed.pools) {
    for (const scope of Object.keys(env.parsed.pools).sort()) {
      const byPersona = env.parsed.pools[scope];
      for (const persona of Object.keys(byPersona).sort()) {
        cards.push({
          scope,
          persona,
          entry: { ...byPersona[persona] },
        });
      }
    }
  }
  return {
    cfg: {
      default_size: env.parsed.default_size ?? 0,
      default_persona: env.parsed.default_persona ?? '',
      default_floor: env.parsed.default_floor ?? 0,
      reserved_for_manual: env.parsed.reserved_for_manual ?? 0,
      routing: { ...(env.parsed.routing ?? {}) },
      pools: {},
    },
    cards,
  };
}

function stateToCfg(s: EditorState): PoolConfigJSON {
  const pools: Record<string, Record<string, PoolEntryRow>> = {};
  for (const c of s.cards) {
    if (!c.scope || !c.persona) continue;
    if (!pools[c.scope]) pools[c.scope] = {};
    pools[c.scope][c.persona] = c.entry;
  }
  return { ...s.cfg, pools };
}

function validate(
  state: EditorState,
  knownPersonas: Set<string>,
  knownAgentTypes: Set<string>
): { perCard: ValidationIssue[][]; global: ValidationIssue[]; canSave: boolean } {
  const perCard: ValidationIssue[][] = state.cards.map(() => []);
  const global: ValidationIssue[] = [];
  let canSave = true;

  if (state.cfg.default_persona && !knownPersonas.has(state.cfg.default_persona)) {
    global.push({
      severity: 'warn',
      message: `default_persona "${state.cfg.default_persona}" not in known personas`,
    });
  }
  if (state.cfg.default_size > 0 && !state.cfg.default_persona) {
    global.push({
      severity: 'warn',
      message:
        'default_size is set but default_persona is empty — no pools will be created without an explicit persona',
    });
  }
  if (state.cfg.reserved_for_manual < 0) {
    global.push({ severity: 'error', message: 'reserved_for_manual must be >= 0' });
    canSave = false;
  }

  // Routing entries.
  for (const [kind, persona] of Object.entries(state.cfg.routing ?? {})) {
    if (!knownPersonas.has(persona)) {
      global.push({
        severity: 'warn',
        message: `routing.${kind} → "${persona}" not in known personas`,
      });
    }
  }

  // Per-card validation + duplicate detection.
  const seen = new Set<string>();
  state.cards.forEach((c, i) => {
    const issues = perCard[i];
    if (!c.persona) {
      issues.push({ severity: 'error', message: 'persona is required' });
      canSave = false;
    } else if (!knownPersonas.has(c.persona)) {
      issues.push({
        severity: 'error',
        message: `persona "${c.persona}" not in known personas`,
      });
      canSave = false;
    }
    const at = c.entry.agent_type ?? 'claude';
    if (knownAgentTypes.size > 0 && !knownAgentTypes.has(at)) {
      issues.push({
        severity: 'error',
        message: `agent_type "${at}" not in registry`,
      });
      canSave = false;
    }
    if (c.entry.size < 0) {
      issues.push({ severity: 'error', message: 'size must be >= 0' });
      canSave = false;
    }
    if (c.entry.floor < 0 || c.entry.floor > 1) {
      issues.push({ severity: 'error', message: 'floor must be in [0, 1]' });
      canSave = false;
    }
    const key = `${c.scope}/${c.persona}`;
    if (seen.has(key)) {
      issues.push({
        severity: 'error',
        message: `duplicate (scope, persona) — already declared`,
      });
      canSave = false;
    }
    seen.add(key);
  });
  return { perCard, global, canSave };
}

export default function PoolsPage() {
  const { orchestrationPlane } = useCapabilities();
  const adaptorID = orchestrationPlane?.adaptor_id ?? '';

  const [env, setEnv] = useState<PoolConfigEnvelope | null>(null);
  const [state, setState] = useState<EditorState | null>(null);
  const [personas, setPersonas] = useState<PersonaFile[]>([]);
  const [orchState, setOrchState] = useState<OrchestrationState | null>(null);
  const [schedulerCfg, setSchedulerCfg] = useState<SchedulerConfig | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveOK, setSaveOK] = useState(false);

  // gt-flow modal state. Only one modal can be open at a time;
  // 'kind' identifies which one. 'null' = closed. The native flow
  // never sets these.
  const [modalKind, setModalKind] = useState<
    null | 'rig' | 'polecat' | 'scheduler'
  >(null);

  // Path-change panel toggle (gm-s47n.19, option b — restart-hint
  // only). The pool.toml path is fixed at server boot via
  // --pool-config; runtime path changes require a restart, so the
  // editor only offers a clipboard-copy of the right flag.
  const [changePathOpen, setChangePathOpen] = useState(false);

  // Refetch the orchestration state — used after a successful gt
  // mutation lands so the rig / polecat lists repopulate.
  const refreshOrchState = useCallback(() => {
    getOrchestrationState()
      .then((s) => setOrchState(s))
      .catch(() => {
        /* tolerate — surfacing this would mask the success notice */
      });
  }, []);

  const refreshSchedulerCfg = useCallback(() => {
    getSchedulerConfig()
      .then((s) => setSchedulerCfg(s))
      .catch(() => {
        /* tolerate; non-gastown returns an empty envelope, errors are non-fatal */
      });
  }, []);

  useEffect(() => {
    let active = true;
    Promise.all([getPoolConfig(), listPersonaFiles()])
      .then(([envRes, personaRes]) => {
        if (!active) return;
        setEnv(envRes);
        setState(envelopeToState(envRes));
        setPersonas(personaRes);
      })
      .catch((e) => active && setLoadError(String(e)));
    if (adaptorID) {
      getOrchestrationState()
        .then((s) => active && setOrchState(s))
        .catch(() => {
          /* tolerate; gt-only */
        });
    }
    if (adaptorID === 'gastown') {
      getSchedulerConfig()
        .then((s) => active && setSchedulerCfg(s))
        .catch(() => {
          /* tolerate */
        });
    }
    return () => {
      active = false;
    };
  }, [adaptorID]);

  const knownPersonas = useMemo(() => new Set(personas.map((p) => p.id)), [personas]);
  const knownAgentTypes = useMemo(() => {
    const fromOrch = new Set(orchState?.agent_types ?? []);
    if (fromOrch.size === 0) DEFAULT_AGENT_TYPES.forEach((a) => fromOrch.add(a));
    return fromOrch;
  }, [orchState]);
  const scopes = useMemo(() => {
    if (adaptorID === 'gastown') return (orchState?.scopes ?? []).map((s) => s.id);
    return ['local'];
  }, [adaptorID, orchState]);

  if (loadError) {
    return (
      <div className="p-6">
        <h1 className="text-xl font-semibold">Pool Dispatch</h1>
        <p className="mt-2 text-sm text-red-700">{loadError}</p>
      </div>
    );
  }
  if (!env || !state) {
    return (
      <div className="p-6 text-sm text-neutral-500" data-testid="pools-page-loading">
        Loading…
      </div>
    );
  }

  if (adaptorID && adaptorID !== 'native' && adaptorID !== 'gastown') {
    return <UnsupportedAdaptor adaptorID={adaptorID} body={env.body} />;
  }

  const { perCard, global, canSave } = validate(state, knownPersonas, knownAgentTypes);
  const scopeAxis = adaptorID === 'gastown' ? 'rig' : 'hidden';
  const previewCfg = stateToCfg(state);

  async function onSave() {
    setSaving(true);
    setSaveOK(false);
    try {
      const next = await putPoolConfig(stateToCfg(state!));
      setEnv(next);
      setState(envelopeToState(next));
      setSaveOK(true);
    } catch (e) {
      setLoadError(String(e));
    } finally {
      setSaving(false);
    }
  }

  function setCfg(patch: Partial<PoolConfigJSON>) {
    setState((s) => (s ? { ...s, cfg: { ...s.cfg, ...patch } } : s));
  }
  function addCard() {
    setState((s) => {
      if (!s) return s;
      const defaultScope = scopes[0] ?? 'local';
      const defaultPersona = personas[0]?.id ?? '';
      return {
        ...s,
        cards: [
          ...s.cards,
          { scope: defaultScope, persona: defaultPersona, entry: emptyEntry() },
        ],
      };
    });
  }
  function updateCard(i: number, next: PoolCardData) {
    setState((s) => {
      if (!s) return s;
      const cards = [...s.cards];
      cards[i] = next;
      return { ...s, cards };
    });
  }
  function removeCard(i: number) {
    setState((s) => {
      if (!s) return s;
      return { ...s, cards: s.cards.filter((_, idx) => idx !== i) };
    });
  }

  // Modal field configurations for the three gt-flow buttons.
  // Validators are kept inline so the field shape stays close to
  // the spec; sharing them across the file would obscure intent.
  const RIG_NAME_RE = /^[A-Za-z0-9_-]+$/;
  const newRigFields: ModalField[] = [
    {
      name: 'name',
      label: 'Rig name',
      placeholder: 'my-rig',
      required: true,
      validate: (v) =>
        RIG_NAME_RE.test(v.trim())
          ? null
          : 'Use letters, numbers, dash or underscore only',
    },
  ];

  const rigOptions =
    orchState?.scopes
      ?.filter((s) => s.kind === 'rig')
      .map((s) => ({ value: s.id, label: s.id })) ?? [];
  const personaOptionsForPolecat = personas.map((p) => ({
    value: p.id,
    label: p.id,
  }));
  const newPolecatFields: ModalField[] = [
    {
      name: 'rig',
      label: 'Rig',
      kind: 'select',
      required: true,
      options: rigOptions,
    },
    {
      name: 'persona',
      label: 'Persona',
      kind: 'select',
      required: true,
      options: personaOptionsForPolecat,
    },
  ];

  const schedulerFields: ModalField[] = [
    {
      name: 'max_polecats',
      label: 'Max polecats',
      kind: 'number',
      required: true,
      initialValue:
        schedulerCfg?.max_polecats != null
          ? String(schedulerCfg.max_polecats)
          : '',
      validate: (v) => {
        const n = Number(v);
        if (!Number.isFinite(n) || !Number.isInteger(n) || n < 1) {
          return 'Must be a positive integer';
        }
        return null;
      },
    },
  ];

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="pools-page">
      <header className="border-b px-6 py-4 dark:border-neutral-800">
        <h1 className="text-xl font-semibold">
          Pool Dispatch · {adaptorID === 'gastown' ? 'Gas Town' : 'Native'}
        </h1>
        <p className="mt-1 text-xs text-neutral-500">
          File: <code>{env.path || '(unset)'}</code> · MaxParallel = {env.max_parallel} ·
          Reserved for manual = {env.reserved_for_manual}
          {' · '}
          <button
            type="button"
            data-testid="change-path-toggle"
            onClick={() => setChangePathOpen((v) => !v)}
            className="text-blue-600 underline-offset-2 hover:underline dark:text-blue-400"
          >
            Change path
          </button>
        </p>
        {changePathOpen && (
          <ChangePathPanel
            currentPath={env.path}
            onClose={() => setChangePathOpen(false)}
          />
        )}
      </header>

      <div className="flex min-h-0 flex-1 gap-6 overflow-auto p-6">
        <div className="flex-1 space-y-6">
          {personas.length === 0 && <NoPersonasHelper />}
          {adaptorID === 'gastown' && (
            <Section title="Imported from gt">
              <div className="space-y-3 text-sm">
                <div data-testid="gt-rigs-list">
                  <span className="text-xs font-medium text-neutral-500 dark:text-neutral-400">
                    Rigs:
                  </span>{' '}
                  {(orchState?.scopes ?? [])
                    .filter((s) => s.kind === 'rig')
                    .map((s) => s.id)
                    .join(' · ') || '(none)'}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    data-testid="gt-new-rig"
                    onClick={() => setModalKind('rig')}
                    className="rounded-sm border px-3 py-1 text-xs hover:bg-neutral-50 dark:border-neutral-700 dark:hover:bg-neutral-800"
                  >
                    + New rig
                  </button>
                  <button
                    type="button"
                    data-testid="gt-new-polecat"
                    onClick={() => setModalKind('polecat')}
                    className="rounded-sm border px-3 py-1 text-xs hover:bg-neutral-50 dark:border-neutral-700 dark:hover:bg-neutral-800"
                  >
                    + New polecat
                  </button>
                  <button
                    type="button"
                    data-testid="gt-edit-scheduler"
                    onClick={() => setModalKind('scheduler')}
                    className="rounded-sm border px-3 py-1 text-xs hover:bg-neutral-50 dark:border-neutral-700 dark:hover:bg-neutral-800"
                  >
                    Edit gt scheduler
                    {schedulerCfg?.max_polecats != null &&
                      schedulerCfg.max_polecats > 0 && (
                        <span className="ml-1 text-neutral-400">
                          (max_polecats={schedulerCfg.max_polecats})
                        </span>
                      )}
                  </button>
                </div>
              </div>
            </Section>
          )}
          <Section title="Server defaults">
            <div className="grid grid-cols-2 gap-3 text-sm">
              <label className="flex flex-col">
                <span className="text-xs">Default persona</span>
                <select
                  data-testid="default-persona"
                  value={state.cfg.default_persona ?? ''}
                  onChange={(e) => setCfg({ default_persona: e.target.value })}
                  className="rounded-sm border px-2 py-1"
                >
                  <option value="">(none)</option>
                  {personas.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.id}
                    </option>
                  ))}
                </select>
              </label>
              <label className="flex flex-col">
                <span className="text-xs">Default size</span>
                <input
                  data-testid="default-size"
                  type="number"
                  min={0}
                  value={state.cfg.default_size}
                  onChange={(e) =>
                    setCfg({ default_size: parseInt(e.target.value, 10) || 0 })
                  }
                  className="rounded-sm border px-2 py-1"
                />
              </label>
              <label className="flex flex-col">
                <span className="text-xs">Default floor</span>
                <input
                  data-testid="default-floor"
                  type="number"
                  step={0.1}
                  min={0}
                  max={1}
                  value={state.cfg.default_floor}
                  onChange={(e) =>
                    setCfg({ default_floor: parseFloat(e.target.value) || 0 })
                  }
                  className="rounded-sm border px-2 py-1"
                />
              </label>
              <label className="flex flex-col">
                <span className="text-xs">Reserved for manual</span>
                <input
                  data-testid="reserved-for-manual"
                  type="number"
                  min={0}
                  value={state.cfg.reserved_for_manual}
                  onChange={(e) =>
                    setCfg({ reserved_for_manual: parseInt(e.target.value, 10) || 0 })
                  }
                  className="rounded-sm border px-2 py-1"
                />
              </label>
            </div>
          </Section>

          <Section title="Routing">
            <div className="space-y-2">
              {BEAD_KINDS.map((kind) => (
                <div
                  key={kind}
                  className="flex items-center gap-3 text-sm"
                  data-testid={`routing-row-${kind}`}
                >
                  <span className="w-20 font-mono text-xs">{kind}</span>
                  <span className="text-neutral-400">→</span>
                  <select
                    value={state.cfg.routing?.[kind] ?? ''}
                    onChange={(e) => {
                      const next = { ...(state.cfg.routing ?? {}) };
                      if (e.target.value) next[kind] = e.target.value;
                      else delete next[kind];
                      setCfg({ routing: next });
                    }}
                    className="rounded-sm border px-2 py-1"
                  >
                    <option value="">(unset)</option>
                    {personas.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.id}
                      </option>
                    ))}
                  </select>
                </div>
              ))}
            </div>
          </Section>

          <Section title="Pools">
            <div className="space-y-3">
              {state.cards.map((c, i) => (
                <PoolCard
                  key={`${c.scope}-${c.persona}-${i}`}
                  data={c}
                  scopeAxis={scopeAxis as 'hidden' | 'rig'}
                  scopes={scopes}
                  personas={personas.map((p) => p.id)}
                  agentTypes={[...knownAgentTypes]}
                  maxParallel={env.max_parallel}
                  reservedForManual={env.reserved_for_manual}
                  onChange={(next) => updateCard(i, next)}
                  onRemove={() => removeCard(i)}
                  issues={perCard[i]}
                />
              ))}
              <button
                type="button"
                data-testid="pool-add-card"
                onClick={addCard}
                className="rounded-sm border border-dashed px-3 py-2 text-sm text-neutral-600 hover:bg-neutral-50 dark:border-neutral-700 dark:text-neutral-300"
              >
                + Add pool
              </button>
            </div>
          </Section>

          {global.length > 0 && (
            <ul data-testid="pools-global-issues" className="space-y-1">
              {global.map((iss, i) => (
                <li
                  key={i}
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

          <div className="flex items-center gap-3">
            <button
              type="button"
              data-testid="pools-save"
              disabled={!canSave || saving}
              onClick={onSave}
              className="rounded-sm bg-blue-600 px-4 py-2 text-sm font-semibold text-white disabled:cursor-not-allowed disabled:opacity-50"
            >
              {saving ? 'Saving…' : 'Save'}
            </button>
            {saveOK && (
              <span
                data-testid="pools-restart-banner"
                className="text-xs text-amber-700 dark:text-amber-400"
              >
                ⚠ Restart server to apply
              </span>
            )}
          </div>
        </div>

        <aside className="w-[28rem] flex-none">
          <div className="sticky top-0 space-y-2">
            <h2 className="text-sm font-semibold">Generated TOML</h2>
            <TOMLPreview cfg={previewCfg} />
          </div>
        </aside>
      </div>

      {/* gt-flow modals — mounted regardless of adaptor; the
          buttons that open them are only rendered for gastown so
          they're inaccessible elsewhere. */}
      <RunCommandModal
        open={modalKind === 'rig'}
        title="Create rig"
        description="Shells out to gt rig create on the gemba server."
        command="gt rig create <name>"
        fields={newRigFields}
        endpoint="/orchestration/scopes"
        method="POST"
        onConfirm={refreshOrchState}
        onClose={() => setModalKind(null)}
      />
      <RunCommandModal
        open={modalKind === 'polecat'}
        title="Create polecat"
        description="Shells out to gt polecat create on the gemba server."
        command="gt polecat create <rig> <persona>"
        fields={newPolecatFields}
        endpoint="/orchestration/polecats"
        method="POST"
        onConfirm={refreshOrchState}
        onClose={() => setModalKind(null)}
      />
      <RunCommandModal
        open={modalKind === 'scheduler'}
        title="gt scheduler config"
        description="Shells out to gt config set scheduler.max_polecats."
        command="gt config set scheduler.max_polecats <max_polecats>"
        fields={schedulerFields}
        endpoint="/orchestration/scheduler-config"
        method="PUT"
        // The PUT endpoint expects {max_polecats: int}, but our
        // generic field-to-body packer would emit a string. Coerce
        // here so the server's strict JSON decode succeeds.
        bodyTransform={(values) => ({
          max_polecats: parseInt(values.max_polecats ?? '0', 10) || 0,
        })}
        onConfirm={refreshSchedulerCfg}
        onClose={() => setModalKind(null)}
      />
    </div>
  );
}

// NoPersonasHelper (gm-root.26 item 3). Fresh project + /settings/pools
// today shows an empty persona dropdown, an empty pools list, and no
// guidance. Pools dispatch to personas, so without at least one
// persona on disk every dropdown in the editor is dead. This helper
// renders only when `personas.length === 0` and is suppressed once
// any persona file exists. Becomes a no-op once gm-root.24's project
// ratify auto-seeds personas — at that point fresh projects always
// boot with an "engineer" persona on disk, so this branch never fires.
function NoPersonasHelper() {
  const SNIPPET = `# .gemba/personas/engineer.toml
id = "engineer"
name = "Engineer"
agent_type = "claude"
system_prompt = "You are a software engineer..."

[[skills]]
id = "default"
`;
  const [copied, setCopied] = useState(false);
  function onCopy() {
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      void navigator.clipboard.writeText(SNIPPET).then(
        () => {
          setCopied(true);
          window.setTimeout(() => setCopied(false), 2000);
        },
        () => {
          /* clipboard API rejected — leave the snippet on screen */
        }
      );
    }
  }
  return (
    <Section title="No personas configured">
      <div className="space-y-3 text-sm" data-testid="pools-no-personas-helper">
        <p className="text-neutral-700 dark:text-neutral-300">
          Pools dispatch to personas. Drop a TOML file into{' '}
          <code className="font-mono text-xs">.gemba/personas/&lt;name&gt;.toml</code>{' '}
          and reload.
        </p>
        <pre
          data-testid="pools-no-personas-snippet"
          className="overflow-x-auto rounded-md bg-neutral-100 p-3 text-xs dark:bg-neutral-900"
        >
          {SNIPPET}
        </pre>
        <button
          type="button"
          data-testid="pools-no-personas-copy"
          onClick={onCopy}
          className="rounded-sm border px-3 py-1 text-xs hover:bg-neutral-50 dark:border-neutral-700 dark:hover:bg-neutral-800"
        >
          {copied ? 'Copied' : 'Copy snippet'}
        </button>
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          Once <code className="font-mono">gm-root.24</code> ships (project
          ratify auto-seeds personas), fresh projects will skip this step.
        </p>
      </div>
    </Section>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section
      className="rounded-md border border-neutral-200 bg-white p-4 shadow-sm dark:border-neutral-800 dark:bg-neutral-900"
      data-testid={`pools-section-${title.replace(/ /g, '-').toLowerCase()}`}
    >
      <h2 className="text-sm font-semibold">{title}</h2>
      <div className="mt-3">{children}</div>
    </section>
  );
}

function UnsupportedAdaptor({ adaptorID, body }: { adaptorID: string; body: string }) {
  return (
    <div className="p-6" data-testid="pools-unsupported-adaptor">
      <h1 className="text-xl font-semibold">Pool Dispatch</h1>
      <p className="mt-2 text-sm text-neutral-500">
        Adaptor <code>{adaptorID}</code> is not supported by the structured editor.
        Edit pool.toml directly:
      </p>
      <pre className="mt-3 overflow-x-auto rounded-md bg-neutral-100 p-3 text-xs dark:bg-neutral-900">
        {body || '(empty)'}
      </pre>
    </div>
  );
}

// ChangePathPanel — gm-s47n.19 option (b). The pool.toml path is
// fixed at server boot (--pool-config flag); runtime path changes
// would require restarting the daemon. Rather than write to a
// different file at runtime (which would diverge "saved file" from
// "running config" — the option-a footgun), we surface a copy-the-
// restart-flag affordance and let the operator do the restart.
function ChangePathPanel({
  currentPath,
  onClose,
}: {
  currentPath: string;
  onClose: () => void;
}) {
  const [path, setPath] = useState(currentPath);
  const [copied, setCopied] = useState(false);
  const flag = `--pool-config ${path || '<path>'}`;

  const onCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(flag);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access denied (insecure context, browser policy).
      // Surface that to the operator so they can copy manually from
      // the pre block above.
      setCopied(false);
    }
  }, [flag]);

  return (
    <div
      className="mt-3 rounded-md border border-neutral-200 bg-neutral-50 p-3 text-xs dark:border-neutral-700 dark:bg-neutral-900"
      data-testid="change-path-panel"
    >
      <div className="flex items-start gap-3">
        <div className="flex-1 space-y-2">
          <label className="block">
            <span className="text-[11px] font-medium text-neutral-500 dark:text-neutral-400">
              New path:
            </span>
            <input
              type="text"
              value={path}
              data-testid="change-path-input"
              onChange={(e) => setPath(e.target.value)}
              placeholder="/path/to/new/pool.toml"
              className="mt-1 w-full rounded-sm border border-neutral-300 bg-white px-2 py-1 font-mono text-xs dark:border-neutral-700 dark:bg-neutral-950"
            />
          </label>
          <pre
            data-testid="change-path-flag-preview"
            className="overflow-x-auto rounded-sm bg-white px-2 py-1 font-mono text-[11px] dark:bg-neutral-950"
          >
            {flag}
          </pre>
          <p className="text-[11px] text-neutral-500 dark:text-neutral-400">
            Stop your server, then restart with this flag in place of
            the current <code>--pool-config</code> (preserve your other
            flags). Hot-reload is filed as a follow-up.
          </p>
        </div>
        <div className="flex flex-col gap-1">
          <button
            type="button"
            data-testid="change-path-copy"
            onClick={onCopy}
            disabled={!path}
            className="rounded-sm border border-neutral-300 bg-white px-3 py-1 text-[11px] hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-950 dark:hover:bg-neutral-800"
          >
            {copied ? 'Copied ✓' : 'Copy flag'}
          </button>
          <button
            type="button"
            data-testid="change-path-close"
            onClick={onClose}
            className="rounded-sm px-3 py-1 text-[11px] text-neutral-500 hover:bg-neutral-100 dark:hover:bg-neutral-800"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
