// ConfigureProjectModal tests (gm-xwa8).
//
// Coverage:
//   - renders all field surfaces (name, beads_db, mode radios, submit).
//   - submit is disabled until required fields are filled.
//   - successful attach calls the API with the right body, fires
//     onAttached, and closes the modal.
//   - "create new repository at default location" mode shows the
//     preview path under defaultDir.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { ProjectPickerProvider } from '@/components/projectpicker/ProjectPickerContext';
import { ConfigureProjectModal } from '../ConfigureProjectModal';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  return <ProjectPickerProvider>{children}</ProjectPickerProvider>;
}

describe('ConfigureProjectModal', () => {
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

  it('renders all required fields when open', async () => {
    render(
      <Wrapper>
        <ConfigureProjectModal open={true} onClose={() => {}} />
      </Wrapper>
    );

    await waitFor(() => screen.getByTestId('configure-project-modal'));
    expect(screen.getByTestId('configure-project-name')).toBeTruthy();
    expect(screen.getByTestId('configure-project-beadsdb')).toBeTruthy();
    expect(screen.getByTestId('configure-project-mode-existing')).toBeTruthy();
    expect(screen.getByTestId('configure-project-mode-create')).toBeTruthy();
    expect(screen.getByTestId('configure-project-submit')).toBeTruthy();
  });

  it('does not render when open=false', () => {
    render(
      <Wrapper>
        <ConfigureProjectModal open={false} onClose={() => {}} />
      </Wrapper>
    );
    expect(screen.queryByTestId('configure-project-modal')).toBeNull();
  });

  it('submit is disabled until name + beads_db + repo_path are filled (existing mode)', async () => {
    render(
      <Wrapper>
        <ConfigureProjectModal open={true} onClose={() => {}} />
      </Wrapper>
    );

    await waitFor(() => screen.getByTestId('configure-project-modal'));
    const submit = screen.getByTestId('configure-project-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);

    fireEvent.change(screen.getByTestId('configure-project-name'), {
      target: { value: 'my-proj' },
    });
    expect(submit.disabled).toBe(true);

    fireEvent.change(screen.getByTestId('configure-project-beadsdb'), {
      target: { value: 'mysql://root@127.0.0.1:3307/my_proj' },
    });
    expect(submit.disabled).toBe(true);

    fireEvent.change(screen.getByTestId('configure-project-repopath'), {
      target: { value: '/abs/path/to/repo' },
    });
    expect(submit.disabled).toBe(false);
  });

  it('shows create-at preview when create mode is selected', async () => {
    render(
      <Wrapper>
        <ConfigureProjectModal
          open={true}
          onClose={() => {}}
          defaultDir="/home/op/gemba/projects"
        />
      </Wrapper>
    );

    await waitFor(() => screen.getByTestId('configure-project-modal'));
    fireEvent.change(screen.getByTestId('configure-project-name'), {
      target: { value: 'adopted' },
    });
    fireEvent.click(screen.getByTestId('configure-project-mode-create'));

    const preview = screen.getByTestId('configure-project-createat-preview');
    expect(preview.textContent).toBe('/home/op/gemba/projects/adopted/');
  });

  it('successful attach calls API, fires onAttached, and closes', async () => {
    const onClose = vi.fn();
    const onAttached = vi.fn();

    // Two-call flow: provider's listProjects (already mocked to empty),
    // then our attach call. Use a sequence so the first response goes
    // to whichever runs first; here we assert by URL.
    fetchSpy.mockImplementation(async (url: string) => {
      if (typeof url === 'string' && url.includes('/v1/projects/attach')) {
        return jsonResponse(
          { name: 'my-proj', path: '/abs/path/to/repo', active: true },
          201
        );
      }
      return jsonResponse({ projects: [], total: 0 });
    });

    render(
      <Wrapper>
        <ConfigureProjectModal
          open={true}
          onClose={onClose}
          onAttached={onAttached}
        />
      </Wrapper>
    );

    await waitFor(() => screen.getByTestId('configure-project-modal'));
    fireEvent.change(screen.getByTestId('configure-project-name'), {
      target: { value: 'my-proj' },
    });
    fireEvent.change(screen.getByTestId('configure-project-beadsdb'), {
      target: { value: 'mysql://root@127.0.0.1:3307/my_proj' },
    });
    fireEvent.change(screen.getByTestId('configure-project-repopath'), {
      target: { value: '/abs/path/to/repo' },
    });
    fireEvent.click(screen.getByTestId('configure-project-submit'));

    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(onAttached).toHaveBeenCalledWith('my-proj');

    // Inspect the attach call.
    const attachCall = fetchSpy.mock.calls.find(
      ([url]) => typeof url === 'string' && url.includes('/v1/projects/attach')
    );
    expect(attachCall).toBeTruthy();
    const init = attachCall![1] as RequestInit;
    expect(init.method).toBe('POST');
    const headers = init.headers as Record<string, string>;
    expect(headers['X-GEMBA-Confirm']).toBeTruthy();
    expect(headers['Content-Type']).toBe('application/json');
    const body = JSON.parse(init.body as string);
    expect(body).toEqual({
      beads_db: 'mysql://root@127.0.0.1:3307/my_proj',
      project_name: 'my-proj',
      repo_path: '/abs/path/to/repo',
    });
  });

  it('renders submit error message when API fails', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (typeof url === 'string' && url.includes('/v1/projects/attach')) {
        return jsonResponse(
          { error: 'beads_db_unreachable', message: 'could not reach beads_db: connection refused' },
          502
        );
      }
      return jsonResponse({ projects: [], total: 0 });
    });

    render(
      <Wrapper>
        <ConfigureProjectModal open={true} onClose={() => {}} />
      </Wrapper>
    );

    await waitFor(() => screen.getByTestId('configure-project-modal'));
    fireEvent.change(screen.getByTestId('configure-project-name'), {
      target: { value: 'my-proj' },
    });
    fireEvent.change(screen.getByTestId('configure-project-beadsdb'), {
      target: { value: 'mysql://root@127.0.0.1:3307/x' },
    });
    fireEvent.change(screen.getByTestId('configure-project-repopath'), {
      target: { value: '/abs/path/to/repo' },
    });
    fireEvent.click(screen.getByTestId('configure-project-submit'));

    await waitFor(() => screen.getByTestId('configure-project-error'));
    expect(screen.getByTestId('configure-project-error').textContent).toContain('connection refused');
  });
});
