// SettingsPage tests (gm-bi5). v1 surface for the
// organization-vs-execution split. Pins:
//
//   * the Organization | Execution tab pair
//   * each tab panel shows the conceptual subtitle from the bead
//   * adaptors are filtered by classifyAdaptor() into the active tab
//   * empty-state copy when no adaptor classifies to the active tab

import { describe, expect, it } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import SettingsPage from '../SettingsPage';
import { CapabilitiesProvider } from '@/capabilities/context';
import type {
  CapabilitiesResponse,
  WorkPlaneManifest,
  OrchestrationManifest,
} from '@/capabilities/types';

function workManifest(
  patch: Partial<WorkPlaneManifest> = {}
): WorkPlaneManifest {
  return {
    adaptor_name: 'beads',
    adaptor_version: '0.1.0',
    protocol_version: '1.0.0',
    transport: 'api',
    state_map: { open: 'unstarted', closed: 'completed' },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
    ...patch,
  };
}

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

interface WrapperOpts {
  initial: CapabilitiesResponse;
}

function buildWrapper(opts: WrapperOpts) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <CapabilitiesProvider initial={opts.initial}>
          <MemoryRouter>{children}</MemoryRouter>
        </CapabilitiesProvider>
      </QueryClientProvider>
    );
  };
}

describe('SettingsPage', () => {
  it('renders Organization and Execution tabs', () => {
    const Wrapper = buildWrapper({
      initial: { work_plane: workManifest(), orchestration_plane: null },
    });
    render(<SettingsPage />, { wrapper: Wrapper });
    expect(screen.getByTestId('settings-tab-organization')).toBeTruthy();
    expect(screen.getByTestId('settings-tab-execution')).toBeTruthy();
  });

  it('shows the organization subtitle on the organization panel', () => {
    const Wrapper = buildWrapper({
      initial: {
        work_plane: workManifest({ adaptor_name: 'jira' }),
        orchestration_plane: null,
      },
    });
    render(<SettingsPage />, { wrapper: Wrapper });
    const panel = screen.getByTestId('settings-panel-organization');
    expect(panel.textContent ?? '').toContain(
      'Organization surface: what your team already does.'
    );
    expect(within(panel).getByTestId('settings-adaptor-jira')).toBeTruthy();
  });

  it('switches to the execution panel and shows execution adaptors', () => {
    const Wrapper = buildWrapper({
      initial: {
        work_plane: workManifest(), // beads → execution
        orchestration_plane: orchManifest(), // native → execution (default fallback)
      },
    });
    render(<SettingsPage />, { wrapper: Wrapper });
    fireEvent.click(screen.getByTestId('settings-tab-execution'));
    const panel = screen.getByTestId('settings-panel-execution');
    expect(panel.textContent ?? '').toContain(
      'Execution surface: what happens in this workspace.'
    );
    expect(within(panel).getByTestId('settings-adaptor-beads')).toBeTruthy();
    expect(within(panel).getByTestId('settings-adaptor-native')).toBeTruthy();
  });

  it('renders the empty state when no adaptors classify to the active tab', () => {
    const Wrapper = buildWrapper({
      initial: { work_plane: workManifest(), orchestration_plane: null },
    });
    // Default tab = organization; beads is execution-classified, so
    // the organization panel is empty.
    render(<SettingsPage />, { wrapper: Wrapper });
    expect(screen.getByTestId('settings-empty-organization')).toBeTruthy();
  });
});
