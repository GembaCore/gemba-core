// CapabilitiesPage tests (gm-e12.14, restructured by gm-bi5). Pins the
// rendering invariants for the read-only adaptor browser:
//
//   * top-level surface sections (Organization vs Execution) always
//     render, with the conceptual subtitle copy from the bead
//   * adaptors are placed under the section their `adaptor_name`
//     classifies to (see lib/surfaceKind.ts)
//   * each adaptor card carries a surface badge
//   * health pill reads the cached /api/adaptors data the SSE pump
//     feeds — no fetch of its own
//   * state map / extension tables / conformance flags all render
//     for the v1 bd manifest
//
// We don't drive a live network; the manifest is seeded into the
// CapabilitiesProvider via `initial`, and the adaptor health is
// pre-populated into the QueryClient cache on the ['adaptors'] key.

import { describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import CapabilitiesPage from '../CapabilitiesPage';
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
    state_map: {
      open: 'unstarted',
      in_progress: 'started',
      closed: 'completed',
    },
    edge_extensions: [
      {
        name: 'beads:discovered_from',
        directed: true,
        inverse: 'beads:discovered_by',
        description: 'Target bead was discovered while working on the source bead.',
      },
    ],
    field_extensions: [
      { name: 'beads:issue_type', type: 'string', description: 'Beads issue type token.' },
    ],
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
    workspace_kinds: ['worktree', 'container'],
    default_workspace_kind: 'worktree',
    group_modes: ['static', 'pool'],
    cost_axes: ['tokens', 'wallclock'],
    escalation_kinds: ['mcp_elicitation', 'permission_prompt'],
    peek_modes: ['transcript'],
    ...patch,
  };
}

interface WrapperOpts {
  initial: CapabilitiesResponse;
  adaptors?: { name: string; plane: 'work' | 'orchestration'; healthy: boolean }[];
}

function buildWrapper(opts: WrapperOpts) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  if (opts.adaptors) {
    qc.setQueryData(['adaptors'], { adaptors: opts.adaptors });
  }
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

