import { apiFetch } from './client';
import type {
  InteractionDraft,
  InteractionKind,
  InteractionMessage,
  InteractionRuntimeHost,
  InteractionScope,
  InteractionSession,
  InteractionStatus,
  InteractionSuggestedAction,
} from '@/interactions/types';

interface WireInteractionAction {
  id: string;
  label: string;
  description: string;
  disabled_reason?: string;
}

interface WireInteractionSession {
  id: string;
  kind: InteractionKind;
  status: InteractionStatus;
  ui_host: InteractionSession['uiHost'];
  runtime_host: InteractionRuntimeHost;
  runtime_label: string;
  scope: InteractionScope;
  messages: InteractionMessage[];
  suggested_actions: WireInteractionAction[];
  quick_replies?: InteractionSession['quickReplies'];
  draft?: InteractionDraft;
  evidence?: InteractionSession['evidence'];
  decision_log?: InteractionSession['decisionLog'];
  capabilities: string[];
}

export interface EnsureInteractionRequest {
  kind?: InteractionKind;
  scope: InteractionScope;
}

export interface SendInteractionTurnRequest {
  id: string;
  message: string;
}

export async function ensureInteraction(
  body: EnsureInteractionRequest
): Promise<InteractionSession> {
  const wire = await apiFetch<WireInteractionSession>('/v1/interactions:ensure', {
    method: 'POST',
    body: JSON.stringify(body),
    headers: { 'Content-Type': 'application/json' },
  });
  return fromWire(wire);
}

export async function sendInteractionTurn(
  body: SendInteractionTurnRequest
): Promise<InteractionSession> {
  const wire = await apiFetch<WireInteractionSession>('/v1/interactions:turn', {
    method: 'POST',
    body: JSON.stringify(body),
    headers: { 'Content-Type': 'application/json' },
  });
  return fromWire(wire);
}

function fromWire(wire: WireInteractionSession): InteractionSession {
  return {
    id: wire.id,
    kind: wire.kind,
    status: wire.status,
    uiHost: wire.ui_host,
    runtimeHost: wire.runtime_host,
    runtimeLabel: wire.runtime_label,
    scope: wire.scope,
    messages: wire.messages,
    suggestedActions: wire.suggested_actions.map(actionFromWire),
    quickReplies: wire.quick_replies,
    draft: wire.draft,
    evidence: wire.evidence,
    decisionLog: wire.decision_log,
    capabilities: wire.capabilities,
  };
}

function actionFromWire(action: WireInteractionAction): InteractionSuggestedAction {
  return {
    id: action.id,
    label: action.label,
    description: action.description,
    disabledReason: action.disabled_reason,
  };
}
