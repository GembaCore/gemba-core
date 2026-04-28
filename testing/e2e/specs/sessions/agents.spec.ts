// specs/sessions/agents.spec.ts (gm-5v8v.9).
//
// Tier: route. AgentsPage live tile rendering — kind-specific
// iconography, current-session badge derived from the sessions list,
// idle marker, drawer drill-in. Live SSE-driven updates are covered
// by the gm-e12.4 vitest suite (web/src/pages/__tests__/
// AgentsPage.test.tsx) under a mocked QueryClient; here we exercise
// the same property end-to-end through the fake fixture.
//
// gm-9a7v: every test below is .skip-ed because the AgentsPage grid
// + AgentDetailDrawer surface (data-testids agents-grid, agents-empty,
// agent-tile-*, agent-drawer) was deleted by gm-e12.15 in favour of
// the per-agent /agents/:id detail page. Re-enable after the rewrite
// described in gm-9a7v lands.

import { expect, test } from '../../fixtures/server';
import { AgentsPage } from '../../pages/AgentsPage';
import { agent, resetAgentIds } from '../../builders/agent';
import { session, resetSessionIds } from '../../builders/session';

test.beforeEach(() => {
  resetAgentIds();
  resetSessionIds();
});

test.skip('empty roster renders the empty-state @sessions', async ({ page, agentPlane }) => {
  agentPlane.seed([]);
  const ap = new AgentsPage(page);
  await ap.goto();
  await expect(ap.empty).toBeVisible();
});

test.skip('one tile per agent with kind-specific iconography @sessions', async ({
  page,
  agentPlane,
}) => {
  agentPlane.seed([
    agent({ id: 'a-1', agent_kind: 'agent', name: 'planner' }),
    agent({ id: 'h-1', agent_kind: 'human', name: 'mike', role: 'mayor' }),
  ]);
  const ap = new AgentsPage(page);
  await ap.goto();
  await expect(ap.tile('a-1')).toBeVisible();
  await expect(ap.tile('h-1')).toBeVisible();
  await expect(page.getByTestId('agent-tile-a-1-icon-agent')).toBeVisible();
  await expect(page.getByTestId('agent-tile-h-1-icon-human')).toBeVisible();
});

test.skip('agent with an observable session shows the status badge @sessions', async ({
  page,
  agentPlane,
  sessionPlane,
}) => {
  agentPlane.seed([agent({ id: 'a-1' })]);
  sessionPlane.seed([session({ id: 'sess-1', agent_id: 'a-1', status: 'working' })]);
  const ap = new AgentsPage(page);
  await ap.goto();
  await expect(ap.status('a-1')).toContainText('working');
  await expect(ap.tile('a-1')).toHaveAttribute('data-active', 'true');
});

test.skip('agent with only terminal sessions renders idle @sessions', async ({
  page,
  agentPlane,
  sessionPlane,
}) => {
  agentPlane.seed([agent({ id: 'a-1' })]);
  sessionPlane.seed([
    session({
      id: 'sess-1',
      agent_id: 'a-1',
      status: 'completed',
      ended_at: '2026-04-25T13:00:00Z',
    }),
  ]);
  const ap = new AgentsPage(page);
  await ap.goto();
  await expect(ap.idleMarker('a-1')).toBeVisible();
});

test.skip('clicking a tile opens the agent detail drawer @sessions', async ({
  page,
  agentPlane,
  sessionPlane,
}) => {
  agentPlane.seed([agent({ id: 'a-1' })]);
  sessionPlane.seed([session({ id: 'sess-1', agent_id: 'a-1', status: 'working' })]);
  const ap = new AgentsPage(page);
  await ap.goto();
  await ap.tile('a-1').click();
  await expect(ap.drawer).toBeVisible();
  await expect(ap.drawerForAgent('a-1')).toBeVisible();
  await expect(ap.drawerSession()).toBeVisible();
  await ap.drawerCloseButton().click();
  await expect(ap.drawer).toHaveCount(0);
});

test.fixme(
  'ending a session removes the agent active badge within event lag @sessions @realtime',
  () => {
    /* fixme: requires the SSE fixture from gm-5v8v.10. The session
       store invalidation today drives a refetch on the next
       interaction; the live-event path is what makes the "within
       event lag" property holdable. */
  }
);
