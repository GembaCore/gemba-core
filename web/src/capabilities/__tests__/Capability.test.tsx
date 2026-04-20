import { afterEach, describe, expect, it } from 'vitest';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import {
  Capability,
  CapabilitiesTestProvider,
  type Manifests,
  type OrchestrationCapabilityManifest,
  type WorkCapabilityManifest,
} from '@/capabilities';

// The JSX gate is the primary capability-negotiation surface
// (gm-e11.4). These tests pin the contract: children render only when
// the active manifest declares the capability, and a missing or null
// manifest hides the gate by default. A bug that inverts this
// behaviour is a production incident — an operator seeing controls
// they can't exercise means the backend-vocabulary firewall leaked.

function workManifest(overrides: Partial<WorkCapabilityManifest> = {}): WorkCapabilityManifest {
  return {
    adaptor_name: 'beads',
    adaptor_version: '0.1.0',
    protocol_version: '1',
    transport: 'jsonl',
    state_map: { open: 'unstarted', closed: 'completed' },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
    ...overrides,
  };
}

function orchManifest(
  overrides: Partial<OrchestrationCapabilityManifest> = {}
): OrchestrationCapabilityManifest {
  return {
    adaptor_id: 'gastown',
    adaptor_version: '0.1.0',
    orchestration_api_version: '1',
    transport: 'api',
    workspace_kinds: ['worktree'],
    group_modes: ['static'],
    cost_axes: ['tokens', 'wallclock'],
    escalation_kinds: ['hitl_approval'],
    peek_modes: ['transcript'],
    ...overrides,
  };
}

// Tiny render harness — we deliberately don't pull in
// @testing-library here to keep the web test deps minimal. This
// mirrors the vitest+jsdom setup in vite.config.ts and is enough for
// the capability-gate surface, which is rendered synchronously.
let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(ui: React.ReactElement) {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(ui);
  });
  return container;
}

afterEach(() => {
  if (root) {
    act(() => {
      root!.unmount();
    });
    root = null;
  }
  if (container) {
    container.remove();
    container = null;
  }
});

function renderWith(manifests: Manifests, ui: React.ReactElement) {
  return mount(<CapabilitiesTestProvider manifests={manifests}>{ui}</CapabilitiesTestProvider>);
}

function byTestId(el: HTMLElement, id: string): HTMLElement | null {
  return el.querySelector(`[data-testid="${id}"]`);
}

describe('Capability JSX gate', () => {
  it('renders children when the capability is declared true', () => {
    const el = renderWith(
      { work: workManifest({ sprint_native: true }), orchestration: null },
      <Capability has="sprint_native">
        <div data-testid="sprint-lane">sprint lane</div>
      </Capability>
    );
    expect(byTestId(el, 'sprint-lane')).not.toBeNull();
  });

  it('hides children when the capability is declared false', () => {
    const el = renderWith(
      { work: workManifest({ sprint_native: false }), orchestration: null },
      <Capability has="sprint_native">
        <div data-testid="sprint-lane">sprint lane</div>
      </Capability>
    );
    expect(byTestId(el, 'sprint-lane')).toBeNull();
  });

  it('hides children when no work manifest is bound (safe default)', () => {
    const el = renderWith(
      { work: null, orchestration: null },
      <Capability has="token_budget_enforced">
        <div data-testid="budget">budget</div>
      </Capability>
    );
    expect(byTestId(el, 'budget')).toBeNull();
  });

  it('renders fallback when capability is off', () => {
    const el = renderWith(
      { work: workManifest({ sprint_native: false }), orchestration: null },
      <Capability has="sprint_native" fallback={<div data-testid="no-sprint">no sprints</div>}>
        <div>sprint lane</div>
      </Capability>
    );
    expect(byTestId(el, 'no-sprint')).not.toBeNull();
  });

  it('inverts via `missing` to show "not-declared" copy', () => {
    const el = renderWith(
      { work: workManifest({ token_budget_enforced: false }), orchestration: null },
      <Capability missing="token_budget_enforced">
        <div data-testid="hint">enable budgets in your adaptor</div>
      </Capability>
    );
    expect(byTestId(el, 'hint')).not.toBeNull();
  });

  it('gates on orchestration escalation kinds', () => {
    const el = renderWith(
      {
        work: null,
        orchestration: orchManifest({ escalation_kinds: ['hitl_approval'] }),
      },
      <>
        <Capability escalation="hitl_approval">
          <div data-testid="hitl">HITL inbox</div>
        </Capability>
        <Capability escalation="mcp_elicitation">
          <div data-testid="mcp">MCP inbox</div>
        </Capability>
      </>
    );
    expect(byTestId(el, 'hitl')).not.toBeNull();
    expect(byTestId(el, 'mcp')).toBeNull();
  });

  it('gates on orchestration peek modes', () => {
    const el = renderWith(
      { work: null, orchestration: orchManifest({ peek_modes: ['transcript'] }) },
      <>
        <Capability peek="transcript">
          <div data-testid="tr">transcript viewer</div>
        </Capability>
        <Capability peek="screenshot">
          <div data-testid="sc">screenshot viewer</div>
        </Capability>
      </>
    );
    expect(byTestId(el, 'tr')).not.toBeNull();
    expect(byTestId(el, 'sc')).toBeNull();
  });

  it('gates on orchestration workspace kinds', () => {
    const el = renderWith(
      { work: null, orchestration: orchManifest({ workspace_kinds: ['container'] }) },
      <>
        <Capability workspace="container">
          <div data-testid="c">container-only</div>
        </Capability>
        <Capability workspace="worktree">
          <div data-testid="w">worktree-only</div>
        </Capability>
      </>
    );
    expect(byTestId(el, 'c')).not.toBeNull();
    expect(byTestId(el, 'w')).toBeNull();
  });
});
