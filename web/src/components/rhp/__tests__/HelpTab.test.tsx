// HelpTab tests — gm-root.22.3.
//
// Coverage:
//   - Renders BoardHelp on /board
//   - Renders WalkHelp on /walk
//   - Renders EscalationsHelp on /escalations
//   - Renders InsightsHelp on /insights
//   - Renders SessionsHelp on /sessions
//   - Renders SettingsHelp on /settings
//   - Cold-start (no active project, picker not loading) renders ColdStartHelp
//     regardless of route
//   - Unknown route renders DefaultHelp
//   - HelpTab registers as a pinned tab with id='help'
//   - Registered icon is HelpCircle from lucide-react

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { useEffect } from 'react';
import { act, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { HelpCircle } from 'lucide-react';
import { RhpProvider, useRhp } from '../RhpContext';
import { RhpPinnedContentProvider, useRhpPinnedContent } from '../RhpPinnedContent';
import { HelpTab } from '../HelpTab';
import { ProjectPickerProvider } from '@/components/projectpicker/ProjectPickerContext';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// Test wrapper: router + providers required by HelpTab.
function Wrapper({
  initialPath = '/board',
  children,
}: {
  initialPath?: string;
  children?: React.ReactNode;
}) {
  return (
    <MemoryRouter initialEntries={[initialPath]}>
      <ProjectPickerProvider>
        <RhpProvider>
          <RhpPinnedContentProvider>
            <HelpTab />
            {children}
          </RhpPinnedContentProvider>
        </RhpProvider>
      </ProjectPickerProvider>
    </MemoryRouter>
  );
}

// Renders the help body by resolving the registered content renderer.
function PinnedBodyRenderer() {
  const { resolve } = useRhpPinnedContent();
  const render = resolve('help');
  if (!render) return <div data-testid="no-content" />;
  return render();
}

// Reads tabs from RhpContext and passes them to the callback.
function TabsProbe({ onTabs }: { onTabs: (ids: string[]) => void }) {
  const { tabs } = useRhp();
  useEffect(() => {
    onTabs(tabs.map((t) => t.id));
  }, [tabs, onTabs]);
  return null;
}

// Reads the help tab's icon from RhpContext.
function IconProbe({ onIcon }: { onIcon: (icon: unknown) => void }) {
  const { tabs } = useRhp();
  const helpTab = tabs.find((t) => t.id === 'help');
  useEffect(() => {
    if (helpTab) onIcon(helpTab.icon);
  }, [helpTab, onIcon]);
  return null;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('HelpTab', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('pinned-tab registration', () => {
    it('registers a pinned tab with id=help on mount', async () => {
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue(
          jsonResponse({ projects: [{ name: 'demo', path: '/tmp', active: true }] })
        )
      );
      let capturedIds: string[] = [];
      render(
        <Wrapper initialPath="/board">
          <TabsProbe onTabs={(ids) => { capturedIds = ids; }} />
        </Wrapper>
      );
      await waitFor(() => {
        expect(capturedIds).toContain('help');
      });
    });

    it('registers the HelpCircle icon on the pinned tab', async () => {
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue(
          jsonResponse({ projects: [{ name: 'demo', path: '/tmp', active: true }] })
        )
      );
      let capturedIcon: unknown = null;
      render(
        <Wrapper initialPath="/board">
          <IconProbe onIcon={(icon) => { capturedIcon = icon; }} />
        </Wrapper>
      );
      await waitFor(() => {
        expect(capturedIcon).toBe(HelpCircle);
      });
    });
  });

  describe('route-based content dispatch', () => {
    function renderHelpAt(path: string) {
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue(
          jsonResponse({ projects: [{ name: 'demo', path: '/tmp', active: true }] })
        )
      );
      return render(
        <Wrapper initialPath={path}>
          <PinnedBodyRenderer />
        </Wrapper>
      );
    }

    it('renders BoardHelp on /board', async () => {
      renderHelpAt('/board');
      await waitFor(() => {
        expect(screen.getByTestId('help-board')).toBeTruthy();
      });
    });

    it('renders WalkHelp on /walk', async () => {
      renderHelpAt('/walk');
      await waitFor(() => {
        expect(screen.getByTestId('help-walk')).toBeTruthy();
      });
    });

    it('renders EscalationsHelp on /escalations', async () => {
      renderHelpAt('/escalations');
      await waitFor(() => {
        expect(screen.getByTestId('help-escalations')).toBeTruthy();
      });
    });

    it('renders InsightsHelp on /insights', async () => {
      renderHelpAt('/insights');
      await waitFor(() => {
        expect(screen.getByTestId('help-insights')).toBeTruthy();
      });
    });

    it('renders SessionsHelp on /sessions', async () => {
      renderHelpAt('/sessions');
      await waitFor(() => {
        expect(screen.getByTestId('help-sessions')).toBeTruthy();
      });
    });

    it('renders SettingsHelp on /settings', async () => {
      renderHelpAt('/settings');
      await waitFor(() => {
        expect(screen.getByTestId('help-settings')).toBeTruthy();
      });
    });

    it('renders DefaultHelp on an unknown route', async () => {
      renderHelpAt('/some-unknown-route');
      await waitFor(() => {
        // DefaultHelp has a heading "About Gemba".
        expect(screen.getByText(/About Gemba/i)).toBeTruthy();
      });
    });
  });

  describe('cold-start (no active project)', () => {
    it('renders ColdStartHelp when no project is active regardless of route', async () => {
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue(jsonResponse({ projects: [] }))
      );
      render(
        <Wrapper initialPath="/board">
          <PinnedBodyRenderer />
        </Wrapper>
      );
      await waitFor(() => {
        expect(screen.getByTestId('help-cold-start')).toBeTruthy();
      });
    });

    it('does NOT render ColdStartHelp while the picker is still loading', async () => {
      // Never resolves — keeps isLoading=true.
      vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise(() => {})));
      render(
        <Wrapper initialPath="/board">
          <PinnedBodyRenderer />
        </Wrapper>
      );
      await act(async () => {});
      // While loading the picker hasn't returned, so we don't know if it's
      // cold-start. The cold-start panel should NOT be visible yet.
      expect(screen.queryByTestId('help-cold-start')).toBeNull();
    });
  });
});
