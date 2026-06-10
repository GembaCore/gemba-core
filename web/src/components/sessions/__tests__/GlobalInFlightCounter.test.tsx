// GlobalInFlightCounter tests (gm-root.16.2). Asserts the chrome
// pill's sum behavior: it only counts sessions whose effective cap
// admits more than one in-flight bead, hides itself at zero, and
// re-renders as the SSE consumer mutates the parallelism store.

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

  it('sums in_flight across multi-cap sessions', () => {
    const map: SessionParallelismMap = {
      'sess-A': {
        session_id: 'sess-A',
        agent_type: 'claude',
        in_flight: 2,
        max_parallel: 3,
      },
      'sess-B': {
        session_id: 'sess-B',
        agent_type: 'claude',
        in_flight: 1,
        max_parallel: 2,
      },
    };
    resetParallelism(map);
    render(<GlobalInFlightCounter />);
    const el = screen.getByTestId('global-in-flight');
    expect(el.getAttribute('data-total')).toBe('3');
    expect(el.textContent).toContain('3');
  });

  it('excludes single-cap sessions from the sum', () => {
    resetParallelism({
      'sess-A': {
        session_id: 'sess-A',
        agent_type: 'claude',
        in_flight: 2,
        max_parallel: 3,
      },
      'sess-B': {
        session_id: 'sess-B',
        agent_type: 'shell-only',
        in_flight: 1,
        max_parallel: 1,
      },
    });
    render(<GlobalInFlightCounter />);
    const el = screen.getByTestId('global-in-flight');
    expect(el.getAttribute('data-total')).toBe('2');
  });

  it('hides itself when every multi-cap session ticks back to zero', () => {
    resetParallelism({
      'sess-A': {
        session_id: 'sess-A',
        agent_type: 'claude',
        in_flight: 0,
        max_parallel: 3,
      },
    });
    const { container } = render(<GlobalInFlightCounter />);
    expect(container.firstChild).toBeNull();
  });
});
