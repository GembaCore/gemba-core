import { describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import {
  driftSeverity,
  maxSeverity,
  pressureSeverity,
  SessionHealthPanel,
  timeOnTaskSeverity,
} from '../SessionHealthPanel';
import type { PlannerOperationalContext } from '@/api/planner';

const HOUR_NS = 3600 * 1e9;

function ctx(
  id: string,
  health: { context_pressure: number; concept_drift: number; time_on_task_ns: number } | null,
): PlannerOperationalContext {
  return {
    session: { id, assignment_id: id, agent_id: id, status: 'working', started_at: '' },
    agent: { id, name: id, agent_kind: 'agent', role: 'crew' },
    health,
  };
}

describe('threshold predicates', () => {
  it('pressureSeverity matches spec thresholds', () => {
    expect(pressureSeverity(0.5)).toBe('ok');
    expect(pressureSeverity(0.6)).toBe('ok'); // strict >
    expect(pressureSeverity(0.61)).toBe('warn');
    expect(pressureSeverity(0.8)).toBe('warn'); // strict >
    expect(pressureSeverity(0.81)).toBe('recycle');
  });

  it('driftSeverity matches spec thresholds', () => {
    expect(driftSeverity(0.4)).toBe('ok');
    expect(driftSeverity(0.5)).toBe('ok'); // strict >
    expect(driftSeverity(0.51)).toBe('warn');
    expect(driftSeverity(0.7)).toBe('warn'); // strict >
    expect(driftSeverity(0.71)).toBe('recycle');
  });

  it('timeOnTaskSeverity flags > 4h', () => {
    expect(timeOnTaskSeverity(0)).toBe('ok');
    expect(timeOnTaskSeverity(4 * HOUR_NS)).toBe('ok'); // strict >
    expect(timeOnTaskSeverity(4.1 * HOUR_NS)).toBe('recycle');
  });

  it('maxSeverity returns the worst of pressure/drift/time', () => {
    expect(
      maxSeverity(ctx('s1', { context_pressure: 0.1, concept_drift: 0.1, time_on_task_ns: 0 })),
    ).toBe('ok');
    expect(
      maxSeverity(
        ctx('s1', { context_pressure: 0.65, concept_drift: 0.1, time_on_task_ns: 0 }),
      ),
    ).toBe('warn');
    expect(
      maxSeverity(
        ctx('s1', { context_pressure: 0.65, concept_drift: 0.85, time_on_task_ns: 0 }),
      ),
    ).toBe('recycle');
  });

  it('maxSeverity tolerates null health', () => {
    expect(maxSeverity(ctx('s1', null))).toBe('ok');
  });
});

describe('SessionHealthPanel', () => {
  it('renders one row per session', () => {
    render(
      <SessionHealthPanel
        sessions={[
          ctx('s1', { context_pressure: 0.2, concept_drift: 0.1, time_on_task_ns: HOUR_NS }),
          ctx('s2', { context_pressure: 0.65, concept_drift: 0.55, time_on_task_ns: 2 * HOUR_NS }),
        ]}
      />,
    );
    expect(screen.getByTestId('session-health-row-s1')).toBeTruthy();
    expect(screen.getByTestId('session-health-row-s2')).toBeTruthy();
  });

  it('tags a session over the recycle pressure threshold', () => {
    render(
      <SessionHealthPanel
        sessions={[
          ctx('s1', { context_pressure: 0.9, concept_drift: 0.1, time_on_task_ns: 0 }),
        ]}
      />,
    );
    const row = screen.getByTestId('session-health-row-s1');
    expect(row.getAttribute('data-severity')).toBe('recycle');
    expect(within(row).getByText('RECYCLE')).toBeTruthy();
  });

  it('tags a session in the warn band', () => {
    render(
      <SessionHealthPanel
        sessions={[
          ctx('s1', { context_pressure: 0.65, concept_drift: 0.1, time_on_task_ns: 0 }),
        ]}
      />,
    );
    expect(screen.getByTestId('session-health-row-s1').getAttribute('data-severity')).toBe('warn');
  });

  it('tags an OK session as ok', () => {
    render(
      <SessionHealthPanel
        sessions={[
          ctx('s1', { context_pressure: 0.1, concept_drift: 0.1, time_on_task_ns: 0 }),
        ]}
      />,
    );
    expect(screen.getByTestId('session-health-row-s1').getAttribute('data-severity')).toBe('ok');
  });

  it('renders em-dash placeholders for null health', () => {
    render(<SessionHealthPanel sessions={[ctx('s1', null)]} />);
    const row = screen.getByTestId('session-health-row-s1');
    expect(within(row).getAllByText('—').length).toBeGreaterThanOrEqual(3);
  });

  it('shows empty state when no sessions', () => {
    render(<SessionHealthPanel sessions={[]} />);
    expect(screen.getByText('No live sessions.')).toBeTruthy();
  });

  it('sorts by severity desc when sortBySeverity is set', () => {
    render(
      <SessionHealthPanel
        sortBySeverity
        sessions={[
          ctx('ok-session', {
            context_pressure: 0.1,
            concept_drift: 0.1,
            time_on_task_ns: 0,
          }),
          ctx('recycle-session', {
            context_pressure: 0.9,
            concept_drift: 0.1,
            time_on_task_ns: 0,
          }),
          ctx('warn-session', {
            context_pressure: 0.65,
            concept_drift: 0.1,
            time_on_task_ns: 0,
          }),
        ]}
      />,
    );
    const rows = screen.getAllByTestId(/session-health-row-/);
    expect(rows.map((r) => r.getAttribute('data-testid'))).toEqual([
      'session-health-row-recycle-session',
      'session-health-row-warn-session',
      'session-health-row-ok-session',
    ]);
  });

  it('preserves input order without sortBySeverity', () => {
    render(
      <SessionHealthPanel
        sessions={[
          ctx('first', { context_pressure: 0.1, concept_drift: 0.1, time_on_task_ns: 0 }),
          ctx('second', { context_pressure: 0.9, concept_drift: 0.1, time_on_task_ns: 0 }),
        ]}
      />,
    );
    const rows = screen.getAllByTestId(/session-health-row-/);
    expect(rows[0].getAttribute('data-testid')).toBe('session-health-row-first');
    expect(rows[1].getAttribute('data-testid')).toBe('session-health-row-second');
  });

  it('respects testid override', () => {
    render(<SessionHealthPanel sessions={[]} testid="custom" />);
    expect(screen.getByTestId('custom')).toBeTruthy();
  });
});
