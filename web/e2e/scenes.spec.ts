// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect, db, getClientSessionId } from './helpers/fixtures';
import type { Page } from '@playwright/test';

/**
 * Connect as guest via the landing page and wait for the terminal to load.
 * Same pattern as web/e2e/terminal.spec.ts.
 */
async function connectAsGuest(page: Page) {
  await page.goto('/');
  await page.getByRole('main').getByRole('button', { name: 'Try as Guest' }).click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
}

/**
 * Send a command via the terminal textarea. The caller is responsible for
 * waiting for any specific output.
 */
async function sendCommand(page: Page, command: string) {
  const input = page.locator('textarea');
  await input.fill(command);
  await input.press('Enter');
}

/**
 * Snapshot the current terminal event count. Used as a baseline before
 * sending a new command so subsequent waits only inspect events appended
 * after the command was sent. Without this, a transcript-style UI that
 * preserves prior events can produce false-positive matches on earlier
 * output.
 */
async function currentEventCount(page: Page): Promise<number> {
  return page.locator('[data-testid="event"]').count();
}

/**
 * Wait for a terminal event whose text matches `pattern` to appear at or
 * after `sinceIndex`, and return the captured substring (the regex match,
 * not the whole event text).
 *
 * Callers MUST capture `sinceIndex` via currentEventCount BEFORE sending
 * the command whose output they want to match. This isolates the wait
 * from any preceding events that may coincidentally match the pattern.
 */
async function waitForOutputMatching(
  page: Page,
  pattern: RegExp,
  sinceIndex: number,
): Promise<string> {
  const events = page.locator('[data-testid="event"]');

  // Poll the locator until some event at index >= sinceIndex matches the
  // pattern. Using expect.poll lets Playwright's auto-waiting semantics
  // handle retry and timeout.
  let captured = '';
  await expect
    .poll(
      async () => {
        const count = await events.count();
        for (let i = sinceIndex; i < count; i++) {
          const text = (await events.nth(i).textContent()) ?? '';
          const match = text.match(pattern);
          if (match && match[0]) {
            captured = match[0];
            return true;
          }
        }
        return false;
      },
      { timeout: 10000 },
    )
    .toBe(true);

  if (!captured) {
    throw new Error(`poll passed but no value captured for pattern ${pattern}`);
  }
  return captured;
}

/**
 * Extract the scene ID from a "Scene created: <ULID>" terminal event.
 * Scene IDs are bare 26-char Crockford base32 ULIDs — holomush-y5inx eliminated
 * the legacy `scene-` prefix. The output line is `Scene created: <ULID>`; the
 * ULID is the first 26-char uppercase-alnum token in events after the command.
 */
async function extractSceneIdFromOutput(page: Page, sinceIndex: number): Promise<string> {
  return waitForOutputMatching(page, /[0-9A-Z]{26}/, sinceIndex);
}

