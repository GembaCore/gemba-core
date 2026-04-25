// EscalationPanel (gm-native.16). Per-session pending-escalation list
// with inline answer-back. Approve / Deny call POST
// /api/escalations/{id}/respond with the canonical resolution kind;
// Custom opens a free-text input that submits as kind=modify with the
// operator's value.
//
// Keyboard ergonomics: when an escalation is focused, 1 = approve,
// 2 = deny, 3 = focus reply input. The handler ignores keystrokes
// inside form controls so the modify input still accepts digits.

import { useEffect, useRef, useState } from 'react';
import { CheckCircle, MessageCircle, X, XCircle } from 'lucide-react';
import { useEscalations, useRespondEscalation } from '@/hooks/useEscalations';
import type {
  EscalationOption,
  EscalationRequest,
  EscalationResolutionKind,
} from '@/api/escalations';

export interface EscalationPanelProps {
  sessionId: string;
  open: boolean;
  onClose: () => void;
}

export function EscalationPanel({ sessionId, open, onClose }: EscalationPanelProps) {
  const { data: escalations = [], isLoading, error } = useEscalations(open ? sessionId : undefined);

  // Auto-close when the last open escalation resolves. The panel is
  // pointless without rows; staying open just hides whatever the
  // operator was looking at on /sessions.
  useEffect(() => {
    if (!open) return;
    if (!isLoading && escalations.length === 0) {
      // small delay so the operator sees "answered" feedback before
      // the overlay tears down
      const t = setTimeout(onClose, 400);
      return () => clearTimeout(t);
    }
    return;
  }, [open, isLoading, escalations.length, onClose]);

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Open escalations"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose();
      }}
    >
      <div
        className="flex w-full max-w-2xl flex-col rounded-lg bg-white shadow-xl dark:bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
        data-testid="escalation-panel"
      >
        <header className="flex items-center justify-between border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
          <div>
            <h2 className="text-lg font-semibold">Open escalations</h2>
            <p className="text-xs text-neutral-500 dark:text-neutral-400">
              Session <span className="font-mono">{sessionId}</span>
            </p>
          </div>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="rounded-md p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-white"
          >
            <X className="h-4 w-4" aria-hidden />
          </button>
        </header>

        <div className="max-h-[70vh] flex-1 overflow-auto px-6 py-4">
          {error && (
            <div className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
              {error.message}
            </div>
          )}
          {isLoading && <div className="py-8 text-center text-sm text-neutral-500">Loading…</div>}
          {!isLoading && escalations.length === 0 && (
            <div className="py-8 text-center text-sm text-neutral-500">No open escalations.</div>
          )}
          <ul className="flex flex-col gap-3">
            {escalations.map((esc) => (
              <li key={esc.id}>
                <EscalationRow escalation={esc} />
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  );
}

interface EscalationRowProps {
  escalation: EscalationRequest;
}

function EscalationRow({ escalation }: EscalationRowProps) {
  const respond = useRespondEscalation();
  const [customOpen, setCustomOpen] = useState(false);
  const [customValue, setCustomValue] = useState('');
  const replyRef = useRef<HTMLTextAreaElement>(null);

  const fire = (kind: EscalationResolutionKind, value?: string) => {
    respond.mutate(
      { id: escalation.id, body: value !== undefined ? { kind, value } : { kind } },
      {
        onSuccess: () => {
          setCustomOpen(false);
          setCustomValue('');
        },
      }
    );
  };

  // Per-row hotkeys: 1 / 2 / 3. Skips form controls so the textarea
  // still accepts digits.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      const tag = target?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
      if (e.key === '1') fire('approve');
      else if (e.key === '2') fire('deny');
      else if (e.key === '3') {
        setCustomOpen(true);
        setTimeout(() => replyRef.current?.focus(), 0);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // Re-bind only when the escalation id changes — fire closes over
    // respond.mutate which is stable across renders.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [escalation.id]);

  const blocking = escalation.urgency === 'blocking';

  return (
    <div
      data-testid={`escalation-row-${escalation.id}`}
      className="rounded-md border border-neutral-200 p-4 dark:border-neutral-800"
    >
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="rounded-md bg-neutral-100 px-2 py-0.5 text-xs font-medium text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
            {escalation.source}
          </span>
          {blocking && (
            <span className="rounded-md bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-400">
              blocking
            </span>
          )}
        </div>
        <span className="font-mono text-xs text-neutral-400">{escalation.id}</span>
      </div>
      {escalation.title && (
        <h3 className="mb-1 text-sm font-medium">{escalation.title}</h3>
      )}
      {escalation.prompt && (
        <pre className="mb-3 max-h-40 overflow-auto whitespace-pre-wrap rounded-md bg-neutral-50 px-3 py-2 text-xs text-neutral-700 dark:bg-neutral-950 dark:text-neutral-300">
          {escalation.prompt}
        </pre>
      )}
      {escalation.options && escalation.options.length > 0 && (
        <div className="mb-3 flex flex-wrap gap-2">
          {escalation.options.map((opt: EscalationOption) => (
            <button
              key={opt.value}
              type="button"
              onClick={() => fire('modify', opt.value)}
              disabled={respond.isPending}
              className="rounded-md border border-neutral-300 px-2 py-1 text-xs text-neutral-700 hover:bg-neutral-50 disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
            >
              {opt.label}
            </button>
          ))}
        </div>
      )}
      {respond.error && (
        <div className="mb-2 rounded-md bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-950 dark:text-red-300">
          {respond.error.message}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => fire('approve')}
          disabled={respond.isPending}
          data-testid={`escalation-row-${escalation.id}-approve`}
          className="inline-flex items-center gap-1 rounded-md bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-emerald-700 disabled:opacity-50"
        >
          <CheckCircle className="h-3.5 w-3.5" aria-hidden />
          Approve <kbd className="ml-1 text-[10px] opacity-70">1</kbd>
        </button>
        <button
          type="button"
          onClick={() => fire('deny')}
          disabled={respond.isPending}
          data-testid={`escalation-row-${escalation.id}-deny`}
          className="inline-flex items-center gap-1 rounded-md bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700 disabled:opacity-50"
        >
          <XCircle className="h-3.5 w-3.5" aria-hidden />
          Deny <kbd className="ml-1 text-[10px] opacity-70">2</kbd>
        </button>
        <button
          type="button"
          onClick={() => setCustomOpen((v) => !v)}
          disabled={respond.isPending}
          className="inline-flex items-center gap-1 rounded-md border border-neutral-300 px-3 py-1.5 text-xs font-medium text-neutral-700 hover:bg-neutral-50 disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
        >
          <MessageCircle className="h-3.5 w-3.5" aria-hidden />
          Custom <kbd className="ml-1 text-[10px] opacity-70">3</kbd>
        </button>
      </div>
      {customOpen && (
        <div className="mt-3">
          <textarea
            ref={replyRef}
            value={customValue}
            onChange={(e) => setCustomValue(e.target.value)}
            placeholder="Custom reply (sent as the next user prompt)…"
            rows={3}
            className="w-full rounded-md border border-neutral-300 px-3 py-2 text-xs dark:border-neutral-700 dark:bg-neutral-800"
          />
          <div className="mt-2 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => {
                setCustomOpen(false);
                setCustomValue('');
              }}
              className="rounded-md px-2 py-1 text-xs text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={() => fire('modify', customValue)}
              disabled={respond.isPending || customValue.trim() === ''}
              className="rounded-md bg-neutral-900 px-3 py-1 text-xs text-white hover:bg-neutral-800 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
            >
              Send reply
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
