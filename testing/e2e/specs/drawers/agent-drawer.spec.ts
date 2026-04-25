// specs/drawers/agent-drawer.spec.ts — gm-5v8v.8 (in progress)
//
// AgentDetailDrawer is opened from /agents. Live transcript pane
// (gm-5uqu) and peek_session via tmux capture-pane are @deep — they
// require the orchestration adaptor wired to a real tmux session.

import { test } from '../../fixtures/server';

test.describe('AgentDetailDrawer @route', () => {
  test.fixme('opens from the agents page', async () => {
    // Needs an /agents-page POM + agent fixture. The page itself
    // is implemented but not yet covered by e2e — sibling bead in
    // the gm-5v8v family will land /agents specs.
  });

  test.fixme('renders status pill, role, and parent agent', async () => {
    // Static surface — can run in fake mode once a builder for
    // AgentRef + a /api/agents handler that surfaces it exists.
  });

  test.fixme('live transcript pane streams capture-pane output @deep', async () => {
    // gm-5uqu surface; requires a real tmux pane.
  });

  test.fixme('status transitions reflect peek_session @deep', async () => {
    // Real adaptor required.
  });
});
