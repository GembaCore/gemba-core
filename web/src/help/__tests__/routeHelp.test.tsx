// Route help module tests — gm-root.22.3.
//
// Coverage:
//   - Each route help module renders without throwing.
//   - Critical live links are present (the project's most-important
//     affordance per route).
//   - resolveHelpComponent returns the correct component for known paths.
//   - resolveHelpComponent returns DefaultHelp for unknown paths.

import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';

import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';
import { RouteHelp as BoardHelp } from '../BoardHelp';
import { RouteHelp as GraphHelp } from '../GraphHelp';
import { RouteHelp as WalkHelp } from '../WalkHelp';
import { RouteHelp as EscalationsHelp } from '../EscalationsHelp';
import { RouteHelp as InsightsHelp } from '../InsightsHelp';
import { RouteHelp as SessionsHelp } from '../SessionsHelp';
import { RouteHelp as SettingsHelp } from '../SettingsHelp';
import { RouteHelp as ColdStartHelp } from '../ColdStartHelp';
import { RouteHelp as DefaultHelp } from '../DefaultHelp';
import { resolveHelpComponent } from '../index';

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

// Render a help component inside a MemoryRouter (required because
// modules use <Link> from react-router-dom).
function renderHelp(Component: React.ComponentType, path = '/') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter initialEntries={[path]}>
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps}>
          <Component />
        </CapabilitiesProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
}

describe('BoardHelp', () => {
  it('renders without throwing', () => {
    expect(() => renderHelp(BoardHelp, '/board')).not.toThrow();
  });

  it('has a link to /walk', () => {
    renderHelp(BoardHelp, '/board');
    const walkLink = screen.getByRole('link', { name: /Gemba walk/i });
    expect(walkLink).toBeTruthy();
  });

  it('has a link to the docsite Getting Started guide', () => {
    renderHelp(BoardHelp, '/board');
    const anchor = screen.getByRole('link', {
      name: /Running Gemba against your work items/i,
    });
    expect(anchor.getAttribute('href')).toMatch(/gembacore\.github\.io/);
  });

  it('mentions hotkey hints', () => {
    renderHelp(BoardHelp, '/board');
    // BoardHelp mentions 'n' to create a new work item.
    expect(screen.getByText(/new work item/i)).toBeTruthy();
  });
});

describe('WalkHelp', () => {
  it('renders without throwing', () => {
    expect(() => renderHelp(WalkHelp, '/walk')).not.toThrow();
  });

  it('mentions ratify / modify / reject / defer hotkeys', () => {
    renderHelp(WalkHelp, '/walk');
    // Walk hotkey hints: r / m / x / d
    // Use getAllByText since the text may appear in multiple list items.
    expect(screen.getAllByText(/ratify/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/modify/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/reject/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/defer/i).length).toBeGreaterThan(0);
  });

  it('has a link back to the Plan board', () => {
    renderHelp(WalkHelp, '/walk');
    const boardLink = screen.getByRole('link', { name: /Plan board/i });
    expect(boardLink).toBeTruthy();
  });
});

describe('GraphHelp', () => {
  it('renders without throwing', () => {
    expect(() => renderHelp(GraphHelp, '/graph')).not.toThrow();
  });

  it('mentions filters and critical path controls', () => {
    renderHelp(GraphHelp, '/graph');
    expect(screen.getByText(/title or id search/i)).toBeTruthy();
    expect(screen.getByText(/Critical path/i)).toBeTruthy();
    expect(screen.getByText(/Cycles/i)).toBeTruthy();
  });

  it('has a link back to the Plan board', () => {
    renderHelp(GraphHelp, '/graph');
    const boardLink = screen.getByRole('link', { name: /Plan board/i });
    expect(boardLink).toBeTruthy();
  });
});

describe('EscalationsHelp', () => {
  it('renders without throwing', () => {
    expect(() => renderHelp(EscalationsHelp, '/escalations')).not.toThrow();
  });

  it('has a link to /walk', () => {
    renderHelp(EscalationsHelp, '/escalations');
    const walkLink = screen.getByRole('link', { name: /Gemba walk/i });
    expect(walkLink).toBeTruthy();
  });

  it('has a docsite link', () => {
    renderHelp(EscalationsHelp, '/escalations');
    const anchors = screen.getAllByRole('link');
    const docsiteLinks = anchors.filter((a) =>
      a.getAttribute('href')?.includes('gembacore.github.io')
    );
    expect(docsiteLinks.length).toBeGreaterThan(0);
  });
});

describe('InsightsHelp', () => {
  it('renders without throwing', () => {
    expect(() => renderHelp(InsightsHelp, '/insights')).not.toThrow();
  });

  it('has a link to /insights/personas', () => {
    renderHelp(InsightsHelp, '/insights');
    const personasLink = screen.getByRole('link', { name: /Persona consult activity/i });
    expect(personasLink).toBeTruthy();
  });
});

