// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// wst — Web Settings — E2E test proving an owner can edit a scene's visibility
// entirely through the web ⚙ Settings sheet with no telnet commands. The DB row
// is asserted before and after the change.

import { test, expect, db, registerAndEnterTerminal } from './helpers/fixtures';

test.describe('Scene settings via web GUI (wst)', () => {
  test('owner edits scene visibility from the web GUI with no telnet command', async ({ page }) => {
    await registerAndEnterTerminal(page, 'wst');

    // Navigate to the /scenes workspace.
    await page.goto('/scenes');
    await expect(page.locator('[data-testid="scenes-workspace"]')).toBeVisible({ timeout: 15000 });

    // Open the create-scene sheet via the "New scene" button in the sidebar toolbar.
    await page.getByRole('button', { name: /new scene/i }).first().click();

    // The CreateSceneSheet slides in — wait for the title input to appear.
    const titleInput = page.locator('input[name="title"]');
    await expect(titleInput).toBeVisible({ timeout: 10000 });

    const sceneTitle = `WST Settings ${Date.now()}`;
    await titleInput.fill(sceneTitle);

    // Submit. The aria-label on the submit button is "Create scene".
    await page.getByRole('button', { name: /create scene/i }).click();

    // submitCreateScene auto-selects the new scene so its title shows in the center header.
    await expect(page.locator('.font-semibold').filter({ hasText: sceneTitle })).toBeVisible({
      timeout: 15000,
    });

    // DB: scene exists and starts with open visibility.
    const scene = await db.getSceneByTitle(sceneTitle);
    expect(scene).not.toBeNull();
    expect(scene!.visibility).toBe('open');
    const sceneId = scene!.id;

    // ── Edit visibility via the ⚙ Settings sheet ────────────────────────────
    // The SceneContextRail shows the ⚙ Settings trigger for the owner of a
    // selected, manageable scene (aria-label "Scene settings").
    await page.getByRole('button', { name: /scene settings/i }).click();

    // The settings form loads asynchronously; wait for the visibility select.
    const visibilitySelect = page.locator('select[name="settings-visibility"]');
    await expect(visibilitySelect).toBeVisible({ timeout: 10000 });

    // Flip Open → Private. This makes the form dirty, enabling the Save button.
    await visibilitySelect.selectOption('private');

    // Save. The submit button carries aria-label "Save settings".
    await page.getByRole('button', { name: /save settings/i }).click();

    // DB reflects the change made entirely through the web GUI.
    await expect
      .poll(async () => (await db.getSceneById(sceneId))?.visibility, { timeout: 15000 })
      .toBe('private');
  });
});
