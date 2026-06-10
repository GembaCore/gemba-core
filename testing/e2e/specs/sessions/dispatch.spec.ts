// specs/sessions/dispatch.spec.ts (gm-5v8v.9).
//
// Tier: route. NewSession flow: open dialog → pick a bead → pick an
// agent type → submit → POST /sessions. The deep-side bits (real
// tmux pane spawn, real worktree provisioning) are fixme'd to @deep
// per the bead.

import { expect, test } from '../../fixtures/server';
import { SessionsPage } from '../../pages/SessionsPage';
import { workItem, resetIds } from '../../builders/workitem';
import { agent, resetAgentIds } from '../../builders/agent';
import { resetSessionIds } from '../../builders/session';

test.beforeEach(() => {
  resetIds();
  resetAgentIds();
  resetSessionIds();
});

test('clicking "New session" opens the dialog @sessions', async ({
  page,
  sessionPlane,
  workPlane,
  agentPlane,
}) => {
  sessionPlane.seed([]);
  workPlane.seed([workItem({ id: 'gm-1' })]);
  agentPlane.seed([agent({ dialect: 'claude' })]);
  const sp = new SessionsPage(page);
  await sp.goto();
  await sp.newSessionButton.click();
  await expect(sp.newSessionDialog()).toBeVisible();
});

test('submit posts to /api/sessions and seeds the list @sessions', async ({
  page,
  sessionPlane,
  workPlane,
  agentPlane,
}) => {
  sessionPlane.seed([]);
  workPlane.seed([
    // Picker filters out started/completed/canceled, so seed a bead
    // in unstarted so it shows up.
    workItem({ id: 'gm-target', state_category: 'unstarted', title: 'pick me' }),
  ]);
  agentPlane.seed([agent({ dialect: 'claude' })]);

  const sp = new SessionsPage(page);
  await sp.goto();
  await sp.newSessionButton.click();
  await expect(sp.newSessionDialog()).toBeVisible();

  // Capture the POST so we can assert on the body shape.
  const [postReq] = await Promise.all([
    page.waitForRequest(
      (req) => req.url().endsWith('/api/sessions') && req.method() === 'POST'
    ),
    (async () => {
      // Pick the bead via the <select size=6> picker.
      await page.getByTestId('new-session-bead').selectOption('gm-target');
      await page.getByTestId('new-session-agent-type').selectOption('claude');
      await page.getByTestId('new-session-submit').click();
    })(),
  ]);

  const body = postReq.postDataJSON();
  expect(body).toMatchObject({ bead_id: 'gm-target', agent_type: 'claude' });
});

test.fixme('@deep session dispatch spawns a tmux pane + provisions worktree', () => {
  /* fixme: deferred to deep mode (gm-5v8v.2). The real-server
     fixture has the bd CLI available but tmux dispatch is the
     orchestrator's job; gm-native.20 wires the chain end-to-end and
     scripts/e2e/hello-world.test.mjs already proves it. The
     migration into specs/integration/ lives at gm-5v8v.15. */
});
