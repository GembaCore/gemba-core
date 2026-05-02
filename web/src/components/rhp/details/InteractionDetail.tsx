import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { MessagesSquare } from 'lucide-react';
import { useCapabilities } from '@/capabilities';
import { InteractionPanel } from '@/components/interactions/InteractionPanel';
import { NewSessionDialog } from '@/components/sessions/NewSessionDialog';
import { useWorkItem } from '@/hooks/useWorkItems';
import { useEnsureInteraction } from '@/hooks/useInteractions';
import {
  decodeInteractionTarget,
  runtimeHostForScope,
  type InteractionSession,
  type InteractionScope,
} from '@/interactions/types';
import { KIND_MILESTONE, type WorkItem } from '@/types/core.gen';
import { useRegisterDetailContent } from '@/components/rhp/RhpDetail';

export const INTERACTION_DETAIL_KIND = 'interaction';

function renderInteractionDetail(id: string) {
  return <InteractionDetail id={id} />;
}

export function InteractionDetailRegistration() {
  useRegisterDetailContent({
    kind: INTERACTION_DETAIL_KIND,
    icon: MessagesSquare,
    label: 'Interactive',
    render: renderInteractionDetail,
  });
  return null;
}

export function InteractionDetail({ id }: { id: string }) {
  const decoded = useMemo(() => decodeInteractionTarget(id), [id]);
  const workItemId = isWorkItemScope(decoded.type) ? decoded.id : undefined;
  const { data: item } = useWorkItem(workItemId);
  const { orchestrationPlane } = useCapabilities();
  const ensured = useEnsureInteraction(decoded);
  const navigate = useNavigate();
  const [dispatchOpen, setDispatchOpen] = useState(false);

  const fallbackSession = useMemo(
    () => buildInteractionSession(decoded, item, orchestrationPlane),
    [decoded, item, orchestrationPlane]
  );
  const session = ensured.data ?? fallbackSession;

  return (
    <>
      <InteractionPanel
        session={session}
        onAction={(actionId) => {
          if (actionId === 'dispatch' && workItemId) setDispatchOpen(true);
        }}
      />
      {workItemId && dispatchOpen ? (
        <NewSessionDialog
          open={dispatchOpen}
          onClose={() => setDispatchOpen(false)}
          prefilledBeadId={workItemId}
          onStarted={() => navigate('/sessions')}
        />
      ) : null}
    </>
  );
}

function isWorkItemScope(type: InteractionScope['type']): boolean {
  return type === 'workitem' || type === 'epic' || type === 'milestone';
}

function buildInteractionSession(
  scope: InteractionScope,
  item: WorkItem | undefined,
  orchestrationPlane: ReturnType<typeof useCapabilities>['orchestrationPlane']
): InteractionSession {
  const resolvedScope = scopeFromItem(scope, item);
  const runtime = runtimeHostForScope(resolvedScope, orchestrationPlane);
  const isGasTown = runtime.host === 'gastown_mayor' || runtime.host === 'gastown_crew';

  return {
    id: `${resolvedScope.type}:${resolvedScope.id}:interaction`,
    kind: resolvedScope.type === 'escalation' ? 'escalation_triage' : 'pm_consult',
    status: 'waiting_on_operator',
    uiHost: 'rhp',
    runtimeHost: runtime.host,
    runtimeLabel: runtime.label,
    scope: resolvedScope,
    messages: [
      {
        id: 'system-1',
        role: 'system',
        body: isGasTown
          ? 'This interaction is routed through the active Gas Town orchestration plane. Gemba hosts the operator cockpit; Gas Town owns the mayor or crew runtime.'
          : 'This interaction is hosted in the RHP. Runtime work will be routed through the active orchestration plane when you dispatch or apply an action.',
      },
      {
        id: 'assistant-1',
        role: 'assistant',
        body: promptForScope(resolvedScope, item),
      },
    ],
    draft: {
      title: 'Working Brief',
      summary:
        'Use this tab for scoped clarification, refinement, triage, and runtime supervision without leaving the board context.',
      bullets: [
        'Transcript-bearing exchanges share the same shape across onboarding, PM consults, escalations, walks, and session supervision.',
        'The UI host remains the RHP while native, Codex, Claude, or Gas Town owns the runtime lifecycle.',
        'Structured actions can later ratify changes, dispatch sessions, attach evidence, or resolve escalations from this same surface.',
      ],
    },
    suggestedActions: [
      {
        id: 'refine',
        label: 'Refine scope',
        description: 'Ask the PM persona to clarify acceptance criteria and risks for this item.',
        disabledReason: 'Persona-backed refinement is not wired to the shared interaction API yet.',
      },
      {
        id: 'dispatch',
        label: 'Dispatch runtime',
        description: isGasTown
          ? `Request a ${runtime.label.toLowerCase()} session for the next action.`
          : `Start work through the ${runtime.label.toLowerCase()} runtime.`,
        disabledReason: orchestrationPlane
          ? undefined
          : 'No orchestration plane is bound for this workspace.',
      },
      {
        id: 'record-decision',
        label: 'Record decision',
        description: 'Capture the operator choice as a decision/evidence entry on the scoped item.',
        disabledReason: 'Decision persistence is pending the shared interaction API.',
      },
    ],
    evidence: item?.evidence?.map((e) => ({
      id: e.id,
      label: `${e.kind}: ${e.summary || e.ref || e.source}`,
    })),
    decisionLog: [],
    capabilities: capabilitiesForRuntime(runtime.host),
  };
}

function scopeFromItem(scope: InteractionScope, item: WorkItem | undefined): InteractionScope {
  if (!item) return scope;
  const type =
    item.kind === KIND_MILESTONE ? 'milestone' : item.kind === 'epic' ? 'epic' : scope.type;
  return {
    type,
    id: item.id,
    title: item.title,
    breadcrumb: [{ id: item.id, label: item.title, type }],
  };
}

function promptForScope(scope: InteractionScope, item: WorkItem | undefined): string {
  if (!item) {
    return `Ready to work with ${scope.type} ${scope.id}. Load details, then use this surface for follow-up questions, decisions, or runtime handoff.`;
  }
  return `Ready to help with ${item.kind} ${item.id}: ${item.title}. Current state is ${item.state_category}; status is ${item.status}.`;
}

function capabilitiesForRuntime(host: InteractionSession['runtimeHost']): string[] {
  switch (host) {
    case 'gastown_mayor':
      return ['transcript.peek', 'input.send', 'suggested_actions.apply', 'runtime.mayor'];
    case 'gastown_crew':
      return ['transcript.peek', 'input.send', 'suggested_actions.apply', 'runtime.crew'];
    case 'native':
    case 'codex':
    case 'claude':
      return ['transcript.peek', 'input.send', 'pause.resume', 'evidence.attach'];
    case 'server_persona':
      return ['suggested_actions.apply', 'ratify'];
    case 'manual':
      return ['input.send'];
  }
}
