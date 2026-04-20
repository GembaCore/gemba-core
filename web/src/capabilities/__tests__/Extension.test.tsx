import { afterEach, describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { CapabilitiesProvider } from '../context';
import { FieldExtensionSlot, EdgeExtensionSlot } from '../Extension';
import {
  __resetExtensionRegistryForTests,
  registerFieldExtension,
  registerEdgeExtension,
  type FieldExtensionProps,
  type EdgeExtensionProps,
} from '../extension-registry';
import type { CapabilitiesResponse, WorkPlaneManifest } from '../types';

function client(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function render(ui: ReactNode, initial: CapabilitiesResponse): string {
  return renderToStaticMarkup(
    <QueryClientProvider client={client()}>
      <CapabilitiesProvider initial={initial}>{ui}</CapabilitiesProvider>
    </QueryClientProvider>,
  );
}

const BEADS: WorkPlaneManifest = {
  adaptor_name: 'beads',
  adaptor_version: '0',
  protocol_version: '1',
  transport: 'api',
  state_map: { open: 'unstarted' },
  sprint_native: false,
  token_budget_enforced: false,
  evidence_synthesis_required: true,
  field_extensions: [{ name: 'bead_id', type: 'string' }],
  edge_extensions: [{ name: 'duplicates', directed: true }],
};

afterEach(() => {
  __resetExtensionRegistryForTests();
});

function BeadId({ value }: FieldExtensionProps) {
  return <code>{String(value)}</code>;
}

function Duplicates({ payload }: EdgeExtensionProps) {
  return <span>dup:{String(payload.id)}</span>;
}

describe('FieldExtensionSlot', () => {
  it('renders the registered widget when manifest declares the field', () => {
    registerFieldExtension('beads', 'bead_id', BeadId);
    const markup = render(<FieldExtensionSlot name="bead_id" value="gm-e11.4" />, {
      work_plane: BEADS,
      orchestration_plane: null,
    });
    expect(markup).toBe('<code>gm-e11.4</code>');
  });

  it('hides (no leak) when manifest does not declare the field', () => {
    registerFieldExtension('beads', 'bead_id', BeadId);
    const markup = render(
      <FieldExtensionSlot name="unknown_field" value="should-not-render" />,
      { work_plane: BEADS, orchestration_plane: null },
    );
    expect(markup).toBe('');
  });

  it('hides when no widget is registered even if manifest declares the field', () => {
    const markup = render(<FieldExtensionSlot name="bead_id" value="x" />, {
      work_plane: BEADS,
      orchestration_plane: null,
    });
    expect(markup).toBe('');
  });

  it('renders fallback on a miss', () => {
    const markup = render(
      <FieldExtensionSlot name="unknown" value="x" fallback={<em>n/a</em>} />,
      { work_plane: BEADS, orchestration_plane: null },
    );
    expect(markup).toBe('<em>n/a</em>');
  });

  it('does not render widgets registered for a different adaptor', () => {
    registerFieldExtension('jira', 'bead_id', BeadId);
    const markup = render(<FieldExtensionSlot name="bead_id" value="x" />, {
      work_plane: BEADS,
      orchestration_plane: null,
    });
    // beads manifest declares bead_id, but only the jira widget is
    // registered — namespacing rule: hide, don't leak.
    expect(markup).toBe('');
  });
});

describe('EdgeExtensionSlot', () => {
  it('renders when both manifest and registry agree', () => {
    registerEdgeExtension('beads', 'duplicates', Duplicates);
    const markup = render(
      <EdgeExtensionSlot name="duplicates" payload={{ id: 'gm-42' }} />,
      { work_plane: BEADS, orchestration_plane: null },
    );
    expect(markup).toBe('<span>dup:gm-42</span>');
  });
});
