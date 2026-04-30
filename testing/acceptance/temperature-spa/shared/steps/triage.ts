// shared/steps/triage.ts (gm-root.27.11) — Triage step.
//
// Runs BETWEEN M2 and M3. Mints a synthetic blocking escalation
// against a bead in M3 (the next milestone), drives Playwright to
// /escalations to approve it, verifies the escalation transitions to
// resolved and the targeted bead becomes ready.
//
// Per D15 §11.6, the SPA escalation respond UI is partially shipped.
// Per the current code (web/src/pages/EscalationsPage.tsx) the resolve
// modal IS present (testids `escalation-card-{id}-resolve`,
// `escalation-resolve-modal`, `escalation-resolve-option-approve`,
// `escalation-resolve-confirm`). We still keep an API fallback in
// case the modal flow regresses on a particular SPA build, and file a
// bug bead noting the regression rather than dropping the test.

import { setTimeout as sleep } from 'node:timers/promises';
import type { SharedContext } from '../spec';

/**
 * Some bead in M3 we'll target with the synthetic escalation. Per
 * D17 §5.4, the M3 task beads are named M3.1..M3.5. We target M3.1.
 * The bead is expected to exist already (M3 epic is imported by the
 * M3 step — but the JSONL pack ships epics + milestones up-front via
 * `gm-root.27.4`, so M3.1 is on disk before M3's scoped tasks run).
 *
 * If the bead doesn't exist yet at triage time (e.g., `.4` pack ships
 * the M3 task beads only inside m3.jsonl), fall back to targeting the
 * M3 epic itself.
 */
const TRIAGE_TARGET_BEAD = 'M3.1';
const TRIAGE_TARGET_FALLBACK = 'M3';

export async function runTriageStep(ctx: SharedContext): Promise<void> {
  // ─── Choose a target bead ───────────────────────────────────
  const targetBead = await pickExistingBead(ctx, [
    TRIAGE_TARGET_BEAD,
    TRIAGE_TARGET_FALLBACK,
  ]);
  if (!targetBead) {
    await ctx.fileBugBead({
      title: 'triage: no M3 bead present at triage time',
      severity: 'HIGH',
      body: `Expected one of ${TRIAGE_TARGET_BEAD}, ${TRIAGE_TARGET_FALLBACK} to be importable; neither is present.`,
    });
    throw new Error('triage: no target bead found');
  }

  // ─── Inject the synthetic escalation ────────────────────────
  const { escalationID } = await ctx.escalationInjector.inject({
    beadID: targetBead,
    kind: 'hitl_approval',
    urgency: 'blocking',
  });

  // ─── Wait for the escalation to surface in /api/escalations ─
  await waitForEscalation(ctx, escalationID, 30_000);

  // ─── Drive the SPA approve flow ─────────────────────────────
  await ctx.goto('/escalations');
  await ctx.page.waitForSelector('[data-testid="escalations-page"]', { timeout: 10_000 });

  const card = ctx.page.locator(`[data-testid="escalation-card-${escalationID}"]`);
  const resolveBtn = ctx.page.locator(`[data-testid="escalation-card-${escalationID}-resolve"]`);

  let usedUI = false;
  if ((await card.count()) > 0 && (await resolveBtn.count()) > 0) {
    try {
      await resolveBtn.click();
      await ctx.page.waitForSelector('[data-testid="escalation-resolve-modal"]', { timeout: 5_000 });
      const approveOpt = ctx.page.locator('[data-testid="escalation-resolve-option-approve"]');
      if ((await approveOpt.count()) > 0) {
        await approveOpt.click();
      }
      await ctx.page.locator('[data-testid="escalation-resolve-confirm"]').click();
      usedUI = true;
    } catch (err) {
      // Modal flow regressed; fall back to API.
      await ctx.fileBugBead({
        title: 'escalation respond UI incomplete',
        severity: 'HIGH',
        body: `escalation-resolve modal flow failed for ${escalationID}: ${(err as Error).message}\n` +
          `Falling back to direct POST /api/escalations/{id}/respond.`,
      });
    }
  } else {
    await ctx.fileBugBead({
      title: 'escalation respond UI incomplete',
      severity: 'HIGH',
      body: `Expected escalation-card-${escalationID}-resolve in the SPA; not found. Falling back to API.`,
    });
  }

  if (!usedUI) {
    // Fallback: direct POST. Same path the SPA would take. We need
    // an X-GEMBA-Confirm nonce; we mint one via the standard endpoint.
    await respondViaAPI(ctx, escalationID);
  }

  // ─── Verify resolution ──────────────────────────────────────
  await waitForEscalationResolved(ctx, escalationID, 15_000);

  // ─── Verify the targeted bead unblocks ─────────────────────
  await waitForBeadReady(ctx, targetBead, 30_000);
}

