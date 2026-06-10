// BootstrapPage host tests (gm-uipx.7). Covers step routing, state
// threading, and the cancel-persists-nothing contract.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BootstrapPage } from '../BootstrapPage';

vi.mock('@/api/bootstrap', async () => {
  const actual = await vi.importActual<typeof import('@/api/bootstrap')>('@/api/bootstrap');
  return {
    ...actual,
    startBootstrap: vi.fn(),
    pollBootstrapProgress: vi.fn(),
    fetchBootstrapPlan: vi.fn(),
    commitBootstrap: vi.fn(),
  };
});
import {
  commitBootstrap,
  fetchBootstrapPlan,
  pollBootstrapProgress,
  startBootstrap,
} from '@/api/bootstrap';

function LocationProbe(): JSX.Element {
  const loc = useLocation();
  return <div data-testid="probe-pathname">{loc.pathname}</div>;
}

function wrap(initialEntries: string[]): JSX.Element {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={initialEntries}>
        <Routes>
          <Route path="/bootstrap/:step" element={<BootstrapPage />} />
          <Route path="/board" element={<div data-testid="board-page">Board</div>} />
        </Routes>
        <LocationProbe />
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe('BootstrapPage', () => {
  beforeEach(() => {
    (startBootstrap as ReturnType<typeof vi.fn>).mockResolvedValue({
      analysis_id: 'a-1',
    });
    (pollBootstrapProgress as ReturnType<typeof vi.fn>).mockResolvedValue([
      { seq: 1, line: 'Scanning…', at: new Date().toISOString() },
      { seq: 2, line: 'Done', at: new Date().toISOString(), done: true },
    ]);
    (fetchBootstrapPlan as ReturnType<typeof vi.fn>).mockResolvedValue({
      analysis_id: 'a-1',
      source: 'fresh',
      plan: [{ id: 'e1', kind: 'epic', title: 'Epic 1' }],
      findings: [{ id: 'f1', level: 'pass', title: 'OK' }],
    });
    (commitBootstrap as ReturnType<typeof vi.fn>).mockResolvedValue({
      project_path: '.gemba/project.toml',
      board_url: '/board',
    });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('Step 1 renders source tiles and step indicator marks Source active', () => {
    render(wrap(['/bootstrap/source']));
    expect(screen.getByTestId('bootstrap-source-tiles')).toBeTruthy();
    const stepEl = screen.getByTestId('bootstrap-step-source');
    expect(stepEl.getAttribute('data-active')).toBe('true');
  });

  it('clicking a tile advances to the analysis step URL', async () => {
    render(wrap(['/bootstrap/source']));
    fireEvent.click(screen.getByTestId('bootstrap-source-tile-fresh'));
    await waitFor(() =>
      expect(screen.getByTestId('probe-pathname').textContent).toBe('/bootstrap/analysis')
    );
  });

  it('analysis step without a pinned source falls back to needs-source notice', () => {
    render(wrap(['/bootstrap/analysis']));
    expect(screen.getByTestId('bootstrap-needs-source')).toBeTruthy();
  });

  it('cancel button clears state and navigates to /board without committing', async () => {
    render(wrap(['/bootstrap/source']));
    fireEvent.click(screen.getByTestId('bootstrap-source-tile-fresh'));
    await waitFor(() =>
      expect(screen.getByTestId('probe-pathname').textContent).toBe('/bootstrap/analysis')
    );
    fireEvent.click(screen.getByTestId('bootstrap-cancel'));
    await waitFor(() =>
      expect(screen.getByTestId('probe-pathname').textContent).toBe('/board')
    );
    expect(commitBootstrap).not.toHaveBeenCalled();
  });
});
