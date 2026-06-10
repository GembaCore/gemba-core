import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { InteractionPanel } from '../InteractionPanel';
import type { InteractionSession } from '@/interactions/types';

const session: InteractionSession = {
  id: 'workitem:gm-1:interaction',
  kind: 'pm_consult',
  status: 'waiting_on_operator',
  uiHost: 'rhp',
  runtimeHost: 'gastown_crew',
  runtimeLabel: 'Gas Town crew',
  scope: {
    type: 'workitem',
    id: 'gm-1',
    title: 'Implement converter',
    breadcrumb: [{ type: 'epic', id: 'gm-e1', label: 'Temperature epic' }],
  },
  messages: [
    { id: 'm1', role: 'system', body: 'Routed through Gas Town.' },
    { id: 'm2', role: 'assistant', body: 'Ready to refine the work.' },
  ],
  suggestedActions: [
    {
      id: 'dispatch',
      label: 'Dispatch runtime',
      description: 'Request a Gas Town crew session.',
    },
  ],
  draft: {
    title: 'Working Brief',
    summary: 'One interaction shape hosts transcript, draft, and actions.',
    bullets: ['Shared shell', 'Runtime-owned execution'],
  },
  evidence: [{ id: 'ev-1', label: 'commit: abc123' }],
  quickReplies: [
    { id: 'looks-good', label: 'Looks good', message: 'This draft looks good.' },
  ],
  capabilities: ['transcript.peek', 'input.send'],
};

describe('InteractionPanel', () => {
  it('renders transcript, runtime host, draft, actions, evidence, and capabilities', () => {
    render(<InteractionPanel session={session} onSend={vi.fn()} />);

    expect(screen.getByTestId('interaction-panel')).toBeTruthy();
    expect(screen.getByText('Implement converter')).toBeTruthy();
    expect(screen.getByText('Gas Town crew')).toBeTruthy();
    expect(screen.getByTestId('interaction-transcript').textContent).toContain(
      'Ready to refine'
    );
    expect(screen.getByTestId('interaction-draft').textContent).toContain(
      'One interaction shape'
    );
    expect(screen.getByTestId('interaction-actions').textContent).toContain('Dispatch runtime');
    expect(screen.getByTestId('interaction-evidence').textContent).toContain('commit');
    expect(screen.getByTestId('interaction-capabilities').textContent).toContain('input.send');
    expect(screen.getByTestId('interaction-quick-replies').textContent).toContain('Looks good');
  });

  it('sends an interactive turn when a sender is provided', () => {
    const onSend = vi.fn();
    render(<InteractionPanel session={session} onSend={onSend} />);

    fireEvent.change(screen.getByTestId('interaction-composer-input'), {
      target: { value: 'Please split this batch.' },
    });
    fireEvent.click(screen.getByTestId('interaction-composer-submit'));

    expect(onSend).toHaveBeenCalledWith('Please split this batch.');
  });

  it('sends a prepared quick reply', () => {
    const onSend = vi.fn();
    render(<InteractionPanel session={session} onSend={onSend} />);

    fireEvent.click(screen.getByText('Looks good'));

    expect(onSend).toHaveBeenCalledWith('This draft looks good.');
  });
});
