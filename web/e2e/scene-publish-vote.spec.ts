// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// pv — Publish Vote — E2E test proving the scene publish-vote live tally
// delivery path via the web GUI with no telnet commands. A second browser
// context acts as a non-participant observer who watches the scene (browse
// board → watch, mirroring scenes-membership.spec.ts) and must never see
// numeric vote counts, only a binary "in progress" badge — participants get
// the full yes/no/pending tally, live, via scene_publish_* event dispatch
// (ADR holomush-uqrr7: refetch-on-event, not a client-side tally reducer).

import { type BrowserContext } from '@playwright/test';
import { test, expect, db, registerAndEnterTerminal } from './helpers/fixtures';

test.describe('Scene publish-vote via web GUI (pv)', () => {
  test('participant sees live tally; observer sees only "in progress", never counts', async ({
    page,
    browser,
  }) => {
    // Multi-step sequential waits: two registrations, scene create, end,
    // start-publish, watch, vote cast, DB polls. Ceiling explicitly so a slow
    // CI runner doesn't kill mid-chain with a generic timeout.
    test.setTimeout(150000);

    // ── Owner setup ────────────────────────────────────────────────────────
    await registerAndEnterTerminal(page, 'pv');

    // ── Observer setup (separate browser context = separate cookie jar) ─────
    const observerCtx: BrowserContext = await browser.newContext();
    try {
      const page2 = await observerCtx.newPage();
      await registerAndEnterTerminal(page2, 'pv');

      // ── Owner creates scene ──────────────────────────────────────────────
      await page.goto('/scenes');
      await expect(page.getByTestId('scenes-workspace')).toBeVisible({ timeout: 15000 });
      await page.getByRole('button', { name: /new scene/i }).first().click();
      const titleInput = page.locator('input[name="title"]');
      await expect(titleInput).toBeVisible({ timeout: 10000 });
      const sceneTitle = `PvTest ${Date.now()}`;
      await titleInput.fill(sceneTitle);
      await page.getByRole('button', { name: /create scene/i }).click();
      await expect(page.locator('.font-semibold').filter({ hasText: sceneTitle })).toBeVisible({
        timeout: 15000,
      });

      const scene = await db.getSceneByTitle(sceneTitle);
      expect(scene, 'scene should exist in DB after creation').not.toBeNull();
      const sceneId = scene!.id;

      // ── Observer watches the scene while it's still active/open ─────────
      // (Task 7's plan scaffold suggested seeding via a DB helper, but
      // web/e2e/helpers/db.ts has no publish seeder — the suite is strictly
      // UI-driven, so we reuse the browse→watch flow from scenes-membership.)
      await page2.goto('/scenes/browse');
      await expect(page2.getByRole('list', { name: 'Scene list' })).toBeVisible({ timeout: 10000 });
      const watchBtn = page2.getByRole('button', { name: `Watch scene ${sceneTitle}` });
      await expect(watchBtn).toBeVisible({ timeout: 10000 });
      await watchBtn.click();
      // Mirrors scenes-membership.spec.ts: wait for the watch navigation to
      // settle, then force a full navigation so the layout remounts and picks
      // up the new observer row (refresh() runs again on mount).
      await page2.waitForURL(/\/scenes$/, { timeout: 15000 });
      await page2.goto(`/scenes?watch=${sceneId}`);
      await expect(page2.getByTestId('scenes-workspace')).toBeVisible({ timeout: 15000 });

      const observerPanel = page2.getByTestId('scene-publish-panel');

      // ── Owner ends the scene, then starts a publication vote ─────────────
      // (showStartPublish only renders once scene.state === 'ended'.)
      await page.getByRole('button', { name: /^End$/ }).click();
      await expect
        .poll(async () => (await db.getSceneById(sceneId))?.state, { timeout: 15000 })
        .toBe('ended');

      await page.getByRole('button', { name: 'Start publish vote' }).click();

      const ownerPanel = page.getByTestId('scene-publish-panel');
      await expect(ownerPanel).toBeVisible({ timeout: 15000 });
      await expect(ownerPanel.locator('.tally li').filter({ hasText: 'Pending' })).toBeVisible({
        timeout: 15000,
      });

      // ── Observer: binary "in progress" only, never numeric counts ────────
      await expect(observerPanel).toBeVisible({ timeout: 15000 });
      await expect(observerPanel.getByText(/publication vote in progress/i)).toBeVisible({
        timeout: 15000,
      });
      await expect(observerPanel.locator('.tally')).toHaveCount(0);

      // ── Owner casts a Yes vote; tally updates live, no manual reload ─────
      await ownerPanel.getByRole('button', { name: 'Yes', exact: true }).click();
      await expect(ownerPanel.locator('.tally li').filter({ hasText: 'Yes' })).toContainText('1', {
        timeout: 15000,
      });
      await expect(ownerPanel.locator('.tally li').filter({ hasText: 'Pending' })).toContainText('0');

      // ── Observer still sees no counts after the vote_cast event ──────────
      // (publishStore.onEvent ignores VOTE_CAST for non-participants.)
      await expect(observerPanel.getByText(/publication vote in progress/i)).toBeVisible();
      await expect(observerPanel.locator('.tally')).toHaveCount(0);
    } finally {
      await observerCtx.close();
    }
  });

  test('participant reload mid-vote resyncs the tally from cold-start', async ({ page }) => {
    test.setTimeout(90000);

    await registerAndEnterTerminal(page, 'pvr');

    await page.goto('/scenes');
    await expect(page.getByTestId('scenes-workspace')).toBeVisible({ timeout: 15000 });
    await page.getByRole('button', { name: /new scene/i }).first().click();
    const titleInput = page.locator('input[name="title"]');
    await expect(titleInput).toBeVisible({ timeout: 10000 });
    const sceneTitle = `PvReload ${Date.now()}`;
    await titleInput.fill(sceneTitle);
    await page.getByRole('button', { name: /create scene/i }).click();
    await expect(page.locator('.font-semibold').filter({ hasText: sceneTitle })).toBeVisible({
      timeout: 15000,
    });

    const scene = await db.getSceneByTitle(sceneTitle);
    expect(scene).not.toBeNull();
    const sceneId = scene!.id;

    await page.getByRole('button', { name: /^End$/ }).click();
    await expect
      .poll(async () => (await db.getSceneById(sceneId))?.state, { timeout: 15000 })
      .toBe('ended');

    await page.getByRole('button', { name: 'Start publish vote' }).click();
    const panel = page.getByTestId('scene-publish-panel');
    await expect(panel).toBeVisible({ timeout: 15000 });
    await panel.getByRole('button', { name: 'Yes', exact: true }).click();
    await expect(panel.locator('.tally li').filter({ hasText: 'Yes' })).toContainText('1', {
      timeout: 15000,
    });

    // ── Reload: the workspace's in-memory selection is not URL-persisted
    // (ScenesShell strips ?watch=/?join= via replaceState after consuming
    // it), so a hard reload lands on an unselected workspace. Re-select via
    // the sidebar — same action a real user takes — to prove the panel
    // repopulates from cold-start (GetScene + GetPublishedScene), not from a
    // missed live event.
    await page.reload();
    await expect(page.getByTestId('scenes-workspace')).toBeVisible({ timeout: 15000 });
    await page
      .getByRole('button', { name: new RegExp(`scene ${sceneTitle}`) })
      .first()
      .click();

    const reloadedPanel = page.getByTestId('scene-publish-panel');
    await expect(reloadedPanel).toBeVisible({ timeout: 15000 });
    await expect(reloadedPanel.locator('.tally li').filter({ hasText: 'Yes' })).toContainText('1', {
      timeout: 15000,
    });
    await expect(reloadedPanel.locator('.tally li').filter({ hasText: 'Pending' })).toContainText('0');
  });
});