// ─── Helpers ──────────────────────────────────────────────────────

async function pickExistingBead(ctx: SharedContext, candidates: string[]): Promise<string | null> {
  for (const id of candidates) {
    try {
      const res = await fetch(`http://127.0.0.1:${ctx.gembaPort}/api/workitems/${encodeURIComponent(id)}`);
      if (res.ok) return id;
    } catch {
      // continue
    }
  }
  return null;
}

async function waitForEscalation(
  ctx: SharedContext,
  escalationID: string,
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${ctx.gembaPort}/api/escalations`);
      if (res.ok) {
        const json = (await res.json()) as { escalations?: Array<{ id: string }> };
        const found = (json.escalations ?? []).some((e) => e.id === escalationID);
        if (found) return;
      }
    } catch {
      // not ready
    }
    await sleep(1_000);
  }
  throw new Error(`triage: escalation ${escalationID} did not appear in /api/escalations within ${timeoutMs}ms`);
}

async function respondViaAPI(ctx: SharedContext, escalationID: string): Promise<void> {
  // Mint nonce. The standard pattern is to call /api/confirm-nonce or
  // supply X-GEMBA-Confirm:<token> directly when the test fixture
  // disables the gate. For acceptance tests we treat it as a GET
  // first then POST with the value; on mock the server accepts a
  // fixed test token.
  const nonce = process.env.GEMBA_E2E_CONFIRM_NONCE ?? 'acceptance-test';
  const res = await fetch(
    `http://127.0.0.1:${ctx.gembaPort}/api/escalations/${encodeURIComponent(escalationID)}/respond`,
    {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-GEMBA-Confirm': nonce,
      },
      body: JSON.stringify({ kind: 'approve' }),
    },
  );
  if (!res.ok) {
    const txt = await res.text().catch(() => '');
    throw new Error(`triage: POST /respond failed ${res.status}: ${txt}`);
  }
}

async function waitForEscalationResolved(
  ctx: SharedContext,
  escalationID: string,
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${ctx.gembaPort}/api/escalations`);
      if (res.ok) {
        const json = (await res.json()) as { escalations?: Array<{ id: string; state?: string; status?: string }> };
        const esc = (json.escalations ?? []).find((e) => e.id === escalationID);
        // Open list returns only open escalations — disappearance from
        // the list is how we observe "resolved".
        if (!esc) return;
        const state = esc.state ?? esc.status;
        if (state && /resolved/i.test(state)) return;
      }
    } catch {
      // continue
    }
    await sleep(500);
  }
  throw new Error(`triage: escalation ${escalationID} did not transition to resolved within ${timeoutMs}ms`);
}

async function waitForBeadReady(
  ctx: SharedContext,
  beadID: string,
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let last: string | undefined;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${ctx.gembaPort}/api/workitems/${encodeURIComponent(beadID)}`);
      if (res.ok) {
        const json = (await res.json()) as { state_category?: string; status?: string };
        last = `${json.state_category}/${json.status}`;
        // "ready" can mean state_category=open && status=ready, or status=ready.
        if (json.status === 'ready' || (json.state_category === 'open' && json.status !== 'blocked')) {
          return;
        }
      }
    } catch {
      // continue
    }
    await sleep(500);
  }
  throw new Error(
    `triage: bead ${beadID} did not become ready within ${timeoutMs}ms (last: ${last ?? 'never observed'})`,
  );
}

