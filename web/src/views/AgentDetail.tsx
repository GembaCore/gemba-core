// AgentDetail — provider-aware agent detail view (gm-e12.15).
//
// Renders the per-Workspace.kind affordance set described by ui-spec
// §5.18 and DD-5. The view is pure: feed it a PlannerOperationalContext
// and it picks the matching panel. The owning page (AgentDetailPage)
// handles fetching + agent selection.
//
// Spirit: the SPA MUST NOT render a worktree-only "Attach" button next
// to a k8s pod, and MUST NOT render kubectl-exec next to a host
// subprocess. Every kind gets its own affordance set or a fallback
// "unknown" panel — never a confused superset.

import { Users } from 'lucide-react';
import type { PlannerOperationalContext } from '@/api/planner';
import {
  ContainerPanel,
  ExecPanel,
  K8sPodPanel,
  SubprocessPanel,
  UnknownKindPanel,
  VMPanel,
  WorktreePanel,
  isCanonicalKind,
} from './agentDetailPanels';

export interface AgentDetailProps {
  ctx: PlannerOperationalContext;
}

export function AgentDetail({ ctx }: AgentDetailProps) {
  return (
    <div className="flex flex-col gap-4 p-6" data-testid="agent-detail">
      <Header ctx={ctx} />
      <KindPanel ctx={ctx} />
    </div>
  );
}

function Header({ ctx }: AgentDetailProps) {
  const agentName = ctx.agent?.name ?? ctx.agent?.id ?? '(no agent)';
  const sessionId = ctx.session?.id ?? '';
  const status = ctx.session?.status ?? 'unknown';
  return (
    <header
      data-testid="agent-detail-header"
      className="flex items-center justify-between gap-3 border-b border-neutral-200 pb-3 dark:border-neutral-800"
    >
      <div className="flex items-center gap-3">
        <Users className="h-5 w-5 text-neutral-500" aria-hidden />
        <div>
          <h1
            data-testid="agent-detail-name"
            className="text-lg font-semibold tracking-tight"
          >
            {agentName}
          </h1>
          {sessionId ? (
            <p className="font-mono text-xs text-neutral-500">
              session: {sessionId}
            </p>
          ) : null}
        </div>
      </div>
      <span
        data-testid="agent-detail-status"
        className="rounded bg-neutral-100 px-2 py-0.5 font-mono text-xs dark:bg-neutral-800"
      >
        {status}
      </span>
    </header>
  );
}

// KindPanel is the one branch in this view: pick the panel that
// matches Workspace.kind, fall back to UnknownKindPanel for free-form
// kinds the SPA doesn't recognise (per DD-5 the wire allows this).
function KindPanel({ ctx }: AgentDetailProps) {
  const ws = ctx.workspace;
  if (!ws) {
    return (
      <p
        data-testid="agent-detail-no-workspace"
        className="text-sm text-neutral-500"
      >
        This agent has no workspace assigned. (No live session, or the
        adaptor has not published a workspace yet.)
      </p>
    );
  }
  const kind = ws.kind;
  if (!isCanonicalKind(kind)) {
    return <UnknownKindPanel ctx={ctx} />;
  }
  switch (kind) {
    case 'worktree':
      return <WorktreePanel ctx={ctx} />;
    case 'container':
      return <ContainerPanel ctx={ctx} />;
    case 'k8s_pod':
      return <K8sPodPanel ctx={ctx} />;
    case 'vm':
      return <VMPanel ctx={ctx} />;
    case 'exec':
      return <ExecPanel ctx={ctx} />;
    case 'subprocess':
      return <SubprocessPanel ctx={ctx} />;
  }
}
