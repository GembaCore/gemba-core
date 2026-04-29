// EpicView escalation-badge integration (gm-e11.3). Verifies that the
// page-supplied `escalationCounts` map drives the EpicCard badge: an
// epic id with a positive count renders the badge, others don't.

import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';
import type { WorkItem } from '@/types/core.gen';
import { EpicView } from '../EpicView';

const caps: CapabilitiesResponse = {
  work_plane: {
    adaptor_name: 'fake',
    adaptor_version: '0.1.0',
    protocol_version: '0.1.0',
    transport: 'api',
    state_map: { open: 'unstarted' },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
  },
  orchestration_plane: null,
};

function epic(id: string): WorkItem {
  return {
    id,
    kind: 'epic',
    title: id,
    status: 'open',
    state_category: 'started',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
  };
}

function wrap(ui: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps}>{ui}</CapabilitiesProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
}

describe('EpicView escalation badge wiring (gm-e11.3)', () => {
  it('renders the escalation badge on the targeted epic', () => {
    const items: WorkItem[] = [epic('e1'), epic('e2')];
    const counts = new Map<string, number>([['e1', 3]]);
    render(
      wrap(
        <EpicView items={items} onSelectEpic={vi.fn()} escalationCounts={counts} />
      )
    );
    const badges = screen.getAllByTestId('epic-badge-escalations');
    expect(badges.length).toBe(1);
    expect(badges[0].textContent).toContain('3');
    expect(badges[0].getAttribute('title')).toContain('3 open escalations');
  });

  it('omits the badge when no escalation targets the epic', () => {
    const items: WorkItem[] = [epic('e1')];
    const counts = new Map<string, number>();
    render(
      wrap(
        <EpicView items={items} onSelectEpic={vi.fn()} escalationCounts={counts} />
      )
    );
    expect(screen.queryByTestId('epic-badge-escalations')).toBeNull();
  });
});
