// NewProjectPage tests — lightweight project-creation form
// (gm-root.17.13). The conversational flow that used to live here is
// now /onboard (covered by OnboardPage.test.tsx).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';

vi.mock('@/api/newproject', async () => {
  const actual =
    await vi.importActual<typeof import('@/api/newproject')>('@/api/newproject');
  return {
    ...actual,
    createProject: vi.fn(),
  };
});

vi.mock('@/components/projectpicker/ProjectPickerContext', () => ({
  useProjectPicker: () => ({
    projects: [],
    activeProject: null,
    isLoading: false,
    error: null,
    switchProject: vi.fn().mockResolvedValue(undefined),
    reload: vi.fn(),
  }),
}));

import { createProject as mockedCreateProject } from '@/api/newproject';
import { NewProjectPage } from '../NewProjectPage';

const createProjectMock = vi.mocked(mockedCreateProject);

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}</div>;
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/new']}>
      <Routes>
        <Route
          path="/new"
          element={
            <>
              <NewProjectPage />
              <LocationProbe />
            </>
          }
        />
        <Route path="/board" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>
  );
}

describe('NewProjectPage (lightweight create form)', () => {
  beforeEach(() => {
    createProjectMock.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the form with a name input and a description textarea', () => {
    renderPage();
    expect(screen.getByTestId('newproject-name')).toBeTruthy();
    expect(screen.getByTestId('newproject-description')).toBeTruthy();
    expect(screen.getByTestId('newproject-create')).toBeTruthy();
  });

  it('disables Create until a name is entered', () => {
    renderPage();
    const submit = screen.getByTestId('newproject-create') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);

    fireEvent.change(screen.getByTestId('newproject-name'), {
      target: { value: 'demo' },
    });
    expect(submit.disabled).toBe(false);
  });

  it('whitespace-only name does not enable Create', () => {
    renderPage();
    fireEvent.change(screen.getByTestId('newproject-name'), {
      target: { value: '   ' },
    });
    expect((screen.getByTestId('newproject-create') as HTMLButtonElement).disabled).toBe(
      true
    );
  });

  it('submits name + description, then navigates to /board', async () => {
    createProjectMock.mockResolvedValue({
      project_path: '/tmp/demo',
      project_name: 'demo',
      milestone_count: 0,
      epic_count: 0,
    });
    renderPage();
    fireEvent.change(screen.getByTestId('newproject-name'), {
      target: { value: 'demo' },
    });
    fireEvent.change(screen.getByTestId('newproject-description'), {
      target: { value: 'a project' },
    });
    fireEvent.click(screen.getByTestId('newproject-create'));

    await waitFor(() => {
      expect(createProjectMock).toHaveBeenCalledWith({
        project_name: 'demo',
        description: 'a project',
      });
    });
    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/board');
    });
  });

  it('renders the failure message when create returns an error', async () => {
    createProjectMock.mockRejectedValue(new Error('dir exists'));
    renderPage();
    fireEvent.change(screen.getByTestId('newproject-name'), {
      target: { value: 'demo' },
    });
    fireEvent.click(screen.getByTestId('newproject-create'));

    const err = await screen.findByTestId('newproject-error');
    expect(err.textContent).toMatch(/dir exists/);
    // No navigation on failure.
    expect(screen.getByTestId('location').textContent).toBe('/new');
  });

  it('trims surrounding whitespace before submit', async () => {
    createProjectMock.mockResolvedValue({
      project_path: '/tmp/clean',
      project_name: 'clean',
      milestone_count: 0,
      epic_count: 0,
    });
    renderPage();
    fireEvent.change(screen.getByTestId('newproject-name'), {
      target: { value: '  clean  ' },
    });
    fireEvent.change(screen.getByTestId('newproject-description'), {
      target: { value: '  trimmed  ' },
    });
    fireEvent.click(screen.getByTestId('newproject-create'));
    await waitFor(() => {
      expect(createProjectMock).toHaveBeenCalledWith({
        project_name: 'clean',
        description: 'trimmed',
      });
    });
  });
});
