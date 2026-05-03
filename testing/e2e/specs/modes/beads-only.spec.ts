import { expect, test } from "../../fixtures/server";
import { epic, parentChild, workItem } from "../../builders/workitem";
import { workPlaneManifest } from "../../fixtures/capabilitiesPlane";
import { BoardPage } from "../../pages/BoardPage";

test.describe("@modes beads-only", () => {
  test("hides orchestration surfaces and presents Cascade plus Flat bead views", async ({
    page,
    workPlane,
    capabilitiesPlane,
  }) => {
    capabilitiesPlane.set({
      runtime_mode: "beads_only",
      beads_only: true,
      beads_source: {
        kind: "beads-dir",
        label: "fake-beads",
        detail: "/tmp/fake",
      },
      beads_history_path: "/tmp/fake/.gemba/session-manifest.jsonl",
      work_plane: workPlaneManifest(),
      orchestration_plane: null,
    });

    const milestone = workItem({
      id: "gm-m1",
      kind: "milestone",
      title: "M1: Beads-only planning",
      state_category: "unstarted",
    });
    const seeded = epic({
      id: "gm-beads-only-epic",
      title: "Shape the lightweight planning flow",
      state_category: "unstarted",
      relationships: [parentChild(milestone.id, "gm-beads-only-epic")],
    });
    const bead = workItem({
      id: "gm-beads-only-task",
      title: "Check the portable manifest",
      state_category: "unstarted",
      relationships: [parentChild(seeded.id, "gm-beads-only-task")],
    });
    workPlane.seed([bead, seeded, milestone]);

    const sessionPosts: string[] = [];
    const cascadePosts: string[] = [];
    await page.route("**/api/sessions", async (route) => {
      if (route.request().method() === "POST")
        sessionPosts.push(route.request().postData() ?? "");
      await route.fallback();
    });
    await page.route("**/api/work-items/*/cascade-dispatch", async (route) => {
      if (route.request().method() === "POST")
        cascadePosts.push(route.request().postData() ?? "");
      await route.fallback();
    });

    const board = new BoardPage(page);
    await board.gotoEpicView();

    await expect(page.getByTestId("sidebar-item-board")).toBeVisible();
    await expect(page.getByTestId("sidebar-item-refine")).toBeVisible();
    await expect(page.getByTestId("sidebar-item-settings")).toBeVisible();
    await expect(page.getByTestId("sidebar-item-sessions")).toHaveCount(0);
    await expect(page.getByTestId("sidebar-item-walk")).toHaveCount(0);
    await expect(page.getByTestId("sidebar-item-escalations")).toHaveCount(0);

    await expect(page.getByTestId("board-list")).toBeVisible();
    await expect(page.getByTestId("view-toggle-list")).toHaveAttribute(
      "data-active",
      "true",
    );
    await expect(page.getByTestId("view-toggle-epic")).toHaveCount(0);
    await expect(page.getByTestId("view-toggle-workitem")).toHaveCount(0);
    await expect(page.getByTestId("board-list-kind-milestone")).toBeVisible();

    await page.getByTestId("view-toggle-cascade").click();
    await expect(page.getByTestId("beads-cascade")).toBeVisible();
    await expect(
      page.getByTestId(`beads-cascade-row-${milestone.id}`),
    ).toBeVisible();
    await expect(
      page.getByTestId(`beads-cascade-row-${seeded.id}`),
    ).toBeVisible();
    await expect(
      page.getByTestId(`beads-cascade-row-${bead.id}`),
    ).toBeVisible();

    await page.evaluate(async (id) => {
      const res = await fetch(`/api/work-items/${encodeURIComponent(id)}`, {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
          "X-GEMBA-Confirm": `e2e-${Date.now()}`,
        },
        body: JSON.stringify({ state_category: "started" }),
      });
      if (!res.ok) throw new Error(await res.text());
    }, bead.id);

    await expect
      .poll(() => workPlane.history().length, { timeout: 5_000 })
      .toBeGreaterThan(0);
    expect(sessionPosts).toEqual([]);
    expect(cascadePosts).toEqual([]);

    await page.getByLabel("Beads history").click();
    await expect(page.getByTestId("rhp-beads-history-body")).toBeVisible();
    await expect(
      page.getByTestId("beads-history-entry").filter({ hasText: "Moved" }),
    ).toBeVisible();

    await page.goto("/sessions");
    await expect(page.getByTestId("beads-only-unavailable")).toBeVisible();
  });
});
