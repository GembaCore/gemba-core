import { afterEach, describe, expect, it } from 'vitest';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import {
  CapabilitiesTestProvider,
  FieldExtensions,
  clearFieldRegistry,
  registerFieldRenderer,
  type Manifests,
} from '@/capabilities';

// Adaptor-namespaced extensions must render under the adaptor's own
// widget when one is registered, and fall through to the safe default
// otherwise (gm-root DD-4). An unregistered extension MUST NOT throw
// — the manifest is advisory, not contractual about every field being
// populated per record.

function manifestsWith(
  adaptorName: string,
  fields: Array<{ name: string; type: string }>
): Manifests {
  return {
    work: {
      adaptor_name: adaptorName,
      adaptor_version: '0.1',
      protocol_version: '1',
      transport: 'jsonl',
      state_map: { open: 'unstarted' },
      field_extensions: fields,
      sprint_native: false,
      token_budget_enforced: false,
      evidence_synthesis_required: false,
    },
    orchestration: null,
  };
}

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(ui: React.ReactElement): HTMLElement {
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
  clearFieldRegistry();
});

function byTestId(el: HTMLElement, id: string): HTMLElement | null {
  return el.querySelector(`[data-testid="${id}"]`);
}

describe('FieldExtensions', () => {
  it('renders a registered adaptor widget for a declared field', () => {
    registerFieldRenderer('beads', 'priority', ({ value }) => (
      <span data-testid="beads-priority">P{String(value)}</span>
    ));

    const el = mount(
      <CapabilitiesTestProvider
        manifests={manifestsWith('beads', [{ name: 'priority', type: 'number' }])}
      >
        <FieldExtensions custom={{ priority: 2 }} />
      </CapabilitiesTestProvider>
    );

    expect(byTestId(el, 'beads-priority')?.textContent).toBe('P2');
  });

  it('falls back to the safe default when no adaptor widget is registered', () => {
    const el = mount(
      <CapabilitiesTestProvider
        manifests={manifestsWith('beads', [{ name: 'effort', type: 'string' }])}
      >
        <FieldExtensions custom={{ effort: 'S' }} />
      </CapabilitiesTestProvider>
    );
    const node = byTestId(el, 'field-ext-effort');
    expect(node?.textContent).toContain('effort');
    expect(node?.textContent).toContain('S');
  });

  it('does not render anything when the manifest declares no extensions', () => {
    const el = mount(
      <CapabilitiesTestProvider manifests={manifestsWith('beads', [])}>
        <FieldExtensions custom={{ anything: 'x' }} />
      </CapabilitiesTestProvider>
    );
    expect(byTestId(el, 'field-extensions')).toBeNull();
  });

  it('does not render when the work manifest is missing', () => {
    const el = mount(
      <CapabilitiesTestProvider manifests={{ work: null, orchestration: null }}>
        <FieldExtensions custom={{ priority: 2 }} />
      </CapabilitiesTestProvider>
    );
    expect(byTestId(el, 'field-extensions')).toBeNull();
  });

  it('drops fields whose custom value is nullish (safe default)', () => {
    const el = mount(
      <CapabilitiesTestProvider
        manifests={manifestsWith('beads', [
          { name: 'a', type: 'string' },
          { name: 'b', type: 'string' },
        ])}
      >
        <FieldExtensions custom={{ a: 'only-a' }} />
      </CapabilitiesTestProvider>
    );
    expect(byTestId(el, 'field-ext-a')).not.toBeNull();
    expect(byTestId(el, 'field-ext-b')).toBeNull();
  });

  it('namespaces by adaptor_name to avoid cross-adaptor collisions', () => {
    registerFieldRenderer('beads', 'owner', () => <span data-testid="b-owner">beads</span>);
    registerFieldRenderer('jira', 'owner', () => <span data-testid="j-owner">jira</span>);

    const el = mount(
      <CapabilitiesTestProvider
        manifests={manifestsWith('jira', [{ name: 'owner', type: 'string' }])}
      >
        <FieldExtensions custom={{ owner: 'me' }} />
      </CapabilitiesTestProvider>
    );

    expect(byTestId(el, 'b-owner')).toBeNull();
    expect(byTestId(el, 'j-owner')).not.toBeNull();
  });
});
