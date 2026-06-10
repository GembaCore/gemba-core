// specs/drawers/agent-drawer.spec.ts — gm-5v8v.8
//
// AgentDetailDrawer (web/src/components/agents/AgentDetailDrawer.tsx).
// The open/close cycle (tile click → drawer → close) is owned by
// gm-5v8v.9's sessions/agents.spec.ts; this file owns assertions on
// what's *inside* the drawer body — metadata rows, idle-state, and
// the transcript surface (gm-5uqu).

import { test, expect } from '../../fixtures/server';
import { AgentsPage } from '../../pages/AgentsPage';
import { agent, resetAgentIds } from '../../builders/agent';
import { session, resetSessionIds } from '../../builders/session';
import { orchestrationManifest } from '../../fixtures/capabilitiesPlane';

test.beforeEach(() => {
  resetAgentIds();
  resetSessionIds();
});

// gm-9a7v: AgentsPage / AgentDetailDrawer surface (data-testids
// agents-grid, agents-empty, agent-drawer, agent-tile-*) was removed
// when gm-e12.15 introduced /agents/:id. The page-object + every
// assertion in this file targets a SPA surface that no longer
// exists, so all 12 tests time out for 31s waiting on missing
// locators. Skipped at the describe level until the rewrite lands.
test.describe.skip('AgentDetailDrawer body @route — TODO gm-9a7v rewrite vs current SPA', () => {
  test('renders the metadata rows for a non-human agent', async ({
    page,
    agentPlane,
  }) => {
    agentPlane.seed([
      agent({
        id: 'a-meta-1',
        name: 'planner-bot',
        agent_kind: 'agent',
        role: 'crew',
        dialect: 'claude',
        workspace: 'gemba',
        parent_id: 'gemba/mayor',
      }),
    ]);

    const ap = new AgentsPage(page);
    await ap.goto();
    await ap.tile('a-meta-1').click();

    const body = ap.drawerForAgent('a-meta-1');
    await expect(body).toBeVisible();
    // The agent card sets the title from name when set, falling back
    // to id. We rendered both above.
    await expect(body).toContainText('planner-bot');
    await expect(body).toContainText('a-meta-1');
    // DetailRows render label + value as adjacent spans; assert each
    // value the seeded agent supplies appears in the body.
    await expect(body).toContainText('agent'); // Kind
    await expect(body).toContainText('crew'); // Role
    await expect(body).toContainText('claude'); // Dialect
    await expect(body).toContainText('gemba'); // Workspace
    await expect(body).toContainText('gemba/mayor'); // Parent
  });

  test('idle state renders when the agent has no observable session', async ({
    page,
    agentPlane,
  }) => {
    agentPlane.seed([agent({ id: 'a-idle' })]);

    const ap = new AgentsPage(page);
    await ap.goto();
    await ap.tile('a-idle').click();

    await expect(ap.drawerForAgent('a-idle')).toBeVisible();
    await expect(page.getByTestId('agent-drawer-idle')).toBeVisible();
    // The session pane and transcript pane should not render in idle
    // state — they're inside the conditional `session ?` branch.
    await expect(ap.drawerSession()).toHaveCount(0);
    await expect(page.getByTestId('agent-drawer-transcript')).toHaveCount(0);
  });

  test('active session pane renders status and identifiers', async ({
    page,
    agentPlane,
    sessionPlane,
  }) => {
    agentPlane.seed([agent({ id: 'a-busy' })]);
    sessionPlane.seed([
      session({
        id: 'sess-busy',
        agent_id: 'a-busy',
        assignment_id: 'gm-test-asg',
        status: 'working',
        last_heartbeat: '2026-04-25T12:30:00Z',
      }),
    ]);

    const ap = new AgentsPage(page);
    await ap.goto();
    await ap.tile('a-busy').click();

    const sessionPane = ap.drawerSession();
    await expect(sessionPane).toBeVisible();
    await expect(sessionPane).toContainText('working'); // Status
    await expect(sessionPane).toContainText('sess-busy'); // Session id
    await expect(sessionPane).toContainText('gm-test-asg'); // Assignment
  });

  test('Escape closes the drawer', async ({ page, agentPlane }) => {
    agentPlane.seed([agent({ id: 'a-esc' })]);

    const ap = new AgentsPage(page);
    await ap.goto();
    await ap.tile('a-esc').click();
    await expect(ap.drawer).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(ap.drawer).toHaveCount(0);
  });

  test('transcript pane renders when OrchestrationPlane declares peek_modes=transcript', async ({
    page,
    agentPlane,
    sessionPlane,
    capabilitiesPlane,
  }) => {
    // gm-5uqu shipped TranscriptPane; it renders only when
    // useCapabilities() returns an OrchestrationPlane manifest whose
    // peek_modes includes 'transcript'. With the capabilities fixture
    // (gm-5v8v.7/.8 follow-up) we can finally seed that.
    capabilitiesPlane.setOrchestrationPlane(
      orchestrationManifest({ peek_modes: ['transcript'] })
    );
    agentPlane.seed([agent({ id: 'a-tx' })]);
    sessionPlane.seed([
      session({ id: 'sess-tx', agent_id: 'a-tx', status: 'working' }),
    ]);

    const ap = new AgentsPage(page);
    await ap.goto();
    await ap.tile('a-tx').click();

    await expect(page.getByTestId('agent-drawer-transcript')).toBeVisible();
    // Header label is part of the pane.
    await expect(page.getByTestId('agent-drawer-transcript')).toContainText('Transcript');
  });

  test('transcript pane stays hidden when peek_modes omits transcript', async ({
    page,
    agentPlane,
    sessionPlane,
    capabilitiesPlane,
  }) => {
    // The capability gate is per-mode: declaring peek_modes=['screenshot']
    // explicitly proves the gate fires on the missing 'transcript'
    // entry, not on a null plane.
    capabilitiesPlane.setOrchestrationPlane(
      orchestrationManifest({ peek_modes: ['screenshot'] })
    );
    agentPlane.seed([agent({ id: 'a-no-tx' })]);
    sessionPlane.seed([session({ id: 'sess-no-tx', agent_id: 'a-no-tx', status: 'working' })]);

    const ap = new AgentsPage(page);
    await ap.goto();
    await ap.tile('a-no-tx').click();

    await expect(ap.drawerSession()).toBeVisible();
    await expect(page.getByTestId('agent-drawer-transcript')).toHaveCount(0);
  });

  test.fixme('transcript content streams from /api/sessions/:id/peek @deep', async () => {
    // gm-5v8v.2 deep mode — once the bd-init isolation gate (gm-h4n)
    // closes and deep specs run cleanly in CI, this asserts a real
    // tmux capture-pane lands in the transcript pane.
  });
});
