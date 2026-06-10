// Minimal toast system (gm-root.26 item 2).
//
// The SPA didn't have a toast library at the time this surface
// landed; rather than pull in react-hot-toast or sonner, we ship a
// tiny in-house provider scoped to the AppShell. The API mirrors the
// common shape (`toast.show({ message, action? })`) so a future swap
// to a library is a one-line import change at call sites.
//
// Toasts auto-dismiss after `durationMs` (default 6000). Action is
// an optional anchor — when set, the toast renders the link inside
// the bubble so the operator can navigate without losing the
// notification's content. ariaLabel falls back to message.

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';

export interface ToastAction {
  to: string;
  label: string;
}

export interface ToastInput {
  message: string;
  action?: ToastAction;
  durationMs?: number;
  ariaLabel?: string;
  id?: string;
}

interface ToastRecord extends ToastInput {
  id: string;
}

export interface ToastAPI {
  show: (input: ToastInput) => string;
  dismiss: (id: string) => void;
}

const ToastContext = createContext<ToastAPI | null>(null);

let nextId = 0;
function freshId(): string {
  nextId += 1;
  return `toast-${Date.now()}-${nextId}`;
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastRecord[]>([]);

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const show = useCallback((input: ToastInput): string => {
    const id = input.id ?? freshId();
    setToasts((prev) => [...prev, { ...input, id }]);
    return id;
  }, []);

  const api = useMemo<ToastAPI>(() => ({ show, dismiss }), [show, dismiss]);

  return (
    <ToastContext.Provider value={api}>
      {children}
      <ToastViewport toasts={toasts} onDismiss={dismiss} />
    </ToastContext.Provider>
  );
}

export function useToast(): ToastAPI {
  const ctx = useContext(ToastContext);
  if (!ctx) {
    // Tolerate use outside a provider — return a no-op so unit tests
    // that exercise components in isolation don't have to mount the
    // shell. The real shell always wraps callers in a provider.
    return {
      show: () => '',
      dismiss: () => {},
    };
  }
  return ctx;
}

function ToastViewport({
  toasts,
  onDismiss,
}: {
  toasts: ToastRecord[];
  onDismiss: (id: string) => void;
}) {
  if (toasts.length === 0) return null;
  return (
    <div
      role="region"
      aria-label="Notifications"
      data-testid="toast-viewport"
      className="pointer-events-none fixed bottom-4 right-4 z-[60] flex max-w-sm flex-col gap-2"
    >
      {toasts.map((t) => (
        <ToastBubble key={t.id} toast={t} onDismiss={onDismiss} />
      ))}
    </div>
  );
}

function ToastBubble({
  toast,
  onDismiss,
}: {
  toast: ToastRecord;
  onDismiss: (id: string) => void;
}) {
  const duration = toast.durationMs ?? 6000;
  useEffect(() => {
    if (duration <= 0) return;
    const handle = window.setTimeout(() => onDismiss(toast.id), duration);
    return () => window.clearTimeout(handle);
  }, [duration, onDismiss, toast.id]);

  return (
    <div
      role="status"
      aria-live="polite"
      aria-label={toast.ariaLabel ?? toast.message}
      data-testid="toast-item"
      data-toast-id={toast.id}
      className="pointer-events-auto flex items-center gap-3 rounded-md border border-neutral-200 bg-white px-3 py-2 text-sm text-neutral-800 shadow-lg dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
    >
      <span className="flex-1">{toast.message}</span>
      {toast.action ? (
        <Link
          to={toast.action.to}
          onClick={() => onDismiss(toast.id)}
          data-testid="toast-action"
          className="rounded-sm text-xs font-medium text-emerald-700 underline underline-offset-2 hover:text-emerald-900 dark:text-emerald-300 dark:hover:text-emerald-100"
        >
          {toast.action.label}
        </Link>
      ) : null}
      <button
        type="button"
        onClick={() => onDismiss(toast.id)}
        aria-label="Dismiss notification"
        data-testid="toast-dismiss"
        className="rounded-sm text-neutral-400 hover:text-neutral-700 dark:hover:text-neutral-200"
      >
        ×
      </button>
    </div>
  );
}
