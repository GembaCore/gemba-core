// RunCommandModal — confirmation modal for adaptor-CLI mutations
// (gm-s47n.17, spec docs/design/pool-editor.md §6.3).
//
// The pool editor's gt-flow surfaces three buttons that shell out to
// the gt CLI on the server side:
//
//   + New rig          → POST /api/orchestration/scopes
//   + New polecat      → POST /api/orchestration/polecats
//   Edit gt scheduler  → PUT  /api/orchestration/scheduler-config
//
// Each one routes through this modal so the operator sees the exact
// shell command before confirming, then sees the captured stdout/
// stderr after the run lands. Every request carries an
// X-GEMBA-Confirm nonce identical to other gemba mutations.
//
// The modal is intentionally generic: callers describe the fields,
// the URL, and the command-preview template, and the modal owns
// validation + nonce + spinner + result rendering. Keeping the
// chrome consistent with <CreateProjectModal> matters more than
// invention here, so we lean on Radix Dialog + Tailwind classes
// already established elsewhere in the SPA.

import * as Dialog from '@radix-ui/react-dialog';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { ApiError, apiFetch } from '@/api/client';
import { CONFIRM_HEADER } from '@/api/workItems';
import { cn } from '@/lib/utils';

export interface ModalField {
  // name maps directly to the JSON body key the server expects.
  name: string;
  label: string;
  placeholder?: string;
  required?: boolean;
  initialValue?: string;
  // kind: 'text' renders an input; 'select' renders a dropdown
  // (options required); 'number' renders a numeric input. Default
  // is 'text'.
  kind?: 'text' | 'select' | 'number';
  options?: { value: string; label: string }[];
  // validate returns an error message string, or null when the
  // value is acceptable. Runs on every keystroke; non-null blocks
  // submit.
  validate?: (v: string) => string | null;
}

export interface RunCommandModalProps {
  open: boolean;
  title: string;
  description?: string;
  // command is the preview template. Placeholders of the form
  // <fieldName> are replaced with the live value of that field. If
  // a field is empty the placeholder is left as-is so the operator
  // sees what they still need to fill in.
  command: string;
  fields: ModalField[];
  endpoint: string; // e.g. '/orchestration/scopes' (no /api prefix)
  method: 'POST' | 'PUT';
  // onConfirm fires AFTER a successful run, before the modal
  // closes. Callers use it to refetch any state that the mutation
  // invalidated.
  onConfirm?: () => void;
  onClose: () => void;
  // bodyTransform lets a caller post a body shape that doesn't
  // exactly match {fieldName: stringValue}. Default packs every
  // field into the request body verbatim.
  bodyTransform?: (values: Record<string, string>) => unknown;
}

interface RunResult {
  command: string;
  stdout: string;
  stderr: string;
  ok: boolean;
  error?: string;
}

function freshNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

// renderCommandPreview substitutes <fieldName> placeholders with the
// current value typed by the operator. Empty values keep the
// placeholder so the preview reads like a fill-in-the-blank.
function renderCommandPreview(template: string, values: Record<string, string>): string {
  let out = template;
  for (const [k, v] of Object.entries(values)) {
    if (v.trim().length === 0) continue;
    out = out.split(`<${k}>`).join(v.trim());
  }
  return out;
}

