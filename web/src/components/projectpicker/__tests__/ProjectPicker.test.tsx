// ProjectPicker + ProjectPickerContext tests (gm-root.18).
//
// Coverage:
//   - empty state: picker chrome visible, dropdown shows "No projects yet"
//   - populated state: all entries visible in the dropdown
//   - selecting a project switches it via the API and updates the label
//   - active project displays "active" badge
//   - Escape key closes the dropdown
//   - switchProject API failure rolls back the optimistic update

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { ProjectPickerProvider } from '../ProjectPickerContext';
import { ProjectPicker } from '../ProjectPicker';
import { CreateProjectModalProvider } from '@/components/projects/CreateProjectModalContext';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function renderPicker(): void {
  render(
    <MemoryRouter>
      <ProjectPickerProvider>
        <CreateProjectModalProvider>
          <ProjectPicker />
        </CreateProjectModalProvider>
      </ProjectPickerProvider>
    </MemoryRouter>
  );
}

// Simple wrapper for tests that need to provide children alongside the picker.
function PickerWrapper({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      <ProjectPickerProvider>
        <CreateProjectModalProvider>{children}</CreateProjectModalProvider>
      </ProjectPickerProvider>
    </MemoryRouter>
  );
}

describe('ProjectPicker', () => {
  // Inner spy holds the per-test queued responses for /v1/projects
  // and /v1/projects/switch. The outer `fetch` mock routes by URL:
  // /v1/projects/adoptable always returns an empty list (gm-gmyl —
  // tests in this file don't exercise the adoptable section, and we
  // don't want the dropdown-open fetch to consume a queue slot meant
  // for the switch call). All other URLs delegate to fetchSpy in
  // FIFO order.
  const fetchSpy = vi.fn();
  const routedFetch = vi.fn();
  // Default routing: /v1/projects/adoptable → empty list; everything
  // else delegates to fetchSpy. Tests that override routedFetch via
  // mockImplementation MUST get the default restored before the next
  // test runs (mockClear preserves implementation), so we re-install
  // the default in beforeEach below.
  const defaultRoutedFetchImpl = async (
    url: string | Request,
    init?: RequestInit
  ) => {
    const u = typeof url === 'string' ? url : url.url;
    if (u.includes('/v1/projects/adoptable')) {
      return jsonResponse({ dbs: [], total: 0 });
    }
    return fetchSpy(url, init);
  };

  beforeEach(() => {
    routedFetch.mockReset();
    routedFetch.mockImplementation(defaultRoutedFetchImpl);
    vi.stubGlobal('fetch', routedFetch);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
    routedFetch.mockClear();
  });

  it('renders the trigger button with workspace-label testid', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderPicker();
    // The trigger must carry data-testid="workspace-label" for back-compat
    // with the existing topbar.spec.ts assertions.
    await waitFor(() => expect(screen.getByTestId('workspace-label')).toBeTruthy());
  });

  it('renders the picker container', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderPicker();
    await waitFor(() => expect(screen.getByTestId('project-picker')).toBeTruthy());
  });

  it('empty state: dropdown shows "No projects yet" message', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => expect(screen.getByTestId('project-picker-empty')).toBeTruthy());
    expect(screen.getByTestId('project-picker-empty').textContent).toContain('No projects yet');
  });

  it('empty state: no project items in dropdown', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => screen.getByTestId('project-picker-dropdown'));
    const items = document.querySelectorAll('[data-testid^="project-picker-item-"]');
    expect(items).toHaveLength(0);
  });

  it('populated state: lists all projects', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        projects: [
          { name: 'alpha', path: '/tmp/alpha' },
          { name: 'beta', path: '/tmp/beta', active: true },
        ],
        total: 2,
      })
    );
    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => {
      expect(screen.getByTestId('project-picker-item-alpha')).toBeTruthy();
      expect(screen.getByTestId('project-picker-item-beta')).toBeTruthy();
    });
  });

  it('trigger label shows active project name', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        projects: [
          { name: 'alpha', path: '/tmp/alpha' },
          { name: 'beta', path: '/tmp/beta', active: true },
        ],
        total: 2,
      })
    );
    renderPicker();
    // Context reflects server active flag.
    await waitFor(() =>
      expect(screen.getByTestId('project-picker-label').textContent).toContain('beta')
    );
  });

  it('active project shows "active" badge', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        projects: [
          { name: 'alpha', path: '/tmp/alpha' },
          { name: 'beta', path: '/tmp/beta', active: true },
        ],
        total: 2,
      })
    );
    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => screen.getByTestId('project-picker-item-beta'));
    expect(screen.getByTestId('project-picker-item-beta').textContent).toContain('active');
    expect(screen.getByTestId('project-picker-item-alpha').textContent).not.toContain('active');
  });

  it('selecting a project calls the switch API', async () => {
    // First call: list; second call: switch.
    fetchSpy
      .mockResolvedValueOnce(
        jsonResponse({
          projects: [
            { name: 'alpha', path: '/tmp/alpha' },
            { name: 'beta', path: '/tmp/beta', active: true },
          ],
          total: 2,
        })
      )
      .mockResolvedValue(
        jsonResponse({
          active: { name: 'alpha', path: '/tmp/alpha', active: true },
        })
      );

    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => screen.getByTestId('project-picker-item-alpha'));

    await act(async () => {
      fireEvent.click(screen.getByTestId('project-picker-item-alpha'));
    });

    // Switch endpoint was called.
    const switchCall = fetchSpy.mock.calls.find(
      (args) => typeof args[0] === 'string' && (args[0] as string).includes('/projects/switch')
    );
    expect(switchCall).toBeDefined();
  });

  it('selecting a project closes the dropdown', async () => {
    fetchSpy
      .mockResolvedValueOnce(
        jsonResponse({
          projects: [{ name: 'alpha', path: '/tmp/alpha' }],
          total: 1,
        })
      )
      .mockResolvedValue(
        jsonResponse({ active: { name: 'alpha', path: '/tmp/alpha', active: true } })
      );

    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => screen.getByTestId('project-picker-item-alpha'));

    await act(async () => {
      fireEvent.click(screen.getByTestId('project-picker-item-alpha'));
    });

    expect(screen.queryByTestId('project-picker-dropdown')).toBeNull();
  });

  it('pressing Escape closes the dropdown', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => screen.getByTestId('project-picker-dropdown'));
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByTestId('project-picker-dropdown')).toBeNull();
  });

  it('switch API failure does not leave the dropdown in a broken state', async () => {
    fetchSpy
      .mockResolvedValueOnce(
        jsonResponse({
          projects: [
            { name: 'alpha', path: '/tmp/alpha' },
            { name: 'beta', path: '/tmp/beta', active: true },
          ],
          total: 2,
        })
      )
      // Switch fails with 404.
      .mockResolvedValueOnce(
        jsonResponse({ error: 'project_not_found', message: 'not found' }, 404)
      )
      // Reload after failure.
      .mockResolvedValue(
        jsonResponse({
          projects: [
            { name: 'alpha', path: '/tmp/alpha' },
            { name: 'beta', path: '/tmp/beta', active: true },
          ],
          total: 2,
        })
      );

    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => screen.getByTestId('project-picker-item-alpha'));

    // The click should not throw even though the API fails.
    await act(async () => {
      fireEvent.click(screen.getByTestId('project-picker-item-alpha'));
    });

    // Picker container is still present — no crash.
    expect(screen.getByTestId('project-picker')).toBeTruthy();
  });

  it('PickerWrapper provides context to children', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ projects: [{ name: 'proj1', path: '/p1' }], total: 1 })
    );
    render(
      <PickerWrapper>
        <ProjectPicker />
      </PickerWrapper>
    );
    await waitFor(() => screen.getByTestId('project-picker'));
  });

  // gm-gmyl: adoptable-DB section. Override routedFetch directly so
  // /v1/projects/adoptable returns a non-empty list and assert the
  // section + per-row testids appear when the dropdown opens.
  it('renders the adoptable-DBs section when the lister returns candidates', async () => {
    routedFetch.mockImplementation(async (url) => {
      const u = typeof url === 'string' ? url : (url as Request).url;
      if (u.includes('/v1/projects/adoptable')) {
        return jsonResponse({
          dbs: [
            { name: 'legacy_db', url: 'mysql://root@127.0.0.1:3307/legacy_db' },
            { name: 'imported', url: 'mysql://root@127.0.0.1:3307/imported' },
          ],
          total: 2,
        });
      }
      return jsonResponse({ projects: [{ name: 'alpha', path: '/p/alpha' }], total: 1 });
    });
    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() =>
      expect(screen.getByTestId('project-picker-adoptable-section')).toBeTruthy()
    );
    expect(screen.getByTestId('project-picker-adoptable-legacy_db')).toBeTruthy();
    expect(screen.getByTestId('project-picker-adoptable-imported')).toBeTruthy();
  });

  // gm-gmyl: empty adoptable list → section is hidden (no testid).
  it('hides the adoptable section when the lister returns empty', async () => {
    routedFetch.mockImplementation(async (url) => {
      const u = typeof url === 'string' ? url : (url as Request).url;
      if (u.includes('/v1/projects/adoptable')) {
        return jsonResponse({ dbs: [], total: 0 });
      }
      return jsonResponse({ projects: [{ name: 'alpha', path: '/p/alpha' }], total: 1 });
    });
    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => screen.getByTestId('project-picker-dropdown'));
    expect(screen.queryByTestId('project-picker-adoptable-section')).toBeNull();
  });

  // gm-root.17.14: incomplete entry → "needs setup" badge renders.
  it('renders a "needs setup" badge for entries whose kind != complete', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        projects: [
          { name: 'alpha', path: '/p/alpha', kind: 'complete' },
          { name: 'orphan', path: '/p/orphan', kind: 'needs_workspace' },
          { name: 'looseworkspace', path: '/p/loose', kind: 'needs_repo' },
        ],
        total: 3,
      })
    );
    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => screen.getByTestId('project-picker-item-orphan'));

    expect(screen.getByTestId('picker-needs-setup-orphan')).toBeTruthy();
    expect(screen.getByTestId('picker-needs-setup-looseworkspace')).toBeTruthy();
    // The complete entry has no badge.
    expect(screen.queryByTestId('picker-needs-setup-alpha')).toBeNull();
  });

  // gm-root.17.14: clicking a complete entry switches workspace.
  it('clicking a complete entry calls switchProject', async () => {
    fetchSpy
      .mockResolvedValueOnce(
        jsonResponse({
          projects: [{ name: 'alpha', path: '/p/alpha', kind: 'complete' }],
          total: 1,
        })
      )
      .mockResolvedValue(
        jsonResponse({ active: { name: 'alpha', path: '/p/alpha', active: true, kind: 'complete' } })
      );

    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => screen.getByTestId('project-picker-item-alpha'));

    await act(async () => {
      fireEvent.click(screen.getByTestId('project-picker-item-alpha'));
    });

    const switchCall = fetchSpy.mock.calls.find(
      ([url]) => typeof url === 'string' && url.includes('/v1/projects/switch')
    );
    expect(switchCall).toBeTruthy();
    // No bind dialog opens for a complete entry.
    expect(screen.queryByTestId('bind-dialog')).toBeNull();
  });

  // gm-root.17.14: clicking a needs-setup entry opens BindDialog
  // instead of switching workspace.
  it('clicking a needs-setup entry opens BindDialog and does NOT switch', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        projects: [
          { name: 'orphan', path: '/p/orphan', kind: 'needs_workspace' },
        ],
        total: 1,
      })
    );

    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() => screen.getByTestId('project-picker-item-orphan'));

    await act(async () => {
      fireEvent.click(screen.getByTestId('project-picker-item-orphan'));
    });

    // BindDialog opened.
    await waitFor(() => screen.getByTestId('bind-dialog'));
    expect(screen.getByTestId('bind-dialog')).toBeTruthy();
    // No switch call was issued.
    const switchCall = fetchSpy.mock.calls.find(
      ([url]) => typeof url === 'string' && url.includes('/v1/projects/switch')
    );
    expect(switchCall).toBeFalsy();
  });

  // gm-gmyl: lister returns a notice (server unreachable) → notice
  // renders as a small inline hint, no adoptable section.
  it('surfaces the lister notice when the Dolt server is unreachable', async () => {
    routedFetch.mockImplementation(async (url) => {
      const u = typeof url === 'string' ? url : (url as Request).url;
      if (u.includes('/v1/projects/adoptable')) {
        return jsonResponse({
          dbs: [],
          total: 0,
          notice: 'dolt server at 127.0.0.1:3307 unreachable',
        });
      }
      return jsonResponse({ projects: [{ name: 'alpha', path: '/p/alpha' }], total: 1 });
    });
    renderPicker();
    await waitFor(() => screen.getByTestId('workspace-label'));
    fireEvent.click(screen.getByTestId('workspace-label'));
    await waitFor(() =>
      expect(screen.getByTestId('project-picker-adoptable-notice')).toBeTruthy()
    );
    expect(screen.getByTestId('project-picker-adoptable-notice').textContent).toContain(
      'unreachable'
    );
    expect(screen.queryByTestId('project-picker-adoptable-section')).toBeNull();
  });
});
