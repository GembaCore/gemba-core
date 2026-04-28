// NewProjectAffordance tests (gm-root.17.2).
//
// Coverage:
//   - renders with the correct testid
//   - carries title attribute "Create new project"
//   - carries aria-label="Create new project"
//   - is reachable by role + accessible name
//   - renders immediately before the ProjectPicker in DOM order (sibling)
//   - clicking navigates to /new

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { NewProjectAffordance } from '../NewProjectAffordance';
import { ProjectPicker } from '../ProjectPicker';
import { ProjectPickerProvider } from '../ProjectPickerContext';

// ── helpers ────────────────────────────────────────────────────────────────

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// A minimal component that exposes the current route pathname for assertions.
function LocationDisplay() {
  const location = useLocation();
  return <div data-testid="location-display">{location.pathname}</div>;
}

// Renders just the affordance (no picker needed for attribute tests).
function renderAffordanceOnly() {
  render(
    <MemoryRouter initialEntries={['/board']}>
      <Routes>
        <Route path="*" element={<><NewProjectAffordance /><LocationDisplay /></>} />
      </Routes>
    </MemoryRouter>
  );
}

// Renders [NewProjectAffordance, ProjectPicker] as siblings — mirrors the
// Topbar DOM order so DOM-position tests are meaningful.
function renderAffordanceWithPicker(fetchSpy: ReturnType<typeof vi.fn>) {
  fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
  render(
    <MemoryRouter initialEntries={['/board']}>
      <Routes>
        <Route
          path="*"
          element={
            <ProjectPickerProvider>
              <NewProjectAffordance />
              <ProjectPicker />
              <LocationDisplay />
            </ProjectPickerProvider>
          }
        />
      </Routes>
    </MemoryRouter>
  );
}

// ── tests ──────────────────────────────────────────────────────────────────

describe('NewProjectAffordance', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
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

  it('clicking navigates to /new', () => {
    renderAffordanceOnly();
    // Start at /board — verify then navigate.
    expect(screen.getByTestId('location-display').textContent).toBe('/board');
    fireEvent.click(screen.getByTestId('new-project-affordance'));
    expect(screen.getByTestId('location-display').textContent).toBe('/new');
  });

  it('renders immediately before the ProjectPicker in DOM order', async () => {
    renderAffordanceWithPicker(fetchSpy);

    const affordance = screen.getByTestId('new-project-affordance');
    const picker = screen.getByTestId('project-picker');

    expect(affordance).toBeTruthy();
    expect(picker).toBeTruthy();

    // DOCUMENT_POSITION_FOLLOWING (4): picker comes AFTER affordance.
    const position = affordance.compareDocumentPosition(picker);
    expect(position & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
