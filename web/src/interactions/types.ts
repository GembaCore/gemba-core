import type { OrchestrationManifest } from '@/capabilities';

export type InteractionKind =
  | 'project_onboarding'
  | 'pm_consult'
  | 'escalation_triage'
  | 'gemba_walk'
  | 'session_supervision'
  | 'evidence_review'
  | 'decision_review';

export type InteractionScopeType =
  | 'project'
  | 'milestone'
  | 'epic'
  | 'workitem'
  | 'session'
  | 'escalation'
  | 'walk';

export type InteractionUIHost = 'rhp' | 'page' | 'modal';

export type InteractionRuntimeHost =
  | 'native'
  | 'codex'
  | 'claude'
  | 'gastown_mayor'
  | 'gastown_crew'
  | 'server_persona'
  | 'manual';

export type InteractionStatus =
  | 'drafting'
  | 'waiting_on_operator'
  | 'running'
  | 'applying'
  | 'done'
  | 'canceled'
  | 'failed';

export interface InteractionScope {
  type: InteractionScopeType;
  id: string;
  title?: string;
  breadcrumb?: Array<{ id: string; label: string; type: InteractionScopeType }>;
}

export interface InteractionMessage {
  id: string;
  role: 'operator' | 'assistant' | 'system' | 'tool';
  body: string;
  at?: string;
}

export interface InteractionSuggestedAction {
  id: string;
  label: string;
  description: string;
  disabledReason?: string;
}

export interface InteractionDraft {
  title: string;
  summary: string;
  bullets?: string[];
}

export interface InteractionDecision {
  id: string;
  summary: string;
  rationale?: string;
  outcome: 'accepted' | 'rejected' | 'deferred';
  decidedAt?: string;
}

export interface InteractionSession {
  id: string;
  kind: InteractionKind;
  status: InteractionStatus;
  uiHost: InteractionUIHost;
  runtimeHost: InteractionRuntimeHost;
  runtimeLabel: string;
  scope: InteractionScope;
  messages: InteractionMessage[];
  suggestedActions: InteractionSuggestedAction[];
  draft?: InteractionDraft;
  evidence?: Array<{ id: string; label: string; href?: string }>;
  decisionLog?: InteractionDecision[];
  capabilities: string[];
}

export function encodeInteractionTarget(scope: InteractionScope): string {
  return `${scope.type}|${encodeURIComponent(scope.id)}`;
}

export function decodeInteractionTarget(raw: string): InteractionScope {
  const sep = raw.indexOf('|');
  if (sep <= 0 || sep === raw.length - 1) {
    return { type: 'workitem', id: raw };
  }
  return {
    type: raw.slice(0, sep) as InteractionScopeType,
    id: decodeURIComponent(raw.slice(sep + 1)),
  };
}

export function runtimeHostForScope(
  scope: InteractionScope,
  orchestrationPlane: OrchestrationManifest | null
): { host: InteractionRuntimeHost; label: string } {
  const adaptor = orchestrationPlane?.adaptor_id ?? '';
  if (adaptor === 'gastown') {
    const mayorScopes: ReadonlySet<InteractionScopeType> = new Set([
      'project',
      'milestone',
      'epic',
      'escalation',
      'walk',
    ]);
    if (mayorScopes.has(scope.type)) {
      return { host: 'gastown_mayor', label: 'Gas Town mayor' };
    }
    return { host: 'gastown_crew', label: 'Gas Town crew' };
  }

  if (adaptor === 'native') return { host: 'native', label: 'Native session' };
  if (adaptor === 'codex') return { host: 'codex', label: 'Codex session' };
  if (adaptor === 'claude') return { host: 'claude', label: 'Claude session' };
  return { host: 'server_persona', label: 'Server persona' };
}
