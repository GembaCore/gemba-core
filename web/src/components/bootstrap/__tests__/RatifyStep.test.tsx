// RatifyStep tests (gm-uipx.7).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { RatifyStep } from '../RatifyStep';
import type { BootstrapPlan } from '@/api/bootstrap';

vi.mock('@/api/bootstrap', async () => {
  const actual = await vi.importActual<typeof import('@/api/bootstrap')>('@/api/bootstrap');
  return {
    ...actual,
    commitBootstrap: vi.fn(),
  };
});
import { commitBootstrap } from '@/api/bootstrap';

const PLAN: BootstrapPlan = {
  analysis_id: 'a1',
  source: 'fresh',
  plan: [{ id: 'e1', kind: 'epic', title: 'Onboard' }],
  findings: [
    { id: 'f1', level: 'pass', title: 'OK' },
    { id: 'f2', level: 'warn', title: 'Watch' },
  ],
};

describe('RatifyStep', () => {
  beforeEach(() => {
    (commitBootstrap as ReturnType<typeof vi.fn>).mockReset();
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders summary including plan-item count and finding counts', () => {
    render(
      <RatifyStep
        analysisId="a1"
        plan={PLAN}
        onBack={() => {}}
        onCommitted={() => {}}
      />
    );
    expect(screen.getByTestId('bootstrap-ratify-summary')).toBeTruthy();
    expect(screen.getByTestId('bootstrap-ratify-plan-count').textContent).toContain('1');
    expect(screen.getByTestId('bootstrap-ratify-findings').textContent).toContain('PASS 1');
    expect(screen.getByTestId('bootstrap-ratify-findings').textContent).toContain('WARN 1');
  });

  it('commit click calls commitBootstrap with a stable nonce, then onCommitted', async () => {
    (commitBootstrap as ReturnType<typeof vi.fn>).mockResolvedValue({
      project_path: '.gemba/project.toml',
      board_url: '/board',
    });
    const onCommitted = vi.fn();
    render(
      <RatifyStep
        analysisId="a1"
        plan={PLAN}
        onBack={() => {}}
        onCommitted={onCommitted}
      />
    );
    fireEvent.click(screen.getByTestId('bootstrap-ratify-commit'));
    await waitFor(() => expect(onCommitted).toHaveBeenCalledWith('/board'));
    expect(commitBootstrap).toHaveBeenCalledTimes(1);
    const [analysisId, opts] = (commitBootstrap as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(analysisId).toBe('a1');
    expect(typeof opts.nonce).toBe('string');
    expect(opts.nonce.length).toBeGreaterThan(0);
  });

  it('warns when there are FAIL findings', () => {
    const planWithFail: BootstrapPlan = {
      ...PLAN,
      findings: [{ id: 'f1', level: 'fail', title: 'Broken' }],
    };
    render(
      <RatifyStep
        analysisId="a1"
        plan={planWithFail}
        onBack={() => {}}
        onCommitted={() => {}}
      />
    );
    expect(screen.getByTestId('bootstrap-ratify-fail-warning')).toBeTruthy();
  });
});