test.describe('Scene lifecycle (Phase 2)', () => {
  test('create -> pause -> resume -> end with DB verification', async ({ page }) => {
    await connectAsGuest(page);
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();
    const session = await db.getSessionById(sessionId!);
    expect(session).not.toBeNull();

    // Create a scene through the terminal. Capture the event count before
    // sending so the wait only inspects events appended after the command.
    let before = await currentEventCount(page);
    await sendCommand(page, 'scene create Phase 2 Lifecycle Test');
    const sceneId = await extractSceneIdFromOutput(page, before);
    expect(sceneId).toMatch(/^[0-9A-Z]{26}$/);

    // DB: scene exists with state='active', owner = current character
    let scene = await db.getSceneById(sceneId);
    expect(scene).not.toBeNull();
    expect(scene!.state).toBe('active');
    expect(scene!.owner_id).toBe(session!.character_id);
    expect(scene!.title).toBe('Phase 2 Lifecycle Test');
    expect(scene!.ended_at).toBeNull();

    // Pause
    before = await currentEventCount(page);
    await sendCommand(page, `scene pause ${sceneId}`);
    await waitForOutputMatching(page, /paused/, before);
    scene = await db.getSceneById(sceneId);
    expect(scene!.state).toBe('paused');

    // Resume
    before = await currentEventCount(page);
    await sendCommand(page, `scene resume ${sceneId}`);
    await waitForOutputMatching(page, /resumed/, before);
    scene = await db.getSceneById(sceneId);
    expect(scene!.state).toBe('active');

    // End
    before = await currentEventCount(page);
    await sendCommand(page, `scene end ${sceneId}`);
    await waitForOutputMatching(page, /ended/, before);
    scene = await db.getSceneById(sceneId);
    expect(scene!.state).toBe('ended');
    expect(scene!.ended_at).not.toBeNull();
  });

  test('scene set updates the title', async ({ page }) => {
    await connectAsGuest(page);

    let before = await currentEventCount(page);
    await sendCommand(page, 'scene create Original Title');
    const sceneId = await extractSceneIdFromOutput(page, before);

    let scene = await db.getSceneById(sceneId);
    expect(scene!.title).toBe('Original Title');

    before = await currentEventCount(page);
    await sendCommand(page, `scene set ${sceneId} title=Renamed Title`);
    await waitForOutputMatching(page, /updated/, before);

    scene = await db.getSceneById(sceneId);
    expect(scene!.title).toBe('Renamed Title');
  });

  test('cannot end an already-ended scene', async ({ page }) => {
    await connectAsGuest(page);

    let before = await currentEventCount(page);
    await sendCommand(page, 'scene create Will End Twice');
    const sceneId = await extractSceneIdFromOutput(page, before);

    before = await currentEventCount(page);
    await sendCommand(page, `scene end ${sceneId}`);
    await waitForOutputMatching(page, /ended/, before);

    // Second end attempt should produce an error event
    before = await currentEventCount(page);
    await sendCommand(page, `scene end ${sceneId}`);
    await waitForOutputMatching(page, /Failed to end scene/, before);
  });

  test('scene info shows scene metadata', async ({ page }) => {
    await connectAsGuest(page);

    let before = await currentEventCount(page);
    await sendCommand(page, 'scene create Info Test Scene');
    const sceneId = await extractSceneIdFromOutput(page, before);

    before = await currentEventCount(page);
    await sendCommand(page, `scene info ${sceneId}`);
    // Both assertions look at events appended after the `scene info` command.
    await waitForOutputMatching(page, /Info Test Scene/, before);
    await waitForOutputMatching(page, /State: active/, before);
  });
});

test.describe('Scene focus routing (Phase 5, holomush-dble7)', () => {
  // Reproduction for holomush-dble7: `scene focus`/`scene grid` are the only
  // commands that hard-require req.ConnectionID (the per-stream connection_id
  // the web client captures from the STREAM_OPENED ControlFrame and echoes on
  // SendCommand). If the live stream is up but the command carries an empty
  // connection_id, the plugin rejects with "requires a live connection"
  // (plugins/core-scenes/commands.go:1146 for focus, :1092 for grid). A guest
  // is auto-joined as the owner of the scene it creates, so membership is
  // satisfied and the ONLY thing that can produce that message is an empty
  // connection_id reaching the handler.
  test('scene focus on a joined scene succeeds (does not report no live connection)', async ({
    page,
  }) => {
    await connectAsGuest(page);

    let before = await currentEventCount(page);
    await sendCommand(page, 'scene create Focus Routing Test');
    const sceneId = await extractSceneIdFromOutput(page, before);
    expect(sceneId).toMatch(/^[0-9A-Z]{26}$/);

    // Focus-substrate membership is established by `scene join` (JoinFocus),
    // not by `scene create` (DB row only) — so join before focusing, which is
    // the real user flow. Both `scene join` and `scene focus` now accept the
    // `#`-prefixed display form interchangeably with a bare ULID (holomush-ehbnk);
    // joining with the `#` form here used to yield "scene not found: #<id>" and
    // is now a regression guard for that parity fix.
    before = await currentEventCount(page);
    await sendCommand(page, `scene join #${sceneId}`);
    await waitForOutputMatching(page, /Joined scene/, before);

    before = await currentEventCount(page);
    await sendCommand(page, `scene focus #${sceneId}`);
    // The dble7 bug surfaced here: an empty connection_id yielded
    // "`scene focus` requires a live connection." instead of the success line.
    await waitForOutputMatching(
      page,
      new RegExp(`now focused on Scene ${sceneId}`),
      before,
    );
  });

  test('scene grid succeeds (does not report no live connection)', async ({ page }) => {
    await connectAsGuest(page);

    // No scene needed — `scene grid` only requires a live per-connection id.
    const before = await currentEventCount(page);
    await sendCommand(page, 'scene grid');
    await waitForOutputMatching(page, /Focused on the grid\./, before);
  });
});
