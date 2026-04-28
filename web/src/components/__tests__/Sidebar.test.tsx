// Sidebar tests (gm-root.17.12).
//
// Coverage:
//   - active project: workspace-scoped items render as interactive links
//   - cold-start (no projects): workspace-scoped items render as muted
//     spans with aria-disabled="true" and a tooltip; clicking is a no-op
//   - Settings + Capability Browser remain interactive on cold-start
//   - initial-fetch loading state does not flash the muted UI

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Sidebar } from '../Sidebar';
import { ProjectPickerProvider } from '../projectpicker/ProjectPickerContext';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function renderSidebar() {
  return render(
    <MemoryRouter initialEntries={['/new']}>
      <ProjectPickerProvider>
        <Sidebar />
      </ProjectPickerProvider>
    </MemoryRouter>
  );
}

// Six-item left rail (gm-e12.19, second amendment). Capability
// Browser and Drift folded into Settings / Escalations panes; only
// Settings remains workspace-agnostic (so a fresh install can reach
// global config from a cold start).
const WORKSPACE_SCOPED = [
  'Plan',
  'Review',
  'Escalations',
  'Insights',
  'Agent Sessions',
];

const ALWAYS_OPERATIONAL = ['Settings'];

describe('Sidebar', () => {
  let fetchSpy: ReturnType<typeof vi.fn>;
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    fetchSpy = vi.fn();
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it('renders all workspace-scoped items as links when a project is active', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        projects: [{ name: 'demo', path: '/tmp/demo', active: true }],
        total: 1,
      })
    );
    renderSidebar();

    for (const label of WORKSPACE_SCOPED) {
      const link = await screen.findByRole('link', { name: label });
      expect(link).toBeTruthy();
      expect(link.getAttribute('href')).toBeTruthy();
    }
  });

  it('cold-start: workspace-scoped items render as aria-disabled spans', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderSidebar();

    // Wait for the picker's initial fetch to land — Settings is always
    // a link, so its presence signals the sidebar has rendered at all.
    await screen.findByRole('link', { name: 'Settings' });

    for (const label of WORKSPACE_SCOPED) {
      // The item still shows the label (greyed, not removed).
      const labelEl = screen.getByText(label, { selector: 'span' });
      expect(labelEl).toBeTruthy();
      // Find the wrapping aria-disabled element.
      const wrap = labelEl.closest('[aria-disabled="true"]');
      expect(wrap).toBeTruthy();
      expect(wrap?.getAttribute('title')).toMatch(/Available after/);
      // Crucially NOT a real link.
      expect(wrap?.tagName.toLowerCase()).toBe('span');
    }
  });

  it('cold-start: Settings stays interactive', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderSidebar();

    for (const label of ALWAYS_OPERATIONAL) {
      const link = await screen.findByRole('link', { name: label });
      expect(link).toBeTruthy();
      expect(link.getAttribute('href')).toBeTruthy();
    }
  });

  it('does not flash muted UI during the initial fetch', async () => {
    // Hold the fetch open so isLoading remains true throughout.
    let resolve: (v: Response) => void = () => {};
    fetchSpy.mockReturnValue(
      new Promise<Response>((r) => {
        resolve = r;
      })
    );
    renderSidebar();

    // While loading, no items should be aria-disabled (we suppress
    // muting until the first fetch lands so the UI doesn't flicker).
    expect(document.querySelector('[aria-disabled="true"]')).toBeNull();

    // Resolve to drain the fixture.
    resolve(jsonResponse({ projects: [], total: 0 }));
  });
});
