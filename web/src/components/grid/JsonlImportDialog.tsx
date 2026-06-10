// JsonlImportDialog (gm-e12.3 DoD piece 2). Bulk-import surface in the
// grid view: paste or drop a JSONL file, see a dry-run diff (n created
// / n updated / n skipped), then commit. Commit fans out POST + PATCH
// against the same /api/work-items surface the rest of the SPA uses.
//
// Idempotency: each row gets its own pre-minted X-GEMBA-Confirm nonce
// so a network blip mid-batch doesn't double-create. The dialog tracks
// success / failure per action and stops reporting "in progress" once
// every action has resolved either way — partial failures are a normal
// outcome and the operator can re-paste the affected rows.

import { useMemo, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Upload } from 'lucide-react';
import { createWorkItem, updateWorkItem } from '@/api/workItems';
import { workItemsKeys } from '@/hooks/useWorkItems';
import type { WorkItem } from '@/types/core.gen';
import {
  planJsonlImport,
  type ImportAction,
  type ImportPlan,
} from './jsonlImport';

export interface JsonlImportDialogProps {
  open: boolean;
  onClose: () => void;
  existing: WorkItem[];
  // commitRow / mintNonce are injected in tests so the dialog can be
  // exercised without a live network. Production omits both — the
  // dialog falls back to the real api/workItems helpers.
  commitRow?: (action: ImportAction, nonce: string) => Promise<void>;
  mintNonce?: () => string;
}

interface CommitResult {
  total: number;
  succeeded: number;
  failed: { line: number; message: string }[];
}

