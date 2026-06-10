// ParallelismPill state tests (gm-root.16.2). Covers the four
// states the DoD enumerates: zero in-flight, partial, at-cap,
// suppressed (intra_parallel=false). The cap-tone is asserted
// via data-state so the test stays insensitive to Tailwind class
// reshuffles.

import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ParallelismPill } from '../ParallelismPill';

describe('ParallelismPill', () => {
  it('renders zero state when in_flight=0 and intra_parallel=true', () => {
    render(<ParallelismPill inFlight={0} maxParallel={3} intraParallel={true} />);
    const el = screen.getByTestId('parallelism-pill');
    expect(el.textContent).toBe('0/3');
    expect(el.getAttribute('data-state')).toBe('zero');
  });

  it('renders partial state when 0 < in_flight < max', () => {
    render(<ParallelismPill inFlight={2} maxParallel={3} intraParallel={true} />);
    const el = screen.getByTestId('parallelism-pill');
    expect(el.textContent).toBe('2/3');
    expect(el.getAttribute('data-state')).toBe('partial');
  });

  it('renders at-cap state when in_flight equals max', () => {
    render(<ParallelismPill inFlight={3} maxParallel={3} intraParallel={true} />);
    const el = screen.getByTestId('parallelism-pill');
    expect(el.textContent).toBe('3/3');
    expect(el.getAttribute('data-state')).toBe('at-cap');
  });

  it('renders nothing when intra_parallel=false', () => {
    const { container } = render(
      <ParallelismPill inFlight={1} maxParallel={1} intraParallel={false} />
    );
    expect(container.firstChild).toBeNull();
  });

  it('still treats in_flight > max as at-cap (no overflow rendering)', () => {
    // Defensive: dispatcher should never let in_flight exceed max,
    // but if a stale event arrives we don't want the pill flashing
    // a misleading "4/3 partial" tone.
    render(<ParallelismPill inFlight={4} maxParallel={3} intraParallel={true} />);
    const el = screen.getByTestId('parallelism-pill');
    expect(el.getAttribute('data-state')).toBe('at-cap');
  });

  it('honors a custom testid prefix', () => {
    render(
      <ParallelismPill
        inFlight={1}
        maxParallel={2}
        intraParallel={true}
        testid="sess-1-pill"
      />
    );
    expect(screen.getByTestId('sess-1-pill').textContent).toBe('1/2');
  });
});
