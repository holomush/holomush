// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// wlc — Web Lifecycle — E2E test proving an owner can run the full scene
// lifecycle (create → pause → resume → end) from the web GUI with no telnet
// commands. DB state is asserted at each step.

import { test, expect, db, registerAndEnterTerminal } from './helpers/fixtures';

test.describe('Scene lifecycle via web GUI (wlc)', () => {
  test('owner runs the full scene lifecycle from the web GUI with no telnet', async ({ page }) => {
    await registerAndEnterTerminal(page, 'wlc');

    // Navigate to the /scenes workspace.
    await page.goto('/scenes');
    await expect(page.locator('[data-testid="scenes-workspace"]')).toBeVisible({ timeout: 15000 });

    // Open the create-scene sheet via the "New scene" button in the sidebar toolbar.
    await page.getByRole('button', { name: /new scene/i }).first().click();

    // The CreateSceneSheet slides in — wait for the title input to appear.
    const titleInput = page.locator('input[name="title"]');
    await expect(titleInput).toBeVisible({ timeout: 10000 });

    const sceneTitle = `WLC Lifecycle ${Date.now()}`;
    await titleInput.fill(sceneTitle);

    // Submit. The aria-label on the submit button is "Create scene".
    await page.getByRole('button', { name: /create scene/i }).click();

    // Wait for the scene to appear in the workspace list / center pane.
    // submitCreateScene auto-selects the new scene so its title shows in the center header.
    await expect(page.locator('.font-semibold').filter({ hasText: sceneTitle })).toBeVisible({
      timeout: 15000,
    });

    // DB: scene exists and is active.
    const scene = await db.getSceneByTitle(sceneTitle);
    expect(scene).not.toBeNull();
    expect(scene!.state).toBe('active');
    const sceneId = scene!.id;

    // The SceneContextRail shows lifecycle buttons for the owner when the scene
    // is selected. The scene was auto-selected by submitCreateScene, so the rail
    // should already have "Pause" and "End" visible (owner on active scene).
    await expect(page.getByRole('button', { name: /^Pause$/ })).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole('button', { name: /^End$/ })).toBeVisible({ timeout: 10000 });

    // ── Pause ──────────────────────────────────────────────────────────────
    await page.getByRole('button', { name: /^Pause$/ }).click();
    await expect
      .poll(async () => (await db.getSceneById(sceneId))?.state, { timeout: 15000 })
      .toBe('paused');

    // After pause: Resume and End are shown for the owner; Pause is hidden.
    await expect(page.getByRole('button', { name: /^Resume$/ })).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole('button', { name: /^End$/ })).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole('button', { name: /^Pause$/ })).not.toBeVisible({ timeout: 5000 });

    // ── Resume ─────────────────────────────────────────────────────────────
    await page.getByRole('button', { name: /^Resume$/ }).click();
    await expect
      .poll(async () => (await db.getSceneById(sceneId))?.state, { timeout: 15000 })
      .toBe('active');

    // After resume: Pause and End return for the owner.
    await expect(page.getByRole('button', { name: /^Pause$/ })).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole('button', { name: /^End$/ })).toBeVisible({ timeout: 10000 });

    // ── End ────────────────────────────────────────────────────────────────
    await page.getByRole('button', { name: /^End$/ }).click();
    await expect
      .poll(async () => (await db.getSceneById(sceneId))?.state, { timeout: 15000 })
      .toBe('ended');

    // After end: no lifecycle buttons remain (ended scenes have no actions).
    await expect(page.getByRole('button', { name: /^Pause$/ })).not.toBeVisible({ timeout: 5000 });
    await expect(page.getByRole('button', { name: /^Resume$/ })).not.toBeVisible({ timeout: 5000 });
    await expect(page.getByRole('button', { name: /^End$/ })).not.toBeVisible({ timeout: 5000 });
  });
});
