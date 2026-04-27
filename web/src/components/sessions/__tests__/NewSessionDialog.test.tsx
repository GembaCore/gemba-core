// NewSessionDialog tests (gm-hmqj). Focus on the manual-mode path
// and the auto-default behaviour. The bead-mode happy path is
// implicitly covered by the existing route-fake e2e specs that
// exercise the dialog from the board drawer.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { NewSessionDialog } from '../NewSessionDialog';

vi.mock('@/api/repositories', () => ({
  listRepositories: vi.fn(),
}));
vi.mock('@/hooks/useAgents', () => ({
  useAgents: vi.fn(),
}));
vi.mock('@/hooks/useWorkItems', () => ({
  useWorkItems: vi.fn(),
}));
vi.mock('@/api/sessions', async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return {
    ...actual,
    startSession: vi.fn(),
  };
});

import { listRepositories } from '@/api/repositories';
import { useAgents } from '@/hooks/useAgents';
import { useWorkItems } from '@/hooks/useWorkItems';
import { startSession } from '@/api/sessions';

const mockedListRepositories = listRepositories as ReturnType<typeof vi.fn>;
const mockedUseAgents = useAgents as ReturnType<typeof vi.fn>;
const mockedUseWorkItems = useWorkItems as ReturnType<typeof vi.fn>;
const mockedStartSession = startSession as ReturnType<typeof vi.fn>;

