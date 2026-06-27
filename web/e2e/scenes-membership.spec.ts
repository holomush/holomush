// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// mbs — Membership — E2E test proving the complete watch→join→kick membership
// lifecycle via the web GUI with no telnet commands. A second browser context
// acts as a separate player (watcher) who:
//   1. watches an open scene via the browse board (WebWatchScene → observer),
//   2. joins it via the SceneComposer "Join scene" CTA (scene join command → member),
// while the owner then kicks the new member via the context rail's "Manage" kebab.
// DB state is verified at each step via getParticipantsBySceneId.

import { type BrowserContext } from '@playwright/test';
import { test, expect, db, registerAndEnterTerminal, getClientCharacterName } from './helpers/fixtures';

test.describe('Scene membership via web GUI (mbs)', () => {
  test('owner adds member via watch→join and removes via kick with no telnet', async ({
    page,
    browser,
  }) => {
    // Multi-step sequential waits: registerAndEnterTerminal (×2), scene create,
    // watch navigate, join command, DB polls, kick. Ceiling the test explicitly
    // so a slow CI runner doesn't kill mid-chain with a generic timeout.
    test.setTimeout(150000);

    // ── Owner setup ────────────────────────────────────────────────────────
    await registerAndEnterTerminal(page, 'mbs');

    // ── Watcher setup (separate browser context = separate cookie jar) ─────
    const watcherCtx: BrowserContext = await browser.newContext();
    try {
      const page2 = await watcherCtx.newPage();
      await registerAndEnterTerminal(page2, 'mbs');
      // The server normalizes character names (capitalizes the first letter of
      // each word, lowercases the rest). Read the stored display name from
      // sessionStorage rather than using the raw input so UI selectors
      // ("Join scene as {name}", "Manage {name}") match what the server wrote.
      const watcherName = await getClientCharacterName(page2);
      expect(watcherName, 'sessionStorage should have characterName after terminal login').not.toBeNull();

      // ── Owner creates scene ──────────────────────────────────────────────
      await page.goto('/scenes');
      await expect(page.locator('[data-testid="scenes-workspace"]')).toBeVisible({
        timeout: 15000,
      });

      await page.getByRole('button', { name: /new scene/i }).first().click();
      const titleInput = page.locator('input[name="title"]');
      await expect(titleInput).toBeVisible({ timeout: 10000 });
      const sceneTitle = `MbsTest ${Date.now()}`;
      await titleInput.fill(sceneTitle);
      await page.getByRole('button', { name: /create scene/i }).click();

      // submitCreateScene auto-selects the new scene so its title appears in
      // the center column header (a .font-semibold element).
      await expect(page.locator('.font-semibold').filter({ hasText: sceneTitle })).toBeVisible({
        timeout: 15000,
      });

      // DB: scene created with open visibility and active state (server default).
      const scene = await db.getSceneByTitle(sceneTitle);
      expect(scene, 'scene should exist in DB after creation').not.toBeNull();
      expect(scene!.state).toBe('active');
      expect(scene!.visibility).toBe('open');
      const sceneId = scene!.id;

      // DB: one participant row exists with role 'owner'.
      // Use role-only assertions to avoid coupling to the exact character-name
      // stored in the DB (the server may trim or normalize the name on write).
      await expect
        .poll(
          async () => {
            const ps = await db.getParticipantsBySceneId(sceneId);
            return ps.map((p) => p.role).sort();
          },
          { timeout: 10000, message: 'owner should be the sole participant' },
        )
        .toEqual(['owner']);

      // ── Watcher browses board and watches scene ──────────────────────────
      await page2.goto('/scenes/browse');
      // The browse board renders a <ul aria-label="Scene list"> of SceneBoardRow
      // articles. Wait for the list to be present before scanning for the button.
      await expect(page2.getByRole('list', { name: 'Scene list' })).toBeVisible({
        timeout: 10000,
      });

      // SceneBoardRow renders aria-label="Watch scene {title}" for open scenes.
      const watchBtn = page2.getByRole('button', { name: `Watch scene ${sceneTitle}` });
      await expect(watchBtn).toBeVisible({ timeout: 10000 });
      await watchBtn.click();

      // handleWatch() calls watchScene() (adds observer row to DB) then
      // goto('/scenes?watch=<id>'). ScenesShell's $effect fires but myScenes is
      // stale — the layout never remounted, so refresh() hasn't seen the new row.
      // selectedScene stays null; SceneComposer doesn't render.
      //
      // Fix: wait for the URL to settle at /scenes (confirming watchScene() has
      // completed), then force a full-page navigation to /scenes?watch=<sceneId>.
      // This remounts the scenes layout, triggering onMount → refresh() which now
      // includes the observer row. The $effect (gated on refreshed=true) then
      // selects the scene and SceneComposer renders the observer "Join scene" CTA.
      await page2.waitForURL(/\/scenes$/, { timeout: 15000 });
      await page2.goto(`/scenes?watch=${sceneId}`);
      await expect(page2.locator('[data-testid="scenes-workspace"]')).toBeVisible({
        timeout: 15000,
      });

      // ── Watcher joins scene via SceneComposer CTA ────────────────────────
      // SceneComposer renders aria-label="Join scene as {charName}" when
      // scene.role === 'observer'. The button sends `scene join #<id>` via the
      // web API (SendCommand RPC), promoting the watcher to member.
      const joinBtn = page2.getByRole('button', { name: `Join scene as ${watcherName!}` });
      await expect(joinBtn).toBeVisible({ timeout: 15000 });
      await joinBtn.click();

      // ── DB: watcher is now a member ───────────────────────────────────────
      // Roles present should be exactly ['member', 'owner'] once join completes.
      await expect
        .poll(
          async () => {
            const ps = await db.getParticipantsBySceneId(sceneId);
            return ps.map((p) => p.role).sort();
          },
          { timeout: 20000, message: 'watcher should become member' },
        )
        .toEqual(['member', 'owner']);

      // ── Owner refreshes roster ────────────────────────────────────────────
      // Re-clicking the scene in the sidebar calls handleSceneSelect →
      // workspaceStore.select() → getScene(), which re-populates scene.participants
      // with the now-joined watcher.
      await page.bringToFront();
      // SceneListItem aria-label: "scene {title}, as {charName}"
      await page
        .getByRole('button', { name: new RegExp(`scene ${sceneTitle}`) })
        .first()
        .click();

      // SceneContextRail's Participants section should show the watcher's name.
      await expect(page.getByLabel('Scene context', { exact: true }).getByText(watcherName!, { exact: true })).toBeVisible({ timeout: 15000 });

      // ── Owner kicks watcher via "Manage" kebab ────────────────────────────
      // The "Manage {name}" button opens a DropdownMenu.  "Kick" is a
      // DropdownMenu.Item rendered as role="menuitem".
      await page.getByRole('button', { name: `Manage ${watcherName!}` }).click();
      await page.getByRole('menuitem', { name: /^Kick$/ }).click();

      // ── DB: watcher participant row removed ───────────────────────────────
      // Only the owner should remain.
      await expect
        .poll(
          async () => {
            const ps = await db.getParticipantsBySceneId(sceneId);
            return ps.map((p) => p.role).sort();
          },
          { timeout: 20000, message: 'only owner should remain after kick' },
        )
        .toEqual(['owner']);

      // kickAction calls refetch() → workspaceStore.select() after the RPC, so
      // the roster should update without a manual re-click.
      await expect(page.getByLabel('Scene context', { exact: true }).getByText(watcherName!, { exact: true })).not.toBeVisible({
        timeout: 10000,
      });
    } finally {
      await watcherCtx.close();
    }
  });
});
