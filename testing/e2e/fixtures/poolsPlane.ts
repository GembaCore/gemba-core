// fixtures/poolsPlane.ts (gm-s47n.18).
//
// Per-test seed for the Pool Dispatch editor surfaces:
//   - GET  /api/pool-config        -> PoolConfigEnvelope
//   - PUT  /api/pool-config        -> PoolConfigEnvelope (echoes the
//     PUT body back as the new state, persisted in this store so a
//     reload sees the saved cfg).
//   - GET  /api/personas           -> PersonaFileListEnvelope
//   - GET  /api/orchestration/state -> OrchestrationState
//
// The store mirrors the wire shapes from web/src/api/poolConfig.ts.
// Keep the small type declarations local so the e2e typecheck does
// not have to resolve the SPA's Vite aliases.

export interface PoolEntryRow {
  size: number;
  agent_type?: string;
  floor: number;
  recycle_after_beads: number;
  idle_ceiling_minutes: number;
  min_interval_per_session_seconds: number;
  max_concurrent: number;
}

export interface PoolConfigJSON {
  default_size: number;
  default_persona?: string;
  default_floor: number;
  reserved_for_manual: number;
  routing?: Record<string, string>;
  pools?: Record<string, Record<string, PoolEntryRow>>;
}

export interface PoolConfigEnvelope {
  path: string;
  body: string;
  parsed: PoolConfigJSON;
  max_parallel: number;
  reserved_for_manual: number;
}

export interface PersonaFile {
  id: string;
  name: string;
  agent_type: string;
  skills?: string[];
}

export type ScopeKind = 'rig' | 'local';

export interface OrchestrationScope {
  id: string;
  kind: ScopeKind;
  personas: string[];
}

export interface OrchestrationPolecat {
  scope: string;
  persona: string;
  name: string;
  state?: string;
}

export interface OrchestrationState {
  adaptor_id: string;
  scopes: OrchestrationScope[];
  agent_types?: string[];
  polecats?: OrchestrationPolecat[];
}

const EMPTY_CFG: PoolConfigJSON = {
  default_size: 0,
  default_persona: '',
  default_floor: 0,
  reserved_for_manual: 0,
  routing: {},
  pools: {},
};

function emptyEnvelope(): PoolConfigEnvelope {
  return {
    path: '/.gemba/pool.toml',
    body: '',
    parsed: { ...EMPTY_CFG },
    max_parallel: 0,
    reserved_for_manual: 0,
  };
}

export interface PoolsPlane {
  /** Seed the GET /api/pool-config response. */
  setConfig(envelope: Partial<PoolConfigEnvelope>): void;
  /** Seed GET /api/personas. */
  setPersonas(personas: PersonaFile[]): void;
  /** Seed GET /api/orchestration/state. Pass null to disable (404). */
  setOrchState(state: OrchestrationState | null): void;
  /** Read-only accessors used by the dispatcher. */
  getConfig(): PoolConfigEnvelope;
  getPersonas(): PersonaFile[];
  getOrchState(): OrchestrationState | null;
  /**
   * Apply a PUT /api/pool-config payload — persists the new parsed
   * config so a subsequent GET round-trips it. Re-renders body to a
   * deterministic synthetic TOML so specs that assert on body contents
   * see something stable.
   */
  applyPut(parsed: PoolConfigJSON): PoolConfigEnvelope;
  /** Wipe to defaults. */
  clear(): void;
}

export function createPoolsPlane(): PoolsPlane {
  let envelope: PoolConfigEnvelope = emptyEnvelope();
  let personas: PersonaFile[] = [];
  let orchState: OrchestrationState | null = null;
  return {
    setConfig(patch) {
      envelope = { ...envelope, ...patch };
    },
    setPersonas(next) {
      personas = [...next];
    },
    setOrchState(next) {
      orchState = next ? { ...next } : null;
    },
    getConfig() {
      return envelope;
    },
    getPersonas() {
      return personas.slice();
    },
    getOrchState() {
      return orchState;
    },
    applyPut(parsed) {
      const body = renderPseudoTOML(parsed);
      envelope = { ...envelope, parsed, body };
      return envelope;
    },
    clear() {
      envelope = emptyEnvelope();
      personas = [];
      orchState = null;
    },
  };
}

// Render a stable string body that round-trips after PUT — the editor
// only displays MaxParallel/path in the header, but the TOML preview
// re-derives from the parsed cfg, so the body string only needs to be
// non-empty for "saved state persisted" assertions.
function renderPseudoTOML(cfg: PoolConfigJSON): string {
  const lines: string[] = [];
  if (cfg.default_persona) lines.push(`default_persona = "${cfg.default_persona}"`);
  if (cfg.default_size) lines.push(`default_size = ${cfg.default_size}`);
  if (cfg.default_floor) lines.push(`default_floor = ${cfg.default_floor}`);
  if (cfg.reserved_for_manual) lines.push(`reserved_for_manual = ${cfg.reserved_for_manual}`);
  for (const [scope, byPersona] of Object.entries(cfg.pools ?? {})) {
    for (const [persona, e] of Object.entries(byPersona)) {
      lines.push(`[pool.${scope}.${persona}]`);
      lines.push(`  size = ${e.size}`);
      if (e.agent_type) lines.push(`  agent_type = "${e.agent_type}"`);
      if (e.floor) lines.push(`  floor = ${e.floor}`);
    }
  }
  return lines.join('\n') + (lines.length ? '\n' : '');
}