export function JsonlImportDialog({
  open,
  onClose,
  existing,
  commitRow,
  mintNonce,
}: JsonlImportDialogProps) {
  const [text, setText] = useState('');
  const [committing, setCommitting] = useState(false);
  const [result, setResult] = useState<CommitResult | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const qc = useQueryClient();

  const plan = useMemo<ImportPlan | null>(() => {
    if (text.trim() === '') return null;
    return planJsonlImport(text, existing);
  }, [text, existing]);

  const reset = () => {
    setText('');
    setResult(null);
    setCommitting(false);
    if (fileRef.current) fileRef.current.value = '';
  };

  const close = () => {
    reset();
    onClose();
  };

  const onFile = async (file: File | undefined) => {
    if (!file) return;
    const content = await file.text();
    setText(content);
    setResult(null);
  };

  const commit = async () => {
    if (!plan || committing) return;
    setCommitting(true);
    setResult(null);

    const actionable = plan.actions.filter(
      (a): a is Extract<ImportAction, { kind: 'create' | 'update' }> =>
        a.kind === 'create' || a.kind === 'update'
    );
    const failures: { line: number; message: string }[] = [];
    let succeeded = 0;
    for (const action of actionable) {
      const nonce = (mintNonce ?? defaultMintNonce)();
      try {
        if (commitRow) {
          await commitRow(action, nonce);
        } else if (action.kind === 'create') {
          await createWorkItem(action.payload, { nonce });
        } else {
          await updateWorkItem(action.id, action.patch, { nonce });
        }
        succeeded++;
      } catch (err) {
        failures.push({
          line: action.line,
          message: err instanceof Error ? err.message : String(err),
        });
      }
    }

    setResult({ total: actionable.length, succeeded, failed: failures });
    setCommitting(false);
    if (succeeded > 0) {
      // Surface the freshly created/updated items in the grid right
      // away. Invalidate by namespace so both the unfiltered list and
      // any active filtered queries refetch.
      qc.invalidateQueries({ queryKey: workItemsKeys.all });
    }
  };

  if (!open) return null;

  const skipsToShow = plan ? plan.actions.filter((a) => a.kind === 'skip').slice(0, 5) : [];
  const hiddenSkipCount = plan ? Math.max(0, plan.counts.skip - skipsToShow.length) : 0;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Import JSONL"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      onKeyDown={(e) => {
        if (e.key === 'Escape') close();
      }}
    >
      <div
        className="w-full max-w-2xl rounded-lg bg-white p-6 shadow-xl dark:bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
        data-testid="jsonl-import-dialog"
      >
        <h2 className="mb-1 text-lg font-semibold">Import work items (JSONL)</h2>
        <p className="mb-4 text-xs text-neutral-500">
          One JSON object per line. Rows with an existing <code>id</code> are
          updates; everything else creates a new item (the adaptor assigns the
          final id).
        </p>

        <div className="mb-2 flex items-center gap-2">
          <input
            ref={fileRef}
            type="file"
            accept=".jsonl,.ndjson,.json,application/json,text/plain"
            onChange={(e) => onFile(e.target.files?.[0])}
            className="text-xs"
            data-testid="jsonl-import-file"
          />
        </div>

        <textarea
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            setResult(null);
          }}
          placeholder={`{"title":"new task"}\n{"id":"gm-1","status":"completed"}`}
          className="mb-3 h-48 w-full rounded-md border border-neutral-300 bg-white px-3 py-2 font-mono text-xs dark:border-neutral-700 dark:bg-neutral-800 dark:text-neutral-100"
          data-testid="jsonl-import-textarea"
        />

        {plan ? (
          <div
            className="mb-3 rounded-md border border-neutral-200 bg-neutral-50 p-3 text-sm dark:border-neutral-800 dark:bg-neutral-950"
            data-testid="jsonl-import-summary"
          >
            <div className="flex gap-4">
              <SummaryStat label="created" value={plan.counts.create} tone="emerald" />
              <SummaryStat label="updated" value={plan.counts.update} tone="sky" />
              <SummaryStat label="skipped" value={plan.counts.skip} tone="amber" />
            </div>
            {skipsToShow.length > 0 ? (
              <ul className="mt-2 space-y-0.5 text-xs text-neutral-600 dark:text-neutral-400">
                {skipsToShow.map(
                  (s) =>
                    s.kind === 'skip' && (
                      <li key={s.line} data-testid={`jsonl-import-skip-${s.line}`}>
                        line {s.line}: {s.reason}
                      </li>
                    )
                )}
                {hiddenSkipCount > 0 ? (
                  <li className="italic">…and {hiddenSkipCount} more</li>
                ) : null}
              </ul>
            ) : null}
          </div>
        ) : null}

        {result ? (
          <div
            className={`mb-3 rounded-md p-3 text-sm ${
              result.failed.length === 0
                ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300'
                : 'bg-amber-50 text-amber-800 dark:bg-amber-950 dark:text-amber-300'
            }`}
            data-testid="jsonl-import-result"
          >
            {result.succeeded} / {result.total} committed
            {result.failed.length > 0 ? (
              <ul className="mt-1 space-y-0.5 text-xs">
                {result.failed.slice(0, 5).map((f) => (
                  <li key={f.line} data-testid={`jsonl-import-failure-${f.line}`}>
                    line {f.line}: {f.message}
                  </li>
                ))}
                {result.failed.length > 5 ? (
                  <li className="italic">…and {result.failed.length - 5} more</li>
                ) : null}
              </ul>
            ) : null}
          </div>
        ) : null}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={close}
            className="rounded-md px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
            data-testid="jsonl-import-cancel"
          >
            {result ? 'Close' : 'Cancel'}
          </button>
          <button
            type="button"
            onClick={commit}
            disabled={
              committing ||
              result !== null ||
              !plan ||
              plan.counts.create + plan.counts.update === 0
            }
            className="inline-flex items-center gap-1 rounded-md bg-neutral-900 px-3 py-1.5 text-sm text-white hover:bg-neutral-800 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
            data-testid="jsonl-import-commit"
          >
            <Upload className="h-3 w-3" />
            {committing
              ? 'Committing…'
              : plan
                ? `Commit ${plan.counts.create + plan.counts.update}`
                : 'Commit'}
          </button>
        </div>
      </div>
    </div>
  );
}

function SummaryStat({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: 'emerald' | 'sky' | 'amber';
}) {
  const ring = {
    emerald: 'text-emerald-700 dark:text-emerald-300',
    sky: 'text-sky-700 dark:text-sky-300',
    amber: 'text-amber-700 dark:text-amber-300',
  }[tone];
  return (
    <div className="flex flex-col" data-testid={`jsonl-import-stat-${label}`}>
      <span className={`font-mono text-lg font-semibold ${ring}`}>{value}</span>
      <span className="text-[10px] uppercase tracking-wide text-neutral-500">{label}</span>
    </div>
  );
}

function defaultMintNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
