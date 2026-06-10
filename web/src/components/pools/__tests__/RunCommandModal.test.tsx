// RunCommandModal tests (gm-s47n.17). Pins:
//   - command preview substitutes <field> placeholders live
//   - validators block the Confirm button
//   - submit fires the X-GEMBA-Confirm nonce header
//   - successful runs show stdout (or "(no output)") + stderr
//   - server-error envelopes render in the inline error panel

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { RunCommandModal, type ModalField } from '../RunCommandModal';

// Stand-in fetch — every test installs its own implementation
// before invoking the modal. We use a module-level handle so
// individual cases can swap behavior between renders.
const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
});
afterEach(() => {
  vi.unstubAllGlobals();
});

function makeFields(): ModalField[] {
  return [
    {
      name: 'name',
      label: 'Rig name',
      required: true,
      validate: (v) =>
        /^[A-Za-z0-9_-]+$/.test(v) ? null : 'Use letters, numbers, dash or underscore only',
    },
  ];
}

describe('RunCommandModal', () => {
  it('renders the command preview with placeholders before any input', () => {
    render(
      <RunCommandModal
        open
        title="Create rig"
        command="gt rig create <name>"
        fields={makeFields()}
        endpoint="/orchestration/scopes"
        method="POST"
        onClose={() => {}}
      />
    );
    const preview = screen.getByTestId('run-command-preview');
    expect(preview.textContent).toContain('<name>');
  });

  it('substitutes the field value into the command preview live', () => {
    render(
      <RunCommandModal
        open
        title="Create rig"
        command="gt rig create <name>"
        fields={makeFields()}
        endpoint="/orchestration/scopes"
        method="POST"
        onClose={() => {}}
      />
    );
    const input = screen.getByTestId('run-command-field-name') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'foo' } });
    expect(screen.getByTestId('run-command-preview').textContent).toBe('gt rig create foo');
  });

  it('blocks confirm when validation fails', () => {
    render(
      <RunCommandModal
        open
        title="Create rig"
        command="gt rig create <name>"
        fields={makeFields()}
        endpoint="/orchestration/scopes"
        method="POST"
        onClose={() => {}}
      />
    );
    const input = screen.getByTestId('run-command-field-name') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'has spaces!' } });
    const confirm = screen.getByTestId('run-command-confirm') as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    expect(screen.getByTestId('run-command-field-error-name').textContent).toContain(
      'Use letters'
    );
  });

  it('blocks confirm when a required field is empty', () => {
    render(
      <RunCommandModal
        open
        title="Create rig"
        command="gt rig create <name>"
        fields={makeFields()}
        endpoint="/orchestration/scopes"
        method="POST"
        onClose={() => {}}
      />
    );
    const confirm = screen.getByTestId('run-command-confirm') as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
  });

  it('submits with the X-GEMBA-Confirm nonce header on confirm', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      statusText: 'OK',
      json: async () => ({
        command: 'gt rig create foo',
        stdout: 'created rig foo',
        stderr: '',
        ok: true,
      }),
    });

    render(
      <RunCommandModal
        open
        title="Create rig"
        command="gt rig create <name>"
        fields={makeFields()}
        endpoint="/orchestration/scopes"
        method="POST"
        onClose={() => {}}
      />
    );
    fireEvent.change(screen.getByTestId('run-command-field-name'), {
      target: { value: 'foo' },
    });
    fireEvent.click(screen.getByTestId('run-command-confirm'));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/orchestration/scopes');
    expect(init.method).toBe('POST');
    expect(init.headers['X-GEMBA-Confirm']).toBeTruthy();
    expect(init.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(init.body as string)).toEqual({ name: 'foo' });
  });

  it('shows stdout on success, even when empty', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      statusText: 'OK',
      json: async () => ({
        command: 'gt rig create foo',
        stdout: '',
        stderr: '',
        ok: true,
      }),
    });

    render(
      <RunCommandModal
        open
        title="Create rig"
        command="gt rig create <name>"
        fields={makeFields()}
        endpoint="/orchestration/scopes"
        method="POST"
        onClose={() => {}}
      />
    );
    fireEvent.change(screen.getByTestId('run-command-field-name'), {
      target: { value: 'foo' },
    });
    fireEvent.click(screen.getByTestId('run-command-confirm'));

    await waitFor(() => screen.getByTestId('run-command-result'));
    expect(screen.getByTestId('run-command-stdout').textContent).toBe('(no output)');
  });

  it('renders stderr in red when ok=false', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      statusText: 'OK',
      json: async () => ({
        command: 'gt rig create foo',
        stdout: '',
        stderr: 'rig already exists',
        ok: false,
        error: 'exit status 1',
      }),
    });

    render(
      <RunCommandModal
        open
        title="Create rig"
        command="gt rig create <name>"
        fields={makeFields()}
        endpoint="/orchestration/scopes"
        method="POST"
        onClose={() => {}}
      />
    );
    fireEvent.change(screen.getByTestId('run-command-field-name'), {
      target: { value: 'foo' },
    });
    fireEvent.click(screen.getByTestId('run-command-confirm'));

    await waitFor(() => screen.getByTestId('run-command-stderr'));
    const stderr = screen.getByTestId('run-command-stderr');
    expect(stderr.className).toMatch(/text-red/);
    expect(screen.getByTestId('run-command-error-line').textContent).toContain('exit status 1');
  });

  it('renders the server error envelope on non-2xx', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 503,
      statusText: 'Service Unavailable',
      json: async () => ({ error: 'adaptor_unsupported', message: 'gt not bound' }),
    });

    render(
      <RunCommandModal
        open
        title="Create rig"
        command="gt rig create <name>"
        fields={makeFields()}
        endpoint="/orchestration/scopes"
        method="POST"
        onClose={() => {}}
      />
    );
    fireEvent.change(screen.getByTestId('run-command-field-name'), {
      target: { value: 'foo' },
    });
    fireEvent.click(screen.getByTestId('run-command-confirm'));

    await waitFor(() => screen.getByTestId('run-command-error'));
    expect(screen.getByTestId('run-command-error').textContent).toContain('adaptor_unsupported');
    expect(screen.getByTestId('run-command-error').textContent).toContain('gt not bound');
  });

  it('fires onConfirm callback after Close on a successful run', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      statusText: 'OK',
      json: async () => ({
        command: 'gt rig create foo',
        stdout: 'ok',
        stderr: '',
        ok: true,
      }),
    });

    const onConfirm = vi.fn();
    const onClose = vi.fn();
    render(
      <RunCommandModal
        open
        title="Create rig"
        command="gt rig create <name>"
        fields={makeFields()}
        endpoint="/orchestration/scopes"
        method="POST"
        onConfirm={onConfirm}
        onClose={onClose}
      />
    );
    fireEvent.change(screen.getByTestId('run-command-field-name'), {
      target: { value: 'foo' },
    });
    fireEvent.click(screen.getByTestId('run-command-confirm'));
    await waitFor(() => screen.getByTestId('run-command-result'));
    fireEvent.click(screen.getByTestId('run-command-close'));
    expect(onConfirm).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('uses bodyTransform when provided', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      statusText: 'OK',
      json: async () => ({ command: '', stdout: '', stderr: '', ok: true }),
    });
    const fields: ModalField[] = [
      {
        name: 'max_polecats',
        label: 'Max polecats',
        kind: 'number',
        required: true,
        validate: () => null,
      },
    ];
    render(
      <RunCommandModal
        open
        title="scheduler"
        command="gt config set scheduler.max_polecats <max_polecats>"
        fields={fields}
        endpoint="/orchestration/scheduler-config"
        method="PUT"
        bodyTransform={(v) => ({ max_polecats: parseInt(v.max_polecats ?? '0', 10) })}
        onClose={() => {}}
      />
    );
    fireEvent.change(screen.getByTestId('run-command-field-max_polecats'), {
      target: { value: '5' },
    });
    fireEvent.click(screen.getByTestId('run-command-confirm'));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body as string)).toEqual({ max_polecats: 5 });
  });
});
