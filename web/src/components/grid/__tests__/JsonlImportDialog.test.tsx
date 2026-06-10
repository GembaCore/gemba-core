// JsonlImportDialog tests (gm-e12.3 DoD piece 2). Exercises the
// dry-run preview, the commit fan-out (create + update), and the
// failure-display path. The commitRow / mintNonce props are injected
// to skip the network — the api-layer createWorkItem / updateWorkItem
// are tested independently in api/workItems and the api/client.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { JsonlImportDialog } from '../JsonlImportDialog';
import type { ImportAction } from '../jsonlImport';
import type { WorkItem } from '@/types/core.gen';

function wi(id: string, patch: Partial<WorkItem> = {}): WorkItem {
  return {
    id,
    kind: 'task',
    title: `title-${id}`,
    status: 'open',
    state_category: 'unstarted',
    created_at: '2026-04-24T00:00:00Z',
    updated_at: '2026-04-24T00:00:00Z',
    ...patch,
  };
}

function wrapper(): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('JsonlImportDialog', () => {
  beforeEach(() => {
    // Quiet the harmless "not wrapped in act()" warnings from async
    // commitRow callbacks resolving in the next microtask.
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders nothing when open=false', () => {
    render(
      <JsonlImportDialog open={false} onClose={() => {}} existing={[]} />,
      { wrapper: wrapper() }
    );
    expect(screen.queryByTestId('jsonl-import-dialog')).toBeNull();
  });

  it('shows a dry-run summary as the operator types', () => {
    render(
      <JsonlImportDialog open onClose={() => {}} existing={[wi('gm-1')]} />,
      { wrapper: wrapper() }
    );
    const ta = screen.getByTestId('jsonl-import-textarea') as HTMLTextAreaElement;
    fireEvent.change(ta, {
      target: {
        value: ['{"title":"new"}', '{"id":"gm-1","status":"completed"}'].join('\n'),
      },
    });
    expect(screen.getByTestId('jsonl-import-stat-created').textContent).toMatch(/1/);
    expect(screen.getByTestId('jsonl-import-stat-updated').textContent).toMatch(/1/);
    expect(screen.getByTestId('jsonl-import-stat-skipped').textContent).toMatch(/0/);
  });

  it('disables commit when there is nothing actionable', () => {
    render(
      <JsonlImportDialog open onClose={() => {}} existing={[]} />,
      { wrapper: wrapper() }
    );
    const ta = screen.getByTestId('jsonl-import-textarea') as HTMLTextAreaElement;
    fireEvent.change(ta, { target: { value: '{not json}' } });
    expect((screen.getByTestId('jsonl-import-commit') as HTMLButtonElement).disabled).toBe(
      true
    );
  });

  it('fans commit out to the injected commitRow per action and reports success', async () => {
    const seen: ImportAction[] = [];
    const commitRow = vi.fn(async (a: ImportAction, _nonce: string) => {
      seen.push(a);
    });
    let nonceN = 0;
    const mintNonce = () => `nonce-${++nonceN}`;

    render(
      <JsonlImportDialog
        open
        onClose={() => {}}
        existing={[wi('gm-1')]}
        commitRow={commitRow}
        mintNonce={mintNonce}
      />,
      { wrapper: wrapper() }
    );

    fireEvent.change(screen.getByTestId('jsonl-import-textarea'), {
      target: {
        value: ['{"title":"a"}', '{"id":"gm-1","status":"completed"}'].join('\n'),
      },
    });

    await act(async () => {
      screen.getByTestId('jsonl-import-commit').click();
    });

    expect(commitRow).toHaveBeenCalledTimes(2);
    expect(seen.map((a) => a.kind)).toEqual(['create', 'update']);
    // Each row got its own nonce so a mid-batch retry is idempotent.
    const nonces = commitRow.mock.calls.map((c) => c[1] as string);
    expect(new Set(nonces).size).toBe(2);

    expect(screen.getByTestId('jsonl-import-result').textContent).toMatch(/2 \/ 2 committed/);
  });

  it('reports per-row failures without aborting the whole batch', async () => {
    const commitRow = vi.fn(async (a: ImportAction, _nonce: string) => {
      if (a.kind === 'create' && a.line === 2) {
        throw new Error('fake-fail');
      }
    });

    render(
      <JsonlImportDialog
        open
        onClose={() => {}}
        existing={[]}
        commitRow={commitRow}
        mintNonce={() => 'nonce'}
      />,
      { wrapper: wrapper() }
    );

    fireEvent.change(screen.getByTestId('jsonl-import-textarea'), {
      target: { value: ['{"title":"ok"}', '{"title":"fail"}', '{"title":"ok2"}'].join('\n') },
    });

    await act(async () => {
      screen.getByTestId('jsonl-import-commit').click();
    });

    expect(screen.getByTestId('jsonl-import-result').textContent).toMatch(/2 \/ 3 committed/);
    expect(screen.getByTestId('jsonl-import-failure-2').textContent).toMatch(/fake-fail/);
  });

  it('skipped rows show alongside the summary with their reasons', () => {
    render(
      <JsonlImportDialog open onClose={() => {}} existing={[]} />,
      { wrapper: wrapper() }
    );
    fireEvent.change(screen.getByTestId('jsonl-import-textarea'), {
      target: {
        value: ['{"title":"ok"}', '{"kind":"task"}', '{"title":"x","badField":1}'].join('\n'),
      },
    });
    expect(screen.getByTestId('jsonl-import-stat-skipped').textContent).toMatch(/2/);
    expect(screen.getByTestId('jsonl-import-skip-2')).toBeTruthy();
    expect(screen.getByTestId('jsonl-import-skip-3')).toBeTruthy();
  });
});