describe('SessionsHelp', () => {
  it('renders without throwing', () => {
    expect(() => renderHelp(SessionsHelp, '/sessions')).not.toThrow();
  });

  it('has a link to the Plan board', () => {
    renderHelp(SessionsHelp, '/sessions');
    const boardLink = screen.getByRole('link', { name: /Plan board/i });
    expect(boardLink).toBeTruthy();
  });

  it('mentions the new-session hotkey', () => {
    renderHelp(SessionsHelp, '/sessions');
    // Mod+Shift+S is the new-session hotkey. Use getAllByText since the
    // phrase 'new session' may match list items and the title.
    expect(screen.getAllByText(/new session/i).length).toBeGreaterThan(0);
  });
});

describe('SettingsHelp', () => {
  it('renders without throwing', () => {
    expect(() => renderHelp(SettingsHelp, '/settings')).not.toThrow();
  });

  it('has a link to the adaptor docs', () => {
    renderHelp(SettingsHelp, '/settings');
    const adaptorLink = screen.getByRole('link', { name: /Adaptors/i });
    expect(adaptorLink.getAttribute('href')).toMatch(/gembacore\.github\.io/);
  });
});

describe('ColdStartHelp', () => {
  it('renders without throwing', () => {
    expect(() => renderHelp(ColdStartHelp, '/')).not.toThrow();
  });

  it('has a link to /settings', () => {
    renderHelp(ColdStartHelp, '/');
    const settingsLink = screen.getByRole('link', { name: /Configure settings/i });
    expect(settingsLink).toBeTruthy();
  });

  it('has a link to the Getting Started guide', () => {
    renderHelp(ColdStartHelp, '/');
    const guideLink = screen.getByRole('link', { name: /Getting Started guide/i });
    expect(guideLink.getAttribute('href')).toMatch(/gembacore\.github\.io/);
  });

  it('does NOT mention workspace-scoped surfaces', () => {
    renderHelp(ColdStartHelp, '/');
    // Cold-start help must not mention Board, Walk, Sessions, etc.
    expect(screen.queryByText(/Plan board/i)).toBeNull();
    expect(screen.queryByText(/Gemba walk/i)).toBeNull();
    expect(screen.queryByText(/Sessions/i)).toBeNull();
    expect(screen.queryByText(/Escalations/i)).toBeNull();
  });
});

describe('DefaultHelp', () => {
  it('renders without throwing', () => {
    expect(() => renderHelp(DefaultHelp, '/unknown')).not.toThrow();
  });

  it('has a link to the full docsite', () => {
    renderHelp(DefaultHelp, '/unknown');
    const docsiteLink = screen.getByRole('link', { name: /Full docsite/i });
    expect(docsiteLink.getAttribute('href')).toMatch(/gembacore\.github\.io/);
  });
});

describe('resolveHelpComponent', () => {
  it('returns BoardHelp for /board', () => {
    const C = resolveHelpComponent('/board');
    renderHelp(C, '/board');
    expect(screen.getByTestId('help-board')).toBeTruthy();
  });

  it('returns WalkHelp for /walk', () => {
    const C = resolveHelpComponent('/walk');
    renderHelp(C, '/walk');
    expect(screen.getByTestId('help-walk')).toBeTruthy();
  });

  it('returns GraphHelp for /graph', () => {
    const C = resolveHelpComponent('/graph');
    renderHelp(C, '/graph');
    expect(screen.getByTestId('help-graph')).toBeTruthy();
  });

  it('returns WalkHelp for /walks/123 (prefix match)', () => {
    const C = resolveHelpComponent('/walks/123');
    renderHelp(C, '/walks/123');
    expect(screen.getByTestId('help-walk')).toBeTruthy();
  });

  it('returns EscalationsHelp for /escalations', () => {
    const C = resolveHelpComponent('/escalations');
    renderHelp(C, '/escalations');
    expect(screen.getByTestId('help-escalations')).toBeTruthy();
  });

  it('returns InsightsHelp for /insights/personas (prefix match)', () => {
    const C = resolveHelpComponent('/insights/personas');
    renderHelp(C, '/insights/personas');
    expect(screen.getByTestId('help-insights')).toBeTruthy();
  });

  it('returns SessionsHelp for /sessions', () => {
    const C = resolveHelpComponent('/sessions');
    renderHelp(C, '/sessions');
    expect(screen.getByTestId('help-sessions')).toBeTruthy();
  });

  it('returns SettingsHelp for /settings', () => {
    const C = resolveHelpComponent('/settings');
    renderHelp(C, '/settings');
    expect(screen.getByTestId('help-settings')).toBeTruthy();
  });

  it('returns DefaultHelp for an unknown path', () => {
    const C = resolveHelpComponent('/totally-unknown');
    renderHelp(C, '/totally-unknown');
    expect(screen.getByText(/About Gemba/i)).toBeTruthy();
  });
});
