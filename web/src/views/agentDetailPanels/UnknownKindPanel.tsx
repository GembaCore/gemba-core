// UnknownKindPanel — fallback when the orchestration adaptor declares
// a Workspace.kind that the SPA doesn't recognise (gm-e12.15).
//
// Per gm-root.5 / DD-5 the wire surface for kind is a free string
// because adaptors MAY emit kinds beyond core's knowledge. The SPA
// MUST not crash on an unknown kind — it surfaces what it can and
// nudges the operator to the capability browser to verify the
// adaptor is registered.

import { HelpCircle } from 'lucide-react';
import type { AgentDetailPanelProps } from './types';

export function UnknownKindPanel({ ctx }: AgentDetailPanelProps) {
  const ws = ctx.workspace;
  if (!ws) return null;
  return (
    <section
      data-testid="agent-detail-panel-unknown"
      data-kind={ws.kind}
      className="rounded-md border border-dashed border-neutral-300 bg-neutral-50 p-4 dark:border-neutral-700 dark:bg-neutral-950"
    >
      <header className="mb-2 flex items-center gap-2">
        <HelpCircle className="h-4 w-4 text-neutral-500" aria-hidden />
        <h2 className="text-sm font-semibold tracking-tight">
          Provider-specific workspace
        </h2>
      </header>
      <p className="text-xs text-neutral-600 dark:text-neutral-400">
        Kind <span className="font-mono">{ws.kind || '(empty)'}</span> isn't
        recognised by this build of the SPA. The orchestration adaptor MAY
        publish it — open the Capability Browser to confirm the kind is
        declared on its manifest.
      </p>
    </section>
  );
}
