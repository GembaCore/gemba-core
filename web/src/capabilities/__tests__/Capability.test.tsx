import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Capability } from '../Capability';
import { CapabilitiesProvider } from '../context';
import type { CapabilitiesResponse, WorkPlaneManifest } from '../types';

// Deliberate: the DoD on gm-e11.4 calls for a snapshot that would catch a
// backend-vocabulary leak. `renderToStaticMarkup` gives us a stable string
// we can lock with expect(markup).toMatchInlineSnapshot. Any accidental
// adaptor-native status token (e.g. "in_progress" when state_map only
// declares "todo", "done") would surface here before it reaches prod.

function client(): QueryClient {
  // retry:false + staleTime:Infinity — tests should not poll.
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
}

function render(ui: ReactNode, initial: CapabilitiesResponse) {
  return renderToStaticMarkup(
    <QueryClientProvider client={client()}>
      <CapabilitiesProvider initial={initial}>{ui}</CapabilitiesProvider>
    </QueryClientProvider>,
  );
}

const BEADS_WORK: WorkPlaneManifest = {
  adaptor_name: 'beads',
  adaptor_version: '0.1.0',
  protocol_version: '1',
  transport: 'api',
  state_map: {
    open: 'unstarted',
    in_progress: 'started',
    closed: 'completed',
  },
  sprint_native: false,
  token_budget_enforced: false,
  evidence_synthesis_required: true,
  field_extensions: [{ name: 'story_points', type: 'number' }],
  edge_extensions: [{ name: 'duplicates', directed: true }],
};

const JIRA_WORK: WorkPlaneManifest = {
  adaptor_name: 'jira',
  adaptor_version: '0.1.0',
  protocol_version: '1',
  transport: 'api',
  state_map: { todo: 'unstarted', in_progress: 'started', done: 'completed' },
  sprint_native: true,
  token_budget_enforced: false,
  evidence_synthesis_required: false,
};

describe('<Capability>', () => {
  it('hides children when the manifest opts out of sprint_native', () => {
    const markup = render(
      <Capability has="sprint_native">
        <span data-testid="sprint-lane">sprint</span>
      </Capability>,
      { work_plane: BEADS_WORK, orchestration_plane: null },
    );
    expect(markup).toBe('');
  });

  it('renders children when the manifest declares sprint_native', () => {
    const markup = render(
      <Capability has="sprint_native">
        <span>sprint</span>
      </Capability>,
      { work_plane: JIRA_WORK, orchestration_plane: null },
    );
    expect(markup).toBe('<span>sprint</span>');
  });

  it('renders fallback on a miss', () => {
    const markup = render(
      <Capability has="sprint_native" fallback={<em>no-sprint</em>}>
        <span>sprint</span>
      </Capability>,
      { work_plane: BEADS_WORK, orchestration_plane: null },
    );
    expect(markup).toBe('<em>no-sprint</em>');
  });

  it('gates on field_extensions presence', () => {
    const hit = render(
      <Capability hasField="story_points">
        <span>pts</span>
      </Capability>,
      { work_plane: BEADS_WORK, orchestration_plane: null },
    );
    const miss = render(
      <Capability hasField="story_points">
        <span>pts</span>
      </Capability>,
      { work_plane: JIRA_WORK, orchestration_plane: null },
    );
    expect(hit).toBe('<span>pts</span>');
    expect(miss).toBe('');
  });

  it('gates on edge_extensions presence', () => {
    const hit = render(
      <Capability hasEdge="duplicates">
        <span>dup</span>
      </Capability>,
      { work_plane: BEADS_WORK, orchestration_plane: null },
    );
    expect(hit).toBe('<span>dup</span>');
  });

  it('renders fallback when no manifest is available', () => {
    const markup = render(
      <Capability has="sprint_native" fallback={<em>offline</em>}>
        <span>sprint</span>
      </Capability>,
      { work_plane: null, orchestration_plane: null },
    );
    expect(markup).toBe('<em>offline</em>');
  });

  it('ANDs multiple predicates', () => {
    const markup = render(
      <Capability has="evidence_synthesis_required" hasField="story_points">
        <span>both</span>
      </Capability>,
      { work_plane: BEADS_WORK, orchestration_plane: null },
    );
    expect(markup).toBe('<span>both</span>');
  });

  it('snapshot: mixed capability gates against beads manifest', () => {
    // This is the DoD snapshot (gm-e11.4). It locks what the UI produces
    // for a representative, capability-heavy tree against the beads
    // manifest. If a future change leaks a backend-native token like
    // "in_progress" into the rendered markup, this snapshot diverges.
    const tree = (
      <ul>
        <li>
          <Capability has="sprint_native" fallback={<span>no-sprint</span>}>
            <span>sprint</span>
          </Capability>
        </li>
        <li>
          <Capability has="token_budget_enforced" fallback={<span>no-budget</span>}>
            <span>budget</span>
          </Capability>
        </li>
        <li>
          <Capability hasField="story_points" fallback={<span>no-pts</span>}>
            <span>pts</span>
          </Capability>
        </li>
        <li>
          <Capability hasEdge="blocks" fallback={<span>no-blocks</span>}>
            <span>blocks</span>
          </Capability>
        </li>
      </ul>
    );
    const markup = render(tree, {
      work_plane: BEADS_WORK,
      orchestration_plane: null,
    });
    expect(markup).toMatchInlineSnapshot(
      `"<ul><li><span>no-sprint</span></li><li><span>no-budget</span></li><li><span>pts</span></li><li><span>no-blocks</span></li></ul>"`,
    );
  });
});
