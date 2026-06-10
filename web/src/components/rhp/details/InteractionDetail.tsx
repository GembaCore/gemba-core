import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { MessagesSquare } from 'lucide-react';
import { useCapabilities } from '@/capabilities';
import { InteractionPanel } from '@/components/interactions/InteractionPanel';
import { NewSessionDialog } from '@/components/sessions/NewSessionDialog';
import { useWorkItem } from '@/hooks/useWorkItems';
import { useEnsureInteraction, useSendInteractionTurn } from '@/hooks/useInteractions';
import {
  decodeInteractionTarget,
  runtimeHostForScope,
  type InteractionKind,
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
  const kind = kindForScope(decoded);
  const ensured = useEnsureInteraction(decoded, kind);
  const sendTurn = useSendInteractionTurn();
  const navigate = useNavigate();
  const [dispatchOpen, setDispatchOpen] = useState(false);

  const fallbackSession = useMemo(
    () => buildInteractionSession(decoded, item, orchestrationPlane, kind),
    [decoded, item, orchestrationPlane, kind]
  );
  const session = ensured.data ?? fallbackSession;

  return (
    <>
      <InteractionPanel
        session={session}
        onSend={(message) =>
          sendTurn.mutate({
            id: session.id,
            message,
            kind,
            scope: decoded,
          })
        }
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
  orchestrationPlane: ReturnType<typeof useCapabilities>['orchestrationPlane'],
  kind: InteractionKind
): InteractionSession {
  const resolvedScope = scopeFromItem(scope, item);
  const runtime = runtimeHostForScope(resolvedScope, orchestrationPlane);
  const isGasTown = runtime.host === 'gastown_mayor' || runtime.host === 'gastown_crew';

  return {
    id: `${resolvedScope.type}:${resolvedScope.id}:interaction`,
    kind,
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
      title: resolvedScope.type === 'bootstrap' ? 'Bootstrap Review Goal' : 'Working Brief',
      summary:
        resolvedScope.type === 'bootstrap'
          ? 'Translate the bootstrap input into a draft Beads decomposition, shape it through guided review and manual edits, then ratify a finished set that represents the operator perspective.'
          : 'Use this tab for scoped clarification, refinement, triage, and runtime supervision without leaving the board context.',
      bullets: [
        ...(resolvedScope.type === 'bootstrap'
          ? [
              'Draft beads are not stored in a Beads database until ratified.',
              'The review should preserve full provider context, including Spec Kit user stories, tasks, acceptance criteria, and draft item tree.',
              'The final output is a coherent set of milestones, epics, stories, and beads ready for database commit or JSONL export.',
            ]
          : [
              'Transcript-bearing exchanges share the same shape across onboarding, PM consults, escalations, walks, and session supervision.',
              'The UI host remains the RHP while native, Codex, Claude, or Gas Town owns the runtime lifecycle.',
              'Structured actions can later ratify changes, dispatch sessions, attach evidence, or resolve escalations from this same surface.',
            ]),
      ],
    },
    quickReplies:
      resolvedScope.type === 'bootstrap'
        ? [
            {
              id: 'looks-good',
              label: 'Looks good',
              message: 'This draft looks good. Help me do a final readiness check before I ratify it.',
            },
            {
              id: 'change-things',
              label: 'I want changes',
              message:
                'I want to change some things. Review the draft as a batch and suggest what should be renamed, split, merged, or clarified.',
            },
            {
              id: 'edit-board',
              label: "I'll edit on board",
              message:
                "I'll edit on the board. Keep track of the goal and call out anything I should verify before ratifying.",
            },
            {
              id: 'export-jsonl',
              label: 'Export JSONL',
              message:
                'I want to export this draft as Beads-compatible JSONL instead of committing it to a database right now.',
            },
            {
              id: 'need-questions',
              label: 'Ask questions',
              message:
                'Ask me any clarifying questions needed before this draft becomes milestones, epics, and beads.',
            },
          ]
        : undefined,
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

function kindForScope(scope: InteractionScope): InteractionKind {
  switch (scope.type) {
    case 'bootstrap':
      return 'pm_consult';
    case 'escalation':
      return 'escalation_triage';
    case 'walk':
      return 'gemba_walk';
    case 'session':
      return 'session_supervision';
    default:
      return 'pm_consult';
  }
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
  if (scope.type === 'bootstrap') {
    return `Ready to review bootstrap draft ${scope.title ?? scope.id}. Ask clarifying questions, reshape the generated Beads as a batch, then approve the final staged set when it looks right.`;
  }
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
      return ['input.send', 'suggested_actions.apply', 'ratify'];
    case 'manual':
      return ['input.send'];
  }
}
