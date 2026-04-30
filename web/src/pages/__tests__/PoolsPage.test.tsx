// PoolsPage tests (gm-s47n.16). Verifies adaptor-aware branching,
// the validation surface, and the save round-trip.

import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import PoolsPage from '../PoolsPage';
import { CapabilitiesProvider } from '@/capabilities/context';
import type {
  CapabilitiesResponse,
  OrchestrationManifest,
} from '@/capabilities/types';

vi.mock('@/api/poolConfig', () => {
  return {
    getPoolConfig: vi.fn(),
    putPoolConfig: vi.fn(),
    listPersonaFiles: vi.fn(),
    getOrchestrationState: vi.fn(),
  };
});

import * as api from '@/api/poolConfig';

function orchManifest(
  patch: Partial<OrchestrationManifest> = {}
): OrchestrationManifest {
  return {
    adaptor_id: 'native',
    adaptor_version: '0.1.0',
    orchestration_api_version: '1.0.0',
    transport: 'api',
    workspace_kinds: ['worktree'],
    group_modes: ['static'],
    cost_axes: ['tokens'],
    escalation_kinds: ['mcp_elicitation'],
    peek_modes: ['transcript'],
    ...patch,
  };
}

function buildWrapper(initial: CapabilitiesResponse) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <CapabilitiesProvider initial={initial}>
          <MemoryRouter>{children}</MemoryRouter>
        </CapabilitiesProvider>
      </QueryClientProvider>
    );
  };
}

beforeEach(() => {
  vi.mocked(api.getPoolConfig).mockResolvedValue({
    path: '/.gemba/pool.toml',
    body: '',
    parsed: {
      default_size: 0,
      default_persona: '',
      default_floor: 0.5,
      reserved_for_manual: 1,
      routing: {},
      pools: {},
    },
    max_parallel: 4,
    reserved_for_manual: 1,
  });
  vi.mocked(api.listPersonaFiles).mockResolvedValue([
    { id: 'engineer-claude', name: 'Engineer', agent_type: 'claude' },
    { id: 'pm-claude', name: 'PM', agent_type: 'claude' },
  ]);
  vi.mocked(api.getOrchestrationState).mockResolvedValue({
    adaptor_id: 'native',
    scopes: [{ id: 'local', kind: 'local', personas: [] }],
    polecats: [],
  });
});

describe('PoolsPage', () => {
  it('renders the native flow when adaptor_id=native', async () => {
    const Wrapper = buildWrapper({
      work_plane: null,
      orchestration_plane: orchManifest({ adaptor_id: 'native' }),
    });
    render(<PoolsPage />, { wrapper: Wrapper });
    await waitFor(() =>
      expect(screen.getByTestId('pools-page')).toBeTruthy()
    );
    expect(screen.getByText(/Pool Dispatch · Native/i)).toBeTruthy();
  });

  it('shows server defaults section', async () => {
    const Wrapper = buildWrapper({
      work_plane: null,
      orchestration_plane: orchManifest({ adaptor_id: 'native' }),
    });
    render(<PoolsPage />, { wrapper: Wrapper });
    await waitFor(() =>
      expect(screen.getByTestId('pools-page')).toBeTruthy()
    );
    expect(screen.getByTestId('default-persona')).toBeTruthy();
    expect(screen.getByTestId('default-size')).toBeTruthy();
  });

  it('adds and saves a pool card', async () => {
    vi.mocked(api.putPoolConfig).mockResolvedValue({
      path: '/.gemba/pool.toml',
      body: '[pool]\n',
      parsed: {
        default_size: 0,
        default_persona: '',
        default_floor: 0.5,
        reserved_for_manual: 1,
        routing: {},
        pools: {
          local: {
            'engineer-claude': {
              size: 1,
              agent_type: 'claude',
              floor: 0,
              recycle_after_beads: 0,
              idle_ceiling_minutes: 0,
              min_interval_per_session_seconds: 0,
              max_concurrent: 0,
            },
          },
        },
      },
      max_parallel: 4,
      reserved_for_manual: 1,
    });

    const Wrapper = buildWrapper({
      work_plane: null,
      orchestration_plane: orchManifest({ adaptor_id: 'native' }),
    });
    render(<PoolsPage />, { wrapper: Wrapper });
    await waitFor(() => screen.getByTestId('pool-add-card'));

    fireEvent.click(screen.getByTestId('pool-add-card'));
    fireEvent.click(screen.getByTestId('pools-save'));

    await waitFor(() =>
      expect(api.putPoolConfig).toHaveBeenCalled()
    );
    await waitFor(() => screen.getByTestId('pools-restart-banner'));
  });

  it('renders the unsupported placeholder for unknown adaptors', async () => {
    const Wrapper = buildWrapper({
      work_plane: null,
      orchestration_plane: orchManifest({ adaptor_id: 'unknown-future-adaptor' }),
    });
    render(<PoolsPage />, { wrapper: Wrapper });
    await waitFor(() =>
      expect(screen.getByTestId('pools-unsupported-adaptor')).toBeTruthy()
    );
  });
});