function wrap(children: ReactNode): JSX.Element {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  mockedUseAgents.mockReturnValue({ data: [{ id: 'a1', dialect: 'claude' }] });
  mockedUseWorkItems.mockReturnValue({ data: [] });
  mockedListRepositories.mockResolvedValue({
    repositories: [
      { id: 'gemba', path: '/repos/gemba', default_branch: 'main', bead_prefix: 'gm' },
    ],
  });
  mockedStartSession.mockResolvedValue({
    id: 'sess-1',
    assignment_id: 'manual-12345-abcd',
    agent_id: 'mike',
    status: 'ready',
    started_at: new Date().toISOString(),
  });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('NewSessionDialog mode tabs', () => {
  it('renders the bead-mode picker by default', () => {
    render(wrap(<NewSessionDialog open onClose={() => {}} />));
    expect(screen.getByTestId('new-session-mode-tabs')).toBeTruthy();
    expect(screen.getByTestId('new-session-bead')).toBeTruthy();
    expect(screen.queryByTestId('new-session-prompt')).toBeNull();
  });

  it('switches to manual mode and surfaces persona/repo/prompt fields', async () => {
    render(wrap(<NewSessionDialog open onClose={() => {}} />));
    act(() => {
      screen.getByTestId('new-session-mode-manual').click();
    });
    expect(await screen.findByTestId('new-session-persona')).toBeTruthy();
    expect(await screen.findByTestId('new-session-repository')).toBeTruthy();
    expect(screen.getByTestId('new-session-prompt')).toBeTruthy();
    expect(screen.queryByTestId('new-session-bead')).toBeNull();
  });

  it('hides the mode tabs when prefilledBeadId is set (board-drawer dispatch)', () => {
    render(wrap(<NewSessionDialog open onClose={() => {}} prefilledBeadId="gm-1" />));
    expect(screen.queryByTestId('new-session-mode-tabs')).toBeNull();
  });
});

describe('NewSessionDialog manual mode', () => {
  it('auto-defaults the only repository on offer', async () => {
    render(wrap(<NewSessionDialog open onClose={() => {}} />));
    act(() => {
      screen.getByTestId('new-session-mode-manual').click();
    });
    const repoSelect = (await screen.findByTestId('new-session-repository')) as HTMLSelectElement;
    await waitFor(() => expect(repoSelect.value).toBe('gemba'));
  });

  it('auto-defaults the only agent type on offer', async () => {
    render(wrap(<NewSessionDialog open onClose={() => {}} />));
    act(() => {
      screen.getByTestId('new-session-mode-manual').click();
    });
    const agentSelect = (await screen.findByTestId('new-session-agent-type')) as HTMLSelectElement;
    await waitFor(() => expect(agentSelect.value).toBe('claude'));
  });

  it('shows an empty-state when no repositories are registered', async () => {
    mockedListRepositories.mockResolvedValue({
      repositories: [],
      notice: 'no .gemba/repositories/ directory in workspace',
    });
    render(wrap(<NewSessionDialog open onClose={() => {}} />));
    act(() => {
      screen.getByTestId('new-session-mode-manual').click();
    });
    expect(await screen.findByTestId('new-session-repos-empty')).toBeTruthy();
    expect(screen.queryByTestId('new-session-repository')).toBeNull();
  });

  it('submit is disabled until prompt is filled', async () => {
    render(wrap(<NewSessionDialog open onClose={() => {}} />));
    act(() => {
      screen.getByTestId('new-session-mode-manual').click();
    });
    const submit = (await screen.findByTestId('new-session-submit')) as HTMLButtonElement;
    await waitFor(() => expect(submit.disabled).toBe(true));
    fireEvent.change(screen.getByTestId('new-session-prompt'), {
      target: { value: 'Explore the auth flake' },
    });
    await waitFor(() => expect(submit.disabled).toBe(false));
  });

  it('Start session POSTs the manual envelope with persona', async () => {
    const onClose = vi.fn();
    render(wrap(<NewSessionDialog open onClose={onClose} />));
    act(() => {
      screen.getByTestId('new-session-mode-manual').click();
    });
    // Wait for repo + agent autodefaults to land.
    const repoSelect = (await screen.findByTestId('new-session-repository')) as HTMLSelectElement;
    await waitFor(() => expect(repoSelect.value).toBe('gemba'));

    fireEvent.change(screen.getByTestId('new-session-persona'), {
      target: { value: 'coach' },
    });
    fireEvent.change(screen.getByTestId('new-session-prompt'), {
      target: { value: 'Investigate auth flake' },
    });

    const submit = (await screen.findByTestId('new-session-submit')) as HTMLButtonElement;
    await waitFor(() => expect(submit.disabled).toBe(false));
    act(() => {
      submit.click();
    });

    await waitFor(() => expect(mockedStartSession).toHaveBeenCalledTimes(1));
    expect(mockedStartSession.mock.calls[0][0]).toMatchObject({
      kind: 'manual',
      agent_type: 'claude',
      repository_id: 'gemba',
      prompt: 'Investigate auth flake',
      persona_id: 'coach',
    });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('omits persona_id when (none) is selected', async () => {
    render(wrap(<NewSessionDialog open onClose={() => {}} />));
    act(() => {
      screen.getByTestId('new-session-mode-manual').click();
    });
    const repoSelect = (await screen.findByTestId('new-session-repository')) as HTMLSelectElement;
    await waitFor(() => expect(repoSelect.value).toBe('gemba'));
    // Persona stays at "" (the (none) option) by default since the
    // roster has 3 options.
    fireEvent.change(screen.getByTestId('new-session-prompt'), {
      target: { value: 'Quick poke around' },
    });
    const submit = (await screen.findByTestId('new-session-submit')) as HTMLButtonElement;
    await waitFor(() => expect(submit.disabled).toBe(false));
    act(() => {
      submit.click();
    });
    await waitFor(() => expect(mockedStartSession).toHaveBeenCalled());
    const body = mockedStartSession.mock.calls[0][0];
    expect(body).toMatchObject({
      kind: 'manual',
      repository_id: 'gemba',
      prompt: 'Quick poke around',
    });
    expect(body.persona_id).toBeUndefined();
  });
});

describe('NewSessionDialog bead-mode happy path', () => {
  it('submits the bead envelope with kind: bead', async () => {
    mockedUseWorkItems.mockReturnValue({
      data: [
        {
          id: 'gm-7',
          title: 'pick me',
          kind: 'task',
          status: 'open',
          state_category: 'unstarted',
          repository_ids: [],
          created_at: '2026-04-26T00:00:00Z',
          updated_at: '2026-04-26T00:00:00Z',
        },
      ],
    });
    render(wrap(<NewSessionDialog open onClose={() => {}} />));
    fireEvent.change(screen.getByTestId('new-session-bead'), {
      target: { value: 'gm-7' },
    });
    // Agent auto-defaults to claude.
    const submit = (await screen.findByTestId('new-session-submit')) as HTMLButtonElement;
    await waitFor(() => expect(submit.disabled).toBe(false));
    act(() => {
      submit.click();
    });
    await waitFor(() => expect(mockedStartSession).toHaveBeenCalled());
    expect(mockedStartSession.mock.calls[0][0]).toMatchObject({
      kind: 'bead',
      bead_id: 'gm-7',
      agent_type: 'claude',
    });
  });
});
