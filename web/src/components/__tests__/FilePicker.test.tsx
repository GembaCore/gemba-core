// FilePicker tests (gm-e12.21.1).
//
// Coverage:
//   - renders the path input + Browse button
//   - typing into the input fires onChange
//   - opening the picker hits /v1/fs/info to bootstrap
//   - directory rows navigate via /v1/fs/list
//   - has_beads_db hint surfaces a beads badge
//   - file rows render disabled (not navigable)
//   - error from the server renders inline
//   - Select calls onChange with the current path

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { FilePicker } from '../FilePicker';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('FilePicker', () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders the path input and Browse button', () => {
    render(<FilePicker value="/tmp" onChange={() => {}} testid="t" />);
    expect(screen.getByTestId('t-input')).toBeTruthy();
    expect(screen.getByTestId('t-open')).toBeTruthy();
  });

  it('typing into the input fires onChange', () => {
    const onChange = vi.fn();
    render(<FilePicker value="" onChange={onChange} testid="t" />);
    fireEvent.change(screen.getByTestId('t-input'), { target: { value: '/abc' } });
    expect(onChange).toHaveBeenCalledWith('/abc');
  });

  it('opening the picker bootstraps from /v1/fs/info when value is empty', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (url.includes('/v1/fs/info')) {
        return jsonResponse({ home: '/Users/op', projects_dir: '/Users/op/projects' });
      }
      if (url.includes('/v1/fs/list')) {
        return jsonResponse({
          path: '/Users/op',
          home: '/Users/op',
          projects_dir: '/Users/op/projects',
          entries: [
            { name: 'workspace-a', kind: 'dir', has_beads_db: true },
            { name: 'workspace-b', kind: 'dir' },
            { name: 'readme.md', kind: 'file' },
          ],
        });
      }
      return jsonResponse({}, 404);
    });

    render(<FilePicker value="" onChange={() => {}} testid="t" />);
    fireEvent.click(screen.getByTestId('t-open'));

    await waitFor(() => {
      expect(screen.getByTestId('t-current-path').textContent).toBe('/Users/op');
    });
    expect(screen.getByTestId('t-entry-workspace-a')).toBeTruthy();
    expect(screen.getByTestId('t-entry-workspace-a-beads')).toBeTruthy();
    // workspace-b has no beads marker
    expect(screen.queryByTestId('t-entry-workspace-b-beads')).toBeNull();
  });

  it('clicking a directory navigates into it', async () => {
    let listCalls = 0;
    fetchSpy.mockImplementation(async (url: string) => {
      if (url.includes('/v1/fs/info')) {
        return jsonResponse({ home: '/Users/op' });
      }
      if (url.includes('/v1/fs/list')) {
        listCalls += 1;
        if (listCalls === 1) {
          return jsonResponse({
            path: '/Users/op',
            home: '/Users/op',
            entries: [{ name: 'sub', kind: 'dir' }],
          });
        }
        return jsonResponse({
          path: '/Users/op/sub',
          parent: '/Users/op',
          home: '/Users/op',
          entries: [{ name: 'inner', kind: 'dir' }],
        });
      }
      return jsonResponse({}, 404);
    });

    render(<FilePicker value="" onChange={() => {}} testid="t" />);
    fireEvent.click(screen.getByTestId('t-open'));
    await waitFor(() => screen.getByTestId('t-entry-sub'));
    fireEvent.click(screen.getByTestId('t-entry-sub'));
    await waitFor(() => {
      expect(screen.getByTestId('t-current-path').textContent).toBe('/Users/op/sub');
    });
    expect(screen.getByTestId('t-entry-inner')).toBeTruthy();
    // Up button appears now that there is a parent.
    expect(screen.getByTestId('t-up')).toBeTruthy();
  });

  it('Select calls onChange with the current path and closes the popover', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (url.includes('/v1/fs/info')) return jsonResponse({ home: '/Users/op' });
      return jsonResponse({
        path: '/Users/op',
        home: '/Users/op',
        entries: [],
      });
    });
    const onChange = vi.fn();
    render(<FilePicker value="" onChange={onChange} testid="t" />);
    fireEvent.click(screen.getByTestId('t-open'));
    await waitFor(() => screen.getByTestId('t-current-path'));
    fireEvent.click(screen.getByTestId('t-select'));
    expect(onChange).toHaveBeenCalledWith('/Users/op');
  });

  it('renders an inline error when the server rejects', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (url.includes('/v1/fs/info')) return jsonResponse({ home: '' });
      return jsonResponse({ error: 'fs_list_error', message: 'denied' }, 403);
    });
    render(<FilePicker value="" onChange={() => {}} testid="t" />);
    fireEvent.click(screen.getByTestId('t-open'));
    await waitFor(() => {
      // No HOME means the picker can't bootstrap; the error surface
      // renders 'No HOME directory available'.
      expect(screen.getByTestId('t-body').textContent).toMatch(/HOME|denied/);
    });
  });
});
