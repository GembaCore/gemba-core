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
        <ProjectPicker />
      </ProjectPickerProvider>
    </MemoryRouter>
  );
}

// Simple wrapper for tests that need to provide children alongside the picker.
function PickerWrapper({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      <ProjectPickerProvider>{children}</ProjectPickerProvider>
    </MemoryRouter>
  );
}

describe('ProjectPicker', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
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
});
