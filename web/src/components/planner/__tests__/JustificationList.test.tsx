import { describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { JustificationList } from '../JustificationList';

describe('JustificationList', () => {
  it('renders one row per line', () => {
    render(
      <JustificationList
        lines={['score_pre_gate: 0.45 = ...', 'demoted ×0.40: out-of-intent', 'score: 0.18']}
      />,
    );
    expect(screen.getByTestId('justification-line-0').textContent).toMatch(/score_pre_gate/);
    expect(screen.getByTestId('justification-line-1').textContent).toMatch(/out-of-intent/);
    expect(screen.getByTestId('justification-line-2').textContent).toMatch(/^score:/);
  });

  it('shows the reason badge when rejected', () => {
    render(
      <JustificationList
        outcome="rejected"
        reason="dispatch_status"
        lines={['rejected: dispatch_status=awaiting-design']}
      />,
    );
    const badge = screen.getByTestId('justification-reason');
    expect(badge.textContent).toBe('dispatch_status');
    expect(screen.getByTestId('justification-list').getAttribute('data-outcome')).toBe('rejected');
  });

  it('omits the reason badge when none supplied', () => {
    render(<JustificationList lines={['score: 0.42']} />);
    expect(screen.queryByTestId('justification-reason')).toBeNull();
  });

  it('renders an empty-state hint when lines is empty', () => {
    render(<JustificationList lines={[]} />);
    expect(screen.getByText('(no justification)')).toBeTruthy();
  });

  it('uses dispatchable as the default outcome', () => {
    render(<JustificationList lines={['score: 0.42']} />);
    expect(screen.getByTestId('justification-list').getAttribute('data-outcome')).toBe(
      'dispatchable',
    );
  });

  it('respects testid override', () => {
    render(<JustificationList testid="custom-just" lines={['x']} />);
    expect(screen.getByTestId('custom-just')).toBeTruthy();
  });

  it('supports a full density mode', () => {
    render(<JustificationList density="full" lines={['line one']} testid="dense-list" />);
    const root = screen.getByTestId('dense-list');
    // Both modes ship; full pulls a different size class. Verify
    // the line is rendered either way.
    expect(within(root).getByTestId('justification-line-0').textContent).toBe('line one');
  });
});