describe('CapabilitiesPage', () => {
  it('renders the work-plane card with adaptor name + transport', () => {
    const Wrapper = buildWrapper({
      initial: { work_plane: workManifest(), orchestration_plane: null },
      adaptors: [{ name: 'beads', plane: 'work', healthy: true }],
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    const card = screen.getByTestId('work-plane-card');
    expect(card).toBeTruthy();
    const txt = card.textContent ?? '';
    expect(txt).toContain('beads');
    expect(txt).toContain('protocol 1.0.0');
    expect(txt).toContain('transport api');
  });

  it('renders the state map rows', () => {
    const Wrapper = buildWrapper({
      initial: { work_plane: workManifest(), orchestration_plane: null },
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    const sm = screen.getByTestId('state-map').textContent ?? '';
    expect(sm).toContain('open');
    expect(sm).toContain('unstarted');
    expect(sm).toContain('in_progress');
    expect(sm).toContain('started');
    expect(sm).toContain('closed');
    expect(sm).toContain('completed');
  });

  it('renders edge + field extension tables when present', () => {
    const Wrapper = buildWrapper({
      initial: { work_plane: workManifest(), orchestration_plane: null },
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    expect(screen.getByTestId('edge-extensions').textContent ?? '').toContain('beads:discovered_from');
    expect(screen.getByTestId('field-extensions').textContent ?? '').toContain('beads:issue_type');
  });

  it('omits an extension table when the manifest has none of that kind', () => {
    const Wrapper = buildWrapper({
      initial: {
        work_plane: workManifest({ edge_extensions: [], field_extensions: [] }),
        orchestration_plane: null,
      },
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    expect(screen.queryByTestId('edge-extensions')).toBeNull();
    expect(screen.queryByTestId('field-extensions')).toBeNull();
  });

  it('shows healthy / degraded pill from the cached adaptor status', () => {
    const Wrapper = buildWrapper({
      initial: { work_plane: workManifest(), orchestration_plane: null },
      adaptors: [{ name: 'beads', plane: 'work', healthy: false }],
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    expect(screen.getByTestId('adaptor-health').textContent?.toLowerCase()).toContain('degraded');
  });

  it('shows "status unknown" when the adaptors cache is empty', () => {
    const Wrapper = buildWrapper({
      initial: { work_plane: workManifest(), orchestration_plane: null },
      // no adaptors seeded
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    expect(screen.getByText(/status unknown/i)).toBeTruthy();
  });

  it('renders read-only badge when the manifest declares it', () => {
    const Wrapper = buildWrapper({
      initial: {
        work_plane: workManifest({ read_only: true }),
        orchestration_plane: null,
      },
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    // Card-scoped lookup so the page subtitle ("Read-only diagnostic
    // view…") doesn't double-match.
    const card = screen.getByTestId('work-plane-card').textContent ?? '';
    expect(card.toLowerCase()).toContain('read-only');
  });

  it('renders both surface sections with their conceptual subtitles', () => {
    const Wrapper = buildWrapper({
      initial: { work_plane: workManifest(), orchestration_plane: null },
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    const org = screen.getByTestId('surface-section-organization');
    const exec = screen.getByTestId('surface-section-execution');
    expect(org.textContent ?? '').toContain(
      'Organization surface: what your team already does.'
    );
    expect(exec.textContent ?? '').toContain(
      'Execution surface: what happens in this workspace.'
    );
  });

  it('places the beads work-plane card under the execution surface', () => {
    const Wrapper = buildWrapper({
      initial: {
        work_plane: workManifest(),
        orchestration_plane: orchManifest(),
      },
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    const exec = screen.getByTestId('surface-section-execution');
    // beads (work plane) and native (orchestration) are both
    // execution-surface adaptors per the classifier.
    expect(within(exec).getByTestId('work-plane-card')).toBeTruthy();
    expect(within(exec).getByTestId('orchestration-plane-card')).toBeTruthy();
    // Organization section is empty.
    expect(screen.getByTestId('surface-empty-organization')).toBeTruthy();
  });

  it('places a jira work-plane card under the organization surface', () => {
    const Wrapper = buildWrapper({
      initial: {
        work_plane: workManifest({ adaptor_name: 'jira' }),
        orchestration_plane: null,
      },
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    const org = screen.getByTestId('surface-section-organization');
    expect(within(org).getByTestId('work-plane-card').textContent ?? '').toContain('jira');
    // Execution section is empty.
    expect(screen.getByTestId('surface-empty-execution')).toBeTruthy();
  });

  it('renders the surface badge on each adaptor card', () => {
    const Wrapper = buildWrapper({
      initial: {
        work_plane: workManifest({ adaptor_name: 'jira' }),
        orchestration_plane: orchManifest(),
      },
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    const orgCard = within(screen.getByTestId('surface-section-organization')).getByTestId(
      'work-plane-card'
    );
    expect(within(orgCard).getByTestId('surface-badge-organization').textContent).toMatch(/org/i);

    const execCard = within(screen.getByTestId('surface-section-execution')).getByTestId(
      'orchestration-plane-card'
    );
    expect(within(execCard).getByTestId('surface-badge-execution').textContent).toMatch(
      /workspace/i
    );
  });

  it('shows an empty state when neither plane is bound', () => {
    const Wrapper = buildWrapper({
      initial: { work_plane: null, orchestration_plane: null },
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    expect(screen.getByTestId('capabilities-empty')).toBeTruthy();
  });

  it('renders conformance flags grid', () => {
    const Wrapper = buildWrapper({
      initial: { work_plane: workManifest({ sprint_native: true }), orchestration_plane: null },
    });
    render(<CapabilitiesPage />, { wrapper: Wrapper });
    expect(screen.getByTestId('conformance-flags')).toBeTruthy();
    // FLAG_NAMES is the source of truth for which flags appear; we
    // just spot-check that the grid is non-empty and that the
    // sprint_native truthiness made it through.
    expect(screen.getByTestId('conformance-flags').textContent ?? '').toContain('has_sprints');
  });
});
