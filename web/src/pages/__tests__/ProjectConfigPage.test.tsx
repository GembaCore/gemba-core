// /project/config tests (gm-uipx.8). Covers sticky nav presence,
// Values editor flow (add/rank/remove), Adaptors read-only, and
// Workspace repos rendering.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { ProjectConfigPage } from '../ProjectConfigPage';
import { SECTIONS } from '@/components/projectconfig/types';
import { resetProjectValuesForTests } from '@/components/projectconfig/ValuesEditor';

vi.mock('@/api/repositories', () => ({
  listRepositories: vi.fn(),
}));
import { listRepositories } from '@/api/repositories';

function wrap(children: ReactNode): JSX.Element {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/project/config']}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

describe('ProjectConfigPage', () => {
  beforeEach(() => {
    window.localStorage.clear();
    resetProjectValuesForTests();
    (listRepositories as ReturnType<typeof vi.fn>).mockResolvedValue({
      repositories: [],
      notice: 'no .gemba/repositories/ directory',
    });
  });
  afterEach(() => {
    window.localStorage.clear();
    resetProjectValuesForTests();
    vi.clearAllMocks();
  });

  // ── sticky nav ──────────────────────────────────────────────

  it('renders the page shell with sticky nav', () => {
    render(wrap(<ProjectConfigPage />));
    expect(screen.getByTestId('project-config-page')).toBeTruthy();
    expect(screen.getByTestId('project-config-sticky-nav')).toBeTruthy();
  });

  it('sticky nav lists all 11 sections in spec order', () => {
    render(wrap(<ProjectConfigPage />));
    const nav = screen.getByTestId('project-config-sticky-nav');
    const links = within(nav).getAllByRole('link');
    expect(links).toHaveLength(11);
    const labels = links.map((l) => l.textContent?.trim());
    expect(labels).toEqual(SECTIONS.map((s) => s.label));
  });

  it('every section renders with a stable testid', () => {
    render(wrap(<ProjectConfigPage />));
    for (const s of SECTIONS) {
      expect(screen.getByTestId(`project-config-section-${s.id}`)).toBeTruthy();
    }
  });

  // ── values editor ───────────────────────────────────────────

  it('Edit button opens the Values modal', () => {
    render(wrap(<ProjectConfigPage />));
    expect(screen.queryByTestId('values-editor-modal')).toBeNull();
    act(() => screen.getByTestId('project-config-values-edit').click());
    expect(screen.getByTestId('values-editor-modal')).toBeTruthy();
  });

  it('add value flow puts the statement at rank 1 with proper persistence', () => {
    render(wrap(<ProjectConfigPage />));
    act(() => screen.getByTestId('project-config-values-edit').click());
    const draft = screen.getByTestId('values-editor-draft') as HTMLTextAreaElement;
    fireEvent.change(draft, { target: { value: 'Ship continuously' } });
    act(() => screen.getByTestId('values-editor-add').click());
    // Modal table now shows the row.
    expect(screen.getByTestId('values-editor-table')).toBeTruthy();
    // localStorage carries the persisted shape.
    const stored = window.localStorage.getItem('gemba.projectconfig.values');
    const parsed = JSON.parse(stored ?? '[]');
    expect(parsed).toHaveLength(1);
    expect(parsed[0].statement).toBe('Ship continuously');
    expect(parsed[0].rank).toBe(1);
  });

  it('move-up swaps the rank with the row above', () => {
    render(wrap(<ProjectConfigPage />));
    act(() => screen.getByTestId('project-config-values-edit').click());
    const draft = screen.getByTestId('values-editor-draft') as HTMLTextAreaElement;
    // Add three values.
    for (const v of ['First', 'Second', 'Third']) {
      fireEvent.change(draft, { target: { value: v } });
      act(() => screen.getByTestId('values-editor-add').click());
    }
    // Find the row whose statement is "Third"; move it up.
    const stored = JSON.parse(
      window.localStorage.getItem('gemba.projectconfig.values') ?? '[]'
    );
    const thirdRow = stored.find(
      (v: { statement: string }) => v.statement === 'Third'
    );
    expect(thirdRow.rank).toBe(3);
    act(() =>
      screen.getByTestId(`values-editor-row-${thirdRow.id}-up`).click()
    );
    const after = JSON.parse(
      window.localStorage.getItem('gemba.projectconfig.values') ?? '[]'
    );
    expect(after.find((v: { statement: string }) => v.statement === 'Third').rank).toBe(2);
    expect(after.find((v: { statement: string }) => v.statement === 'Second').rank).toBe(3);
  });

  it('remove deletes the row and renumbers ranks', () => {
    render(wrap(<ProjectConfigPage />));
    act(() => screen.getByTestId('project-config-values-edit').click());
    const draft = screen.getByTestId('values-editor-draft') as HTMLTextAreaElement;
    for (const v of ['One', 'Two', 'Three']) {
      fireEvent.change(draft, { target: { value: v } });
      act(() => screen.getByTestId('values-editor-add').click());
    }
    const stored = JSON.parse(
      window.localStorage.getItem('gemba.projectconfig.values') ?? '[]'
    );
    const middle = stored.find((v: { statement: string }) => v.statement === 'Two');
    act(() =>
      screen.getByTestId(`values-editor-row-${middle.id}-remove`).click()
    );
    const after = JSON.parse(
      window.localStorage.getItem('gemba.projectconfig.values') ?? '[]'
    );
    expect(after).toHaveLength(2);
    // Ranks renumbered to 1,2 (no gap).
    expect(after.map((v: { rank: number }) => v.rank).sort()).toEqual([1, 2]);
  });

  it('close button on the modal returns to the page without losing values', () => {
    render(wrap(<ProjectConfigPage />));
    act(() => screen.getByTestId('project-config-values-edit').click());
    const draft = screen.getByTestId('values-editor-draft') as HTMLTextAreaElement;
    fireEvent.change(draft, { target: { value: 'Persists' } });
    act(() => screen.getByTestId('values-editor-add').click());
    act(() => screen.getByTestId('values-editor-close').click());
    expect(screen.queryByTestId('values-editor-modal')).toBeNull();
    // The section preview now shows the value.
    expect(screen.getByText('Persists')).toBeTruthy();
  });

  // ── adaptors read-only ──────────────────────────────────────

  it('Adaptors section surfaces the read-only notice with a link to the config file', () => {
    render(wrap(<ProjectConfigPage />));
    const banner = screen.getByTestId('project-config-adaptors-readonly');
    expect(banner.textContent).toMatch(/Read-only/);
    expect(banner.textContent).toMatch(/.gemba\/workspace.toml/);
    expect(screen.getByTestId('project-config-adaptors-link')).toBeTruthy();
  });

  // ── workspace repos ─────────────────────────────────────────

  it('Workspace repos surfaces the notice when the directory is missing', async () => {
    render(wrap(<ProjectConfigPage />));
    // Wait for query resolve.
    expect(
      await screen.findByTestId('project-config-repos-notice')
    ).toBeTruthy();
    expect(screen.queryByTestId('project-config-repos-table')).toBeNull();
  });

  it('Workspace repos lists registered repos from the API', async () => {
    (listRepositories as ReturnType<typeof vi.fn>).mockResolvedValue({
      repositories: [
        {
          id: 'gemba',
          path: '/repos/gemba',
          default_branch: 'main',
          bead_prefix: 'gm',
        },
        {
          id: 'frontend',
          path: '/repos/frontend',
          default_branch: 'main',
          bead_prefix: 'fe',
        },
      ],
    });
    render(wrap(<ProjectConfigPage />));
    expect(
      await screen.findByTestId('project-config-repos-table')
    ).toBeTruthy();
    expect(screen.getByTestId('project-config-repo-gemba')).toBeTruthy();
    expect(screen.getByTestId('project-config-repo-frontend')).toBeTruthy();
  });
});
