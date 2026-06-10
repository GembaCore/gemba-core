// PlanReviewStep tests (gm-uipx.7).

import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { PlanReviewStep } from '../PlanReviewStep';
import type { BootstrapPlan } from '@/api/bootstrap';

function wrap(children: ReactNode): JSX.Element {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

const PLAN: BootstrapPlan = {
  analysis_id: 'a1',
  source: 'fresh',
  plan: [
    {
      id: 'epic-1',
      kind: 'epic',
      title: 'Onboard',
      children: [{ id: 'm1', kind: 'milestone', title: 'M1' }],
    },
  ],
  findings: [
    { id: 'f1', level: 'pass', title: 'OK' },
    { id: 'f2', level: 'warn', title: 'Watch out', detail: 'Detail body' },
    { id: 'f3', level: 'fail', title: 'Broken' },
  ],
};

describe('PlanReviewStep', () => {
  it('renders the side-by-side plan + report panes when plan is supplied', () => {
    render(
      wrap(
        <PlanReviewStep
          analysisId="a1"
          plan={PLAN}
          setPlan={() => {}}
          onBack={() => {}}
          onContinue={() => {}}
        />
      )
    );
    expect(screen.getByTestId('bootstrap-plan-pane')).toBeTruthy();
    expect(screen.getByTestId('bootstrap-report-pane')).toBeTruthy();
  });

  it('summary surfaces PASS / WARN / FAIL counts', () => {
    render(
      wrap(
        <PlanReviewStep
          analysisId="a1"
          plan={PLAN}
          setPlan={() => {}}
          onBack={() => {}}
          onContinue={() => {}}
        />
      )
    );
    expect(screen.getByTestId('bootstrap-report-count-pass').textContent).toContain('1');
    expect(screen.getByTestId('bootstrap-report-count-warn').textContent).toContain('1');
    expect(screen.getByTestId('bootstrap-report-count-fail').textContent).toContain('1');
    // Overall is FAIL when any finding is fail.
    expect(screen.getByTestId('bootstrap-report-overall').textContent).toBe('FAIL');
  });

  it('finding toggle reveals detail body', () => {
    render(
      wrap(
        <PlanReviewStep
          analysisId="a1"
          plan={PLAN}
          setPlan={() => {}}
          onBack={() => {}}
          onContinue={() => {}}
        />
      )
    );
    expect(screen.queryByTestId('bootstrap-report-detail-f2')).toBeNull();
    fireEvent.click(screen.getByTestId('bootstrap-report-toggle-f2'));
    expect(screen.getByTestId('bootstrap-report-detail-f2')).toBeTruthy();
  });

  it('plan rows render with kind tag and title', () => {
    render(
      wrap(
        <PlanReviewStep
          analysisId="a1"
          plan={PLAN}
          setPlan={() => {}}
          onBack={() => {}}
          onContinue={() => {}}
        />
      )
    );
    expect(screen.getByTestId('bootstrap-plan-row-epic-1')).toBeTruthy();
    expect(screen.getByTestId('bootstrap-plan-row-m1')).toBeTruthy();
  });
});
