// specs/drawers/new-workitem-dialog.spec.ts — gm-5v8v.8
//
// Covers the open / cancel / parent-prefill paths through
// NewWorkItemDialog (web/src/components/board/NewWorkItemDialog.tsx).
// The component lacks data-testids so we drive it by ARIA role + name.
// Submission verification (POST /api/work-items round-trip + bd
// create) is tagged @deep — it lives behind gm-5v8v.2.

import { test, expect } from '../../fixtures/server';
import { EpicDrawerPO } from '../../pages/EpicDrawer';
import * as build from '../../builders/workitem';

test.describe('NewWorkItemDialog @route', () => {
  test('opens from EpicDrawer "+ New child" with parent prefilled', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-newchild';
    workPlane.seed([build.epic({ id: epicId, title: 'Parent epic' })]);

    const drawer = new EpicDrawerPO(page);
    await drawer.openByDeepLink(epicId);
    await drawer.newChild.click();

    const dialog = page.getByRole('dialog', { name: 'New work item' });
    await expect(dialog).toBeVisible();
    // Parent block shows the locked id + title.
    await expect(dialog).toContainText(epicId);
    await expect(dialog).toContainText('Parent epic');
  });

  test('Create button is disabled until title is filled', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-validation';
    workPlane.seed([build.epic({ id: epicId })]);

    const drawer = new EpicDrawerPO(page);
    await drawer.openByDeepLink(epicId);
    await drawer.newChild.click();

    const dialog = page.getByRole('dialog', { name: 'New work item' });
    const create = dialog.getByRole('button', { name: 'Create' });
    await expect(create).toBeDisabled();

    await dialog.getByPlaceholder('What needs doing?').fill('A new task');
    await expect(create).toBeEnabled();
  });

  test('Cancel button closes the dialog', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-cancel';
    workPlane.seed([build.epic({ id: epicId })]);

    const drawer = new EpicDrawerPO(page);
    await drawer.openByDeepLink(epicId);
    await drawer.newChild.click();

    const dialog = page.getByRole('dialog', { name: 'New work item' });
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toBeHidden();
  });

  test.fixme('POST /api/work-items + bd creates the item @deep', async () => {
    // gm-5v8v.2 lands the deep-mode backend; this verifies the
    // adaptor materialised the item and returned a real id.
  });
});
