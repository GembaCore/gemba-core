// gm-root.26 item 3 — pool editor empty-state helper.
//
// When listPersonaFiles returns an empty list, PoolsPage renders a
// "No personas configured" Section with copy-paste guidance. Once
// any persona exists the section is suppressed entirely.

import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import PoolsPage from '../PoolsPage';
import { CapabilitiesProvider } from '@/capabilities/context';
import type {
  CapabilitiesResponse,
  OrchestrationManifest,
} from '@/capabilities/types';

vi.mock('@/api/poolConfig', () => ({
  getPoolConfig: vi.fn(),
  putPoolConfig: vi.fn(),
  listPersonaFiles: vi.fn(),
  getOrchestrationState: vi.fn(),
  getSchedulerConfig: vi.fn(),
}));

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
  vi.mocked(api.getOrchestrationState).mockResolvedValue({
    adaptor_id: 'native',
    scopes: [{ id: 'local', kind: 'local', personas: [] }],
    polecats: [],
  });
  vi.mocked(api.getSchedulerConfig).mockResolvedValue({ max_polecats: 0 });
});

describe('PoolsPage — no-personas helper (gm-root.26 item 3)', () => {
  it('renders the helper section when listPersonaFiles returns []', async () => {
    vi.mocked(api.listPersonaFiles).mockResolvedValue([]);
    const Wrapper = buildWrapper({
      work_plane: null,
      orchestration_plane: orchManifest({ adaptor_id: 'native' }),
    });
    render(<PoolsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('pools-page')).toBeTruthy());
    const helper = await waitFor(() =>
      screen.getByTestId('pools-no-personas-helper')
    );
    expect(helper).toBeTruthy();
    // Snippet content is on screen for copy-paste fallback when the
    // clipboard API is unavailable.
    const snippet = screen.getByTestId('pools-no-personas-snippet');
    expect(snippet.textContent).toContain('id = "engineer"');
    expect(snippet.textContent).toContain('agent_type = "claude"');
    expect(screen.getByTestId('pools-no-personas-copy')).toBeTruthy();
  });

  it('suppresses the helper when at least one persona exists', async () => {
    vi.mocked(api.listPersonaFiles).mockResolvedValue([
      { id: 'engineer', name: 'Engineer', agent_type: 'claude' },
    ]);
    const Wrapper = buildWrapper({
      work_plane: null,
      orchestration_plane: orchManifest({ adaptor_id: 'native' }),
    });
    render(<PoolsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('pools-page')).toBeTruthy());
    // Wait for the persona-dependent default-persona dropdown to render
    // (a proxy for personas[] hydration), then assert helper is absent.
    await waitFor(() =>
      expect(screen.getByTestId('default-persona')).toBeTruthy()
    );
    expect(screen.queryByTestId('pools-no-personas-helper')).toBeNull();
  });
});