export function RunCommandModal({
  open,
  title,
  description,
  command,
  fields,
  endpoint,
  method,
  onConfirm,
  onClose,
  bodyTransform,
}: RunCommandModalProps) {
  // values is the live input state, keyed by field name.
  const [values, setValues] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);
  const [result, setResult] = useState<RunResult | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  // Reset state on each open so a closed-and-reopened modal starts
  // clean. Seed each field with its initialValue (or '').
  useEffect(() => {
    if (!open) return;
    const seed: Record<string, string> = {};
    for (const f of fields) {
      seed[f.name] = f.initialValue ?? '';
    }
    setValues(seed);
    setSubmitting(false);
    setResult(null);
    setErrorMessage(null);
  }, [open, fields]);

  // Per-field validation is recomputed on every render — it's
  // cheap and lets the Confirm button reflect the current state
  // without separate state.
  const fieldErrors = useMemo(() => {
    const out: Record<string, string | null> = {};
    for (const f of fields) {
      const v = values[f.name] ?? '';
      if (f.required && v.trim().length === 0) {
        out[f.name] = `${f.label} is required`;
        continue;
      }
      if (f.validate && v.length > 0) {
        out[f.name] = f.validate(v);
        continue;
      }
      out[f.name] = null;
    }
    return out;
  }, [fields, values]);

  const canSubmit =
    !submitting &&
    !result &&
    Object.values(fieldErrors).every((e) => e === null);

  const previewCommand = useMemo(
    () => renderCommandPreview(command, values),
    [command, values]
  );

  const onSubmit = useCallback(async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setErrorMessage(null);
    try {
      const body =
        bodyTransform != null
          ? bodyTransform(values)
          : Object.fromEntries(
              fields.map((f) => [f.name, values[f.name]?.trim() ?? ''])
            );
      const res = await apiFetch<RunResult>(endpoint, {
        method,
        body: JSON.stringify(body),
        headers: {
          'Content-Type': 'application/json',
          [CONFIRM_HEADER]: freshNonce(),
        },
      });
      setResult(res);
    } catch (err) {
      if (err instanceof ApiError) {
        // Surface the server's typed error envelope. retryable is
        // implicit — the operator can press Confirm again after
        // the modal resets, but we keep the message terse here.
        setErrorMessage(`${err.code}: ${err.message}`);
      } else {
        setErrorMessage(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setSubmitting(false);
    }
  }, [canSubmit, bodyTransform, values, fields, endpoint, method]);

  // Enter submits when valid and not already in flight; the modal
  // is form-shaped so the keyboard shortcut is implicit. Esc is
  // wired by Radix Dialog; we override its onOpenChange to forbid
  // closing while a request is in flight.
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey && !result && canSubmit) {
      e.preventDefault();
      void onSubmit();
    }
  };

  const onCloseAttempt = (next: boolean) => {
    if (next) return;
    if (submitting) return; // forbid close while in flight
    if (result && onConfirm) onConfirm();
    onClose();
  };

  return (
    <Dialog.Root open={open} onOpenChange={onCloseAttempt}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/50" />
        <Dialog.Content
          data-testid="run-command-modal"
          onKeyDown={onKeyDown}
          className={cn(
            'fixed left-1/2 top-1/2 z-50 w-full max-w-2xl -translate-x-1/2 -translate-y-1/2',
            'rounded-lg border border-neutral-200 bg-white p-6 shadow-lg',
            'dark:border-neutral-800 dark:bg-neutral-950'
          )}
        >
          <Dialog.Title className="mb-1 text-lg font-semibold">{title}</Dialog.Title>
          {description && (
            <Dialog.Description className="mb-4 text-xs text-neutral-500 dark:text-neutral-400">
              {description}
            </Dialog.Description>
          )}

          {/* Field inputs (hidden once the run lands; the modal
              becomes a results view at that point). */}
          {!result && (
            <div className="space-y-3">
              {fields.map((f) => {
                const err = fieldErrors[f.name];
                const v = values[f.name] ?? '';
                return (
                  <label key={f.name} className="block">
                    <span className="mb-1 block text-sm font-medium">{f.label}</span>
                    {f.kind === 'select' ? (
                      <select
                        data-testid={`run-command-field-${f.name}`}
                        value={v}
                        disabled={submitting}
                        onChange={(e) =>
                          setValues((cur) => ({ ...cur, [f.name]: e.target.value }))
                        }
                        className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-900"
                      >
                        <option value="">(select…)</option>
                        {(f.options ?? []).map((opt) => (
                          <option key={opt.value} value={opt.value}>
                            {opt.label}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <input
                        type={f.kind === 'number' ? 'number' : 'text'}
                        data-testid={`run-command-field-${f.name}`}
                        value={v}
                        placeholder={f.placeholder}
                        disabled={submitting}
                        onChange={(e) =>
                          setValues((cur) => ({ ...cur, [f.name]: e.target.value }))
                        }
                        autoFocus={f === fields[0]}
                        className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-900"
                      />
                    )}
                    {err && (
                      <span
                        data-testid={`run-command-field-error-${f.name}`}
                        className="mt-1 block text-xs text-red-700 dark:text-red-300"
                      >
                        {err}
                      </span>
                    )}
                  </label>
                );
              })}
            </div>
          )}

          {/* Live command preview — always shown above the action
              row so the operator sees what they're about to run. */}
          <div className="mt-4">
            <span className="mb-1 block text-xs font-medium text-neutral-500 dark:text-neutral-400">
              Will run:
            </span>
            <pre
              data-testid="run-command-preview"
              className="overflow-x-auto rounded-md bg-neutral-100 p-2 font-mono text-xs dark:bg-neutral-900"
            >
              {previewCommand}
            </pre>
          </div>

          {/* Result panes appear after a successful (or failed)
              run. stdout and stderr are both surfaced — stderr in
              red when the exit was non-zero, amber when the exit
              was OK but stderr was non-empty. */}
          {result && (
            <div className="mt-4 space-y-3" data-testid="run-command-result">
              <div>
                <span className="mb-1 block text-xs font-medium text-neutral-500 dark:text-neutral-400">
                  stdout
                </span>
                <pre
                  data-testid="run-command-stdout"
                  className="max-h-48 overflow-auto rounded-md bg-neutral-100 p-2 font-mono text-xs dark:bg-neutral-900"
                >
                  {result.stdout || '(no output)'}
                </pre>
              </div>
              {result.stderr && (
                <div>
                  <span className="mb-1 block text-xs font-medium text-neutral-500 dark:text-neutral-400">
                    stderr
                  </span>
                  <pre
                    data-testid="run-command-stderr"
                    className={cn(
                      'max-h-48 overflow-auto rounded-md p-2 font-mono text-xs',
                      result.ok
                        ? 'bg-amber-50 text-amber-900 dark:bg-amber-950 dark:text-amber-200'
                        : 'bg-red-50 text-red-800 dark:bg-red-950 dark:text-red-200'
                    )}
                  >
                    {result.stderr}
                  </pre>
                </div>
              )}
              {result.error && (
                <div
                  data-testid="run-command-error-line"
                  className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300"
                >
                  {result.error}
                </div>
              )}
            </div>
          )}

          {errorMessage && !result && (
            <div
              data-testid="run-command-error"
              role="alert"
              className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300"
            >
              {errorMessage}
            </div>
          )}

          <div className="mt-6 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => onCloseAttempt(false)}
              disabled={submitting}
              data-testid="run-command-close"
              className="rounded-md px-3 py-1.5 text-sm text-neutral-700 hover:bg-neutral-100 disabled:cursor-not-allowed disabled:opacity-50 dark:text-neutral-300 dark:hover:bg-neutral-900"
            >
              {result ? 'Close' : 'Cancel'}
            </button>
            {!result && (
              <button
                type="button"
                onClick={onSubmit}
                disabled={!canSubmit}
                data-testid="run-command-confirm"
                className={cn(
                  'rounded-md px-4 py-1.5 text-sm font-medium',
                  canSubmit
                    ? 'bg-neutral-900 text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200'
                    : 'bg-neutral-200 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-500'
                )}
              >
                {submitting ? (
                  <span className="inline-flex items-center gap-2">
                    <span
                      data-testid="run-command-spinner"
                      className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-current border-t-transparent"
                      aria-hidden
                    />
                    Running…
                  </span>
                ) : (
                  <>Run <code>gt …</code></>
                )}
              </button>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
