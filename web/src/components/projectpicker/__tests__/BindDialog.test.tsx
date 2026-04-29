// BindDialog tests (gm-root.17.14).
//
// Coverage:
//   - renders both kinds with their respective body copy
//   - submit calls bindProject with mode="create" and the entry's path
//     for both beads_db_path and target_repo_path
//   - submit calls bindProject with mode="navigate" and the operator-
//     supplied target_repo_path
//   - error path surfaces the server message
//   - cancel closes without making a network call

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { ProjectPickerProvider } from '../ProjectPickerContext';
import { BindDialog } from '../BindDialog';
import type { ProjectEntry } from '@/api/projects';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  return <ProjectPickerProvider>{children}</ProjectPickerProvider>;
}

const needsWorkspaceEntry: ProjectEntry = {
  name: 'orphan',
  path: '/abs/projects/orphan',
  kind: 'needs_workspace',
};

const needsRepoEntry: ProjectEntry = {
  name: 'looseworkspace',
  path: '/abs/projects/looseworkspace',
  kind: 'needs_repo',
};

describe('BindDialog', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
    // The provider mounts and fetches /v1/projects on mount; resolve
    // with an empty list so the picker context settles.
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('does not render when open=false', () => {
    render(
      <Wrapper>
        <BindDialog
          open={false}
          entry={needsWorkspaceEntry}
          onClose={() => {}}
        />
      </Wrapper>
    );
    expect(screen.queryByTestId('bind-dialog')).toBeNull();
  });

  it('renders title with the entry name', async () => {
    render(
      <Wrapper>
        <BindDialog
          open={true}
          entry={needsWorkspaceEntry}
          onClose={() => {}}
        />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('bind-dialog'));
    expect(screen.getByTestId('bind-dialog').textContent).toContain('orphan');
  });

  it('shows "needs_workspace" body copy for that kind', async () => {
    render(
      <Wrapper>
        <BindDialog
          open={true}
          entry={needsWorkspaceEntry}
          onClose={() => {}}
        />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('bind-dialog-description'));
    expect(screen.getByTestId('bind-dialog-description').textContent).toContain(
      "doesn't have a .gemba/workspace.toml"
    );
  });

  it('shows "needs_repo" body copy for that kind', async () => {
    render(
      <Wrapper>
        <BindDialog
          open={true}
          entry={needsRepoEntry}
          onClose={() => {}}
        />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('bind-dialog-description'));
    expect(screen.getByTestId('bind-dialog-description').textContent).toContain(
      "isn't in a git repo"
    );
  });

  it('confirm in create mode posts to /v1/projects/bind with mode=create', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (typeof url === 'string' && url.includes('/v1/projects/bind')) {
        return jsonResponse({
          name: 'orphan',
          path: '/abs/projects/orphan',
          active: true,
          kind: 'complete',
        });
      }
      return jsonResponse({ projects: [], total: 0 });
    });
    const onClose = vi.fn();
    render(
      <Wrapper>
        <BindDialog
          open={true}
          entry={needsWorkspaceEntry}
          onClose={onClose}
        />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('bind-dialog'));

    await act(async () => {
      fireEvent.click(screen.getByTestId('bind-dialog-confirm'));
    });

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    const bindCall = fetchSpy.mock.calls.find(
      ([url]) => typeof url === 'string' && url.includes('/v1/projects/bind')
    );
    expect(bindCall).toBeTruthy();
    const init = bindCall![1] as RequestInit;
    expect(init.method).toBe('POST');
    const headers = init.headers as Record<string, string>;
    expect(headers['X-GEMBA-Confirm']).toBeTruthy();
    expect(headers['Content-Type']).toBe('application/json');
    const body = JSON.parse(init.body as string);
    expect(body).toEqual({
      beads_db_path: '/abs/projects/orphan',
      target_repo_path: '/abs/projects/orphan',
      mode: 'create',
    });
  });

  it('confirm in navigate mode posts to /v1/projects/bind with the supplied path', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (typeof url === 'string' && url.includes('/v1/projects/bind')) {
        return jsonResponse({
          name: 'orphan',
          path: '/abs/projects/orphan',
          active: true,
          kind: 'complete',
        });
      }
      return jsonResponse({ projects: [], total: 0 });
    });
    const onClose = vi.fn();
    render(
      <Wrapper>
        <BindDialog
          open={true}
          entry={needsWorkspaceEntry}
          onClose={onClose}
        />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('bind-dialog'));

    fireEvent.click(screen.getByTestId('bind-dialog-mode-navigate'));
    await waitFor(() => screen.getByTestId('bind-dialog-target-repo-path'));
    fireEvent.change(screen.getByTestId('bind-dialog-target-repo-path'), {
      target: { value: '/abs/path/to/repo' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('bind-dialog-confirm'));
    });

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    const bindCall = fetchSpy.mock.calls.find(
      ([url]) => typeof url === 'string' && url.includes('/v1/projects/bind')
    );
    expect(bindCall).toBeTruthy();
    const body = JSON.parse((bindCall![1] as RequestInit).body as string);
    expect(body).toEqual({
      beads_db_path: '/abs/projects/orphan',
      target_repo_path: '/abs/path/to/repo',
      mode: 'navigate',
    });
  });

  it('navigate mode disables confirm until target path is filled', async () => {
    render(
      <Wrapper>
        <BindDialog
          open={true}
          entry={needsWorkspaceEntry}
          onClose={() => {}}
        />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('bind-dialog'));

    fireEvent.click(screen.getByTestId('bind-dialog-mode-navigate'));
    const confirm = screen.getByTestId('bind-dialog-confirm') as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);

    fireEvent.change(screen.getByTestId('bind-dialog-target-repo-path'), {
      target: { value: '/some/repo' },
    });
    expect(confirm.disabled).toBe(false);
  });

  it('error path surfaces the server message', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (typeof url === 'string' && url.includes('/v1/projects/bind')) {
        return jsonResponse(
          { error: 'not_a_git_repo', message: 'target_repo_path is not a git worktree' },
          400
        );
      }
      return jsonResponse({ projects: [], total: 0 });
    });
    const onClose = vi.fn();
    render(
      <Wrapper>
        <BindDialog
          open={true}
          entry={needsWorkspaceEntry}
          onClose={onClose}
        />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('bind-dialog'));

    await act(async () => {
      fireEvent.click(screen.getByTestId('bind-dialog-confirm'));
    });

    await waitFor(() => screen.getByTestId('bind-dialog-error'));
    expect(screen.getByTestId('bind-dialog-error').textContent).toContain(
      'not a git worktree'
    );
    // Failure does not auto-close — operator gets to retry or cancel.
    expect(onClose).not.toHaveBeenCalled();
  });

  it('cancel closes without making a network call', async () => {
    const onClose = vi.fn();
    render(
      <Wrapper>
        <BindDialog
          open={true}
          entry={needsWorkspaceEntry}
          onClose={onClose}
        />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('bind-dialog'));

    fireEvent.click(screen.getByTestId('bind-dialog-cancel'));
    expect(onClose).toHaveBeenCalled();
    // No bind call fired.
    const bindCall = fetchSpy.mock.calls.find(
      ([url]) => typeof url === 'string' && url.includes('/v1/projects/bind')
    );
    expect(bindCall).toBeFalsy();
  });
});
