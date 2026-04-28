// RatifyDoneScreen component tests (gm-root.17.7).
//
// Covers:
//   - renders the project name, path, and seeded counts
//   - Start planning CTA navigates to /walk
//   - Skip CTA navigates to /gemba
//   - zero-count edge cases render without error

import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { RatifyDoneScreen } from '../RatifyDoneScreen';

// Stub lucide-react icons so jsdom doesn't choke on SVG rendering.
vi.mock('lucide-react', async () => {
  const actual = await vi.importActual<typeof import('lucide-react')>('lucide-react');
  return {
    ...actual,
    CheckCircle2: ({ className, ...rest }: Record<string, unknown>) => (
      <span data-testid="icon-check" className={String(className ?? '')} {...rest} />
    ),
    Map: ({ className, ...rest }: Record<string, unknown>) => (
      <span data-testid="icon-map" className={String(className ?? '')} {...rest} />
    ),
    SkipForward: ({ className, ...rest }: Record<string, unknown>) => (
      <span data-testid="icon-skip" className={String(className ?? '')} {...rest} />
    ),
  };
});

function LocationProbe(): JSX.Element {
  const loc = useLocation();
  return <div data-testid="probe-pathname">{loc.pathname}</div>;
}

interface WrapProps {
  projectName?: string;
  projectPath?: string;
  milestoneCount?: number;
  epicCount?: number;
}

function wrap({
  projectName = 'my-project',
  projectPath = '/tmp/fake-projects/my-project',
  milestoneCount = 3,
  epicCount = 7,
}: WrapProps = {}): JSX.Element {
  return (
    <MemoryRouter initialEntries={['/new']}>
      <Routes>
        <Route
          path="/new"
          element={
            <RatifyDoneScreen
              projectName={projectName}
              projectPath={projectPath}
              milestoneCount={milestoneCount}
              epicCount={epicCount}
            />
          }
        />
        <Route path="/walk" element={<div data-testid="walk-page">Walk</div>} />
        <Route path="/gemba" element={<div data-testid="gemba-page">Gemba</div>} />
      </Routes>
      <LocationProbe />
    </MemoryRouter>
  );
}

describe('RatifyDoneScreen', () => {
  it('renders the project name in the headline', () => {
    render(wrap({ projectName: 'my-crm' }));
    expect(screen.getByTestId('ratify-done-project-name').textContent).toContain('my-crm');
  });

  it('renders the project path', () => {
    render(wrap({ projectPath: '/workspace/projects/my-crm' }));
    expect(screen.getByTestId('ratify-done-path').textContent).toContain(
      '/workspace/projects/my-crm'
    );
  });

  it('renders the milestone and epic summary line when counts are positive', () => {
    render(wrap({ milestoneCount: 3, epicCount: 7 }));
    const summary = screen.getByTestId('ratify-done-summary');
    expect(summary.textContent).toContain('3');
    expect(summary.textContent).toContain('milestone');
    expect(summary.textContent).toContain('7');
    expect(summary.textContent).toContain('epic');
  });

  it('does not render the summary line when milestone count is zero', () => {
    render(wrap({ milestoneCount: 0, epicCount: 0 }));
    expect(screen.queryByTestId('ratify-done-summary')).toBeNull();
  });

  it('uses singular "milestone" when count is 1', () => {
    render(wrap({ milestoneCount: 1, epicCount: 2 }));
    const summary = screen.getByTestId('ratify-done-summary');
    expect(summary.textContent).toContain('1 milestone');
    expect(summary.textContent).not.toContain('1 milestones');
  });

  it('uses singular "epic" when count is 1', () => {
    render(wrap({ milestoneCount: 2, epicCount: 1 }));
    const summary = screen.getByTestId('ratify-done-summary');
    expect(summary.textContent).toContain('1 epic');
    expect(summary.textContent).not.toContain('1 epics');
  });

  it('renders both CTAs', () => {
    render(wrap());
    expect(screen.getByTestId('ratify-done-start-planning')).toBeTruthy();
    expect(screen.getByTestId('ratify-done-skip')).toBeTruthy();
  });

  it('Start planning navigates to /walk', async () => {
    render(wrap());
    fireEvent.click(screen.getByTestId('ratify-done-start-planning'));
    await waitFor(() =>
      expect(screen.getByTestId('probe-pathname').textContent).toBe('/walk')
    );
  });

  it('Skip navigates to /gemba', async () => {
    render(wrap());
    fireEvent.click(screen.getByTestId('ratify-done-skip'));
    await waitFor(() =>
      expect(screen.getByTestId('probe-pathname').textContent).toBe('/gemba')
    );
  });

  it('renders the success icon', () => {
    render(wrap());
    expect(screen.getByTestId('ratify-done-icon')).toBeTruthy();
  });

  it('falls back to "Project" when project name is empty', () => {
    render(wrap({ projectName: '' }));
    expect(screen.getByTestId('ratify-done-project-name').textContent).toContain('Project');
  });
});
