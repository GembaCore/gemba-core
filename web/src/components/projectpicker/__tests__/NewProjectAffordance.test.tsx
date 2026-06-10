// NewProjectAffordance tests (gm-root.17.2 / gm-e12.21.3).
//
// Coverage:
//   - renders with the correct testid
//   - carries title attribute "Create new project"
//   - carries aria-label="Create new project"
//   - is reachable by role + accessible name
//   - renders immediately before the ProjectPicker in DOM order (sibling)
//   - clicking opens the unified Create-project modal (gm-e12.21.3
//     replaced the navigate-to-/new behaviour)

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { NewProjectAffordance } from '../NewProjectAffordance';
import { ProjectPicker } from '../ProjectPicker';
import { ProjectPickerProvider } from '../ProjectPickerContext';
import { CreateProjectModalProvider } from '@/components/projects/CreateProjectModalContext';

// ── helpers ────────────────────────────────────────────────────────────────

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// Renders just the affordance (no picker needed for attribute tests).
// Wraps in CreateProjectModalProvider so useCreateProjectModal() resolves.
function renderAffordanceOnly() {
  render(
    <MemoryRouter initialEntries={['/board']}>
      <ProjectPickerProvider>
        <CreateProjectModalProvider>
          <Routes>
            <Route path="*" element={<NewProjectAffordance />} />
          </Routes>
        </CreateProjectModalProvider>
      </ProjectPickerProvider>
    </MemoryRouter>
  );
}

// Renders [NewProjectAffordance, ProjectPicker] as siblings — mirrors the
// Topbar DOM order so DOM-position tests are meaningful.
function renderAffordanceWithPicker() {
  render(
    <MemoryRouter initialEntries={['/board']}>
      <ProjectPickerProvider>
        <CreateProjectModalProvider>
          <Routes>
            <Route
              path="*"
              element={
                <>
                  <NewProjectAffordance />
                  <ProjectPicker />
                </>
              }
            />
          </Routes>
        </CreateProjectModalProvider>
      </ProjectPickerProvider>
    </MemoryRouter>
  );
}

// ── tests ──────────────────────────────────────────────────────────────────

describe('NewProjectAffordance', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders the + button with the expected testid', () => {
    renderAffordanceOnly();
    expect(screen.getByTestId('new-project-affordance')).toBeTruthy();
  });

  it('has title "Create new project"', () => {
    renderAffordanceOnly();
    const btn = screen.getByTestId('new-project-affordance');
    expect(btn.getAttribute('title')).toBe('Create new project');
  });

  it('has aria-label "Create new project"', () => {
    renderAffordanceOnly();
    const btn = screen.getByTestId('new-project-affordance');
    expect(btn.getAttribute('aria-label')).toBe('Create new project');
  });

  it('is reachable by role + accessible name', () => {
    renderAffordanceOnly();
    expect(screen.getByRole('button', { name: 'Create new project' })).toBeTruthy();
  });

  it('clicking opens the Create-project modal', () => {
    renderAffordanceOnly();
    // Modal isn't mounted yet — Radix's Dialog renders nothing until
    // open=true. After click, the dialog content's testid should appear
    // in a portal.
    expect(screen.queryByTestId('create-project-modal')).toBeNull();
    fireEvent.click(screen.getByTestId('new-project-affordance'));
    expect(screen.getByTestId('create-project-modal')).toBeTruthy();
  });

  it('renders immediately before the ProjectPicker in DOM order', () => {
    renderAffordanceWithPicker();

    const affordance = screen.getByTestId('new-project-affordance');
    const picker = screen.getByTestId('project-picker');

    expect(affordance).toBeTruthy();
    expect(picker).toBeTruthy();

    // DOCUMENT_POSITION_FOLLOWING (4): picker comes AFTER affordance.
    const position = affordance.compareDocumentPosition(picker);
    expect(position & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
