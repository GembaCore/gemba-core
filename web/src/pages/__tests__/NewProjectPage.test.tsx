// NewProjectPage host tests (gm-root.17.3). Covers session
// bootstrap, turn submission, in-place plan edits, the persistent
// Ratify button gating, and the nonce-confirmed commit modal.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';

vi.mock('@/api/newproject', async () => {
  const actual =
    await vi.importActual<typeof import('@/api/newproject')>('@/api/newproject');
  return {
    ...actual,
    startNewProject: vi.fn(),
    submitTurn: vi.fn(),
    ratifyNewProject: vi.fn(),
  };
});

import {
  EMPTY_STATE,
  ratifyNewProject,
  startNewProject,
  submitTurn,
} from '@/api/newproject';
import { NewProjectPage } from '../NewProjectPage';

function LocationProbe(): JSX.Element {
  const loc = useLocation();
  return <div data-testid="probe-pathname">{loc.pathname}</div>;
}

function wrap(): JSX.Element {
  return (
    <MemoryRouter initialEntries={['/new']}>
      <Routes>
        <Route path="/new" element={<NewProjectPage />} />
        <Route path="/board" element={<div data-testid="board-page">Board</div>} />
      </Routes>
      <LocationProbe />
    </MemoryRouter>
  );
}

const seededState = {
  ...EMPTY_STATE,
  ProjectName: 'demo',
  Description: 'A demo.',
  Milestones: [
    {
      Title: 'M1',
      Description: 'first',
      Acceptance: '',
      Labels: [],
      Priority: 1,
      Estimate: 0,
      Skills: [],
      DesignNotes: '',
      Notes: '',
      Epics: [
        {
          Title: 'Epic A',
          Description: '',
          Acceptance: '',
          Labels: [],
          Priority: 1,
          Estimate: 0,
          Skills: [],
          DesignNotes: '',
          Notes: '',
          Beads: [],
        },
      ],
    },
  ],
};

describe('NewProjectPage', () => {
  beforeEach(() => {
    (startNewProject as ReturnType<typeof vi.fn>).mockResolvedValue({
      session_id: 'sess-1',
      state: EMPTY_STATE,
      greeting: 'hi there',
    });
    (submitTurn as ReturnType<typeof vi.fn>).mockResolvedValue({
      state: seededState,
      reply: 'drafted milestones',
      reply_id: 'rep-1',
      reply_at: new Date().toISOString(),
    });
    (ratifyNewProject as ReturnType<typeof vi.fn>).mockResolvedValue({
      project_path: '/tmp/p',
      next_url: '/board',
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders starting state then transitions to active with greeting', async () => {
    render(wrap());
    expect(screen.getByTestId('newproject-starting')).toBeTruthy();
    await waitFor(() =>
      expect(screen.getByTestId('newproject-page').getAttribute('data-phase')).toBe('active')
    );
    // greeting message landed in the transcript.
    expect(screen.getByTestId('newproject-message-greeting')).toBeTruthy();
    // both panes painted.
    expect(screen.getByTestId('newproject-conversation-pane')).toBeTruthy();
    expect(screen.getByTestId('newproject-plan-pane')).toBeTruthy();
    expect(screen.getByTestId('newproject-plan-empty')).toBeTruthy();
  });

  it('Ratify button is disabled when no milestones exist', async () => {
    render(wrap());
    await waitFor(() => screen.getByTestId('newproject-ratify'));
    const btn = screen.getByTestId('newproject-ratify') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it('submitting a turn appends transcript and populates the plan tree', async () => {
    render(wrap());
    await waitFor(() => screen.getByTestId('newproject-input'));
    const input = screen.getByTestId('newproject-input') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'build a CRM' } });
    fireEvent.click(screen.getByTestId('newproject-send'));
    await waitFor(() => expect(submitTurn).toHaveBeenCalledTimes(1));
    await waitFor(() => screen.getByTestId('newproject-message-rep-1'));
    expect(screen.getByTestId('newproject-milestone-0')).toBeTruthy();
    // Ratify button now enabled.
    const ratify = screen.getByTestId('newproject-ratify') as HTMLButtonElement;
    expect(ratify.disabled).toBe(false);
  });

  it('opens ratify modal, confirms, and navigates to next_url', async () => {
    render(wrap());
    await waitFor(() => screen.getByTestId('newproject-input'));
    fireEvent.change(screen.getByTestId('newproject-input'), {
      target: { value: 'build a CRM' },
    });
    fireEvent.click(screen.getByTestId('newproject-send'));
    await waitFor(() =>
      expect(
        (screen.getByTestId('newproject-ratify') as HTMLButtonElement).disabled
      ).toBe(false)
    );
    fireEvent.click(screen.getByTestId('newproject-ratify'));
    expect(screen.getByTestId('newproject-ratify-modal')).toBeTruthy();
    expect(screen.getByTestId('newproject-ratify-tree')).toBeTruthy();
    fireEvent.click(screen.getByTestId('newproject-ratify-confirm'));
    await waitFor(() => expect(ratifyNewProject).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(screen.getByTestId('probe-pathname').textContent).toBe('/board')
    );
  });

  it('cancel from the modal returns to active without committing', async () => {
    render(wrap());
    await waitFor(() => screen.getByTestId('newproject-input'));
    fireEvent.change(screen.getByTestId('newproject-input'), {
      target: { value: 'build a CRM' },
    });
    fireEvent.click(screen.getByTestId('newproject-send'));
    await waitFor(() =>
      expect(
        (screen.getByTestId('newproject-ratify') as HTMLButtonElement).disabled
      ).toBe(false)
    );
    fireEvent.click(screen.getByTestId('newproject-ratify'));
    expect(screen.getByTestId('newproject-ratify-modal')).toBeTruthy();
    fireEvent.click(screen.getByTestId('newproject-ratify-cancel'));
    await waitFor(() =>
      expect(screen.queryByTestId('newproject-ratify-modal')).toBeNull()
    );
    expect(ratifyNewProject).not.toHaveBeenCalled();
  });

  it('start error surfaces a banner without the panes', async () => {
    (startNewProject as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error('llm unconfigured')
    );
    render(wrap());
    await waitFor(() => screen.getByTestId('newproject-start-error'));
    expect(screen.queryByTestId('newproject-conversation-pane')).toBeNull();
  });
});
