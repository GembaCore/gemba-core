// TOMLPreview — renders a parsed PoolConfig back into TOML text for
// the editor's live preview pane. Mirrors internal/config/pool.go's
// EncodePoolConfig key-order so a SPA-rendered preview matches what
// the server will write on Save (gm-s47n.16, spec §5/§6).

import type { PoolConfigJSON } from '@/api/poolConfig';

export function renderTOML(cfg: PoolConfigJSON): string {
  const parts: string[] = ['[pool]\n'];
  if (cfg.default_size) parts.push(`default_size = ${cfg.default_size}\n`);
  if (cfg.default_persona)
    parts.push(`default_persona = ${quote(cfg.default_persona)}\n`);
  if (cfg.default_floor) parts.push(`default_floor = ${cfg.default_floor}\n`);
  if (cfg.reserved_for_manual)
    parts.push(`reserved_for_manual = ${cfg.reserved_for_manual}\n`);
  if (cfg.routing && Object.keys(cfg.routing).length > 0) {
    parts.push('\n[pool.routing]\n');
    for (const k of Object.keys(cfg.routing).sort()) {
      parts.push(`${k} = ${quote(cfg.routing[k])}\n`);
    }
  }
  if (cfg.pools) {
    for (const scope of Object.keys(cfg.pools).sort()) {
      const byPersona = cfg.pools[scope];
      for (const persona of Object.keys(byPersona).sort()) {
        const e = byPersona[persona];
        parts.push(`\n[pool.${scope}.${persona}]\n`);
        if (e.size) parts.push(`size = ${e.size}\n`);
        if (e.agent_type) parts.push(`agent_type = ${quote(e.agent_type)}\n`);
        if (e.floor) parts.push(`floor = ${e.floor}\n`);
        if (e.recycle_after_beads)
          parts.push(`recycle_after_beads = ${e.recycle_after_beads}\n`);
        if (e.idle_ceiling_minutes)
          parts.push(`idle_ceiling_minutes = ${e.idle_ceiling_minutes}\n`);
        if (e.min_interval_per_session_seconds)
          parts.push(
            `min_interval_per_session_seconds = ${e.min_interval_per_session_seconds}\n`
          );
        if (e.max_concurrent) parts.push(`max_concurrent = ${e.max_concurrent}\n`);
      }
    }
  }
  return parts.join('');
}

function quote(s: string): string {
  return `"${s.replace(/"/g, '\\"')}"`;
}

interface Props {
  cfg: PoolConfigJSON;
}

export function TOMLPreview({ cfg }: Props) {
  const text = renderTOML(cfg);
  return (
    <pre
      data-testid="pool-toml-preview"
      className="overflow-x-auto rounded-md bg-neutral-100 p-3 text-xs font-mono text-neutral-800 dark:bg-neutral-900 dark:text-neutral-200"
    >
      {text}
    </pre>
  );
}
