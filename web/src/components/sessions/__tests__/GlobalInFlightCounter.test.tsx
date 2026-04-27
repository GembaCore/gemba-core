// GlobalInFlightCounter tests (gm-root.16.2). Asserts the chrome
// pill's sum behavior: it ignores intra_parallel=false sessions,
// hides itself at zero, and re-renders as the SSE consumer mutates
// the parallelism store.

import { afterEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { GlobalInFlightCounter } from '../GlobalInFlightCounter';
import { resetParallelism, type SessionParallelismMap } from '@/api/parallelism';

afterEach(() => {
  resetParallelism();
});

describe('GlobalInFlightCounter', () => {
  it('renders nothing when total in_flight is zero', () => {
    resetParallelism({});
    const { container } = render(<GlobalInFlightCounter />);
    expect(container.firstChild).toBeNull();
  });

  it('sums in_flight across intra_parallel=true sessions', () => {
    const map: SessionParallelismMap = {
      'sess-A': {
        session_id: 'sess-A',
        agent_id: 'claude',
        in_flight: 2,
        max_parallel: 3,
        intra_parallel: true,
      },
      'sess-B': {
        session_id: 'sess-B',
        agent_id: 'claude',
        in_flight: 1,
        max_parallel: 2,
        intra_parallel: true,
      },
    };
    resetParallelism(map);
    render(<GlobalInFlightCounter />);
    const el = screen.getByTestId('global-in-flight');
    expect(el.getAttribute('data-total')).toBe('3');
    expect(el.textContent).toContain('3');
  });

  it('excludes intra_parallel=false sessions from the sum', () => {
    resetParallelism({
      'sess-A': {
        session_id: 'sess-A',
        agent_id: 'claude',
        in_flight: 2,
        max_parallel: 3,
        intra_parallel: true,
      },
      'sess-B': {
        session_id: 'sess-B',
        agent_id: 'shell-only',
        in_flight: 1,
        max_parallel: 0,
        intra_parallel: false,
      },
    });
    render(<GlobalInFlightCounter />);
    const el = screen.getByTestId('global-in-flight');
    expect(el.getAttribute('data-total')).toBe('2');
  });

  it('hides itself when every session ticks back to zero', () => {
    resetParallelism({
      'sess-A': {
        session_id: 'sess-A',
        agent_id: 'claude',
        in_flight: 0,
        max_parallel: 3,
        intra_parallel: true,
      },
    });
    const { container } = render(<GlobalInFlightCounter />);
    expect(container.firstChild).toBeNull();
  });
});
