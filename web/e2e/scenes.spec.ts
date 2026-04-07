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
 * Wait for the most recent terminal event whose text matches `pattern` and
 * return the captured substring (the regex match, not the whole event text).
 */
async function waitForOutputMatching(page: Page, pattern: RegExp): Promise<string> {
  const event = page
    .locator('[data-testid="event"]')
    .filter({ hasText: pattern })
    .last();
  await expect(event).toBeVisible({ timeout: 10000 });
  const text = await event.textContent();
  if (!text) {
    throw new Error(`event matched ${pattern} but had no text`);
  }
  const match = text.match(pattern);
  if (!match || !match[0]) {
    throw new Error(`pattern ${pattern} matched event but extracted no value`);
  }
  return match[0];
}

/**
 * Extract the scene ID from a "Scene created: scene-XXXXX" terminal event.
 * Scene IDs are `scene-` plus a 26-char Crockford base32 ULID.
 */
async function extractSceneIdFromOutput(page: Page): Promise<string> {
  const text = await waitForOutputMatching(page, /scene-[A-Z0-9]+/);
  return text;
}

test.describe('Scene lifecycle (Phase 2)', () => {
  test('create -> pause -> resume -> end with DB verification', async ({ page }) => {
    await connectAsGuest(page);
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();
    const session = await db.getSessionById(sessionId!);
    expect(session).not.toBeNull();

    // Create a scene through the terminal
    await sendCommand(page, 'scene create Phase 2 Lifecycle Test');
    const sceneId = await extractSceneIdFromOutput(page);
    expect(sceneId).toMatch(/^scene-[A-Z0-9]+$/);

    // DB: scene exists with state='active', owner = current character
    let scene = await db.getSceneById(sceneId);
    expect(scene).not.toBeNull();
    expect(scene!.state).toBe('active');
    expect(scene!.owner_id).toBe(session!.character_id);
    expect(scene!.title).toBe('Phase 2 Lifecycle Test');
    expect(scene!.ended_at).toBeNull();

    // Pause
    await sendCommand(page, `scene pause ${sceneId}`);
    await waitForOutputMatching(page, /paused/);
    scene = await db.getSceneById(sceneId);
    expect(scene!.state).toBe('paused');

    // Resume
    await sendCommand(page, `scene resume ${sceneId}`);
    await waitForOutputMatching(page, /resumed/);
    scene = await db.getSceneById(sceneId);
    expect(scene!.state).toBe('active');

    // End
    await sendCommand(page, `scene end ${sceneId}`);
    await waitForOutputMatching(page, /ended/);
    scene = await db.getSceneById(sceneId);
    expect(scene!.state).toBe('ended');
    expect(scene!.ended_at).not.toBeNull();
  });

  test('scene set updates the title', async ({ page }) => {
    await connectAsGuest(page);

    await sendCommand(page, 'scene create Original Title');
    const sceneId = await extractSceneIdFromOutput(page);

    let scene = await db.getSceneById(sceneId);
    expect(scene!.title).toBe('Original Title');

    await sendCommand(page, `scene set ${sceneId} title=Renamed Title`);
    await waitForOutputMatching(page, /updated/);

    scene = await db.getSceneById(sceneId);
    expect(scene!.title).toBe('Renamed Title');
  });

  test('cannot end an already-ended scene', async ({ page }) => {
    await connectAsGuest(page);

    await sendCommand(page, 'scene create Will End Twice');
    const sceneId = await extractSceneIdFromOutput(page);

    await sendCommand(page, `scene end ${sceneId}`);
    await waitForOutputMatching(page, /ended/);

    // Second end attempt should produce an error event
    await sendCommand(page, `scene end ${sceneId}`);
    await waitForOutputMatching(page, /Failed to end scene/);
  });

  test('scene info shows scene metadata', async ({ page }) => {
    await connectAsGuest(page);

    await sendCommand(page, 'scene create Info Test Scene');
    const sceneId = await extractSceneIdFromOutput(page);

    await sendCommand(page, `scene info ${sceneId}`);
    await waitForOutputMatching(page, /Info Test Scene/);
    await waitForOutputMatching(page, /State: active/);
  });
});
