// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect, db } from './helpers/fixtures';
import type { Page } from '@playwright/test';

/**
 * Connect as guest via the landing page and wait for the terminal.
 */
async function connectAsGuest(page: Page) {
  await page.goto('/');
  await page.getByRole('main').getByRole('button', { name: 'Try as Guest' }).click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
}

/**
 * Send a command via the terminal input and wait for a response containing
 * the expected text in the event list.
 */
async function sendCommand(page: Page, command: string, expectText?: string) {
  const input = page.locator('textarea');
  await input.fill(command);
  await input.press('Enter');
  if (expectText) {
    await expect(
      page.locator('[data-testid="event"]').filter({ hasText: expectText }),
    ).toBeVisible({ timeout: 10000 });
  }
}

test.describe('Channel Commands', () => {
  test('bootstrap seeds Public channel in plugin schema', async ({ page }) => {
    await connectAsGuest(page);

    // The plugin seeds "Public" during Init. Verify it exists in the DB.
    const channel = await db.getChannelByName('Public');
    expect(channel, 'Public channel should be seeded by plugin bootstrap').not.toBeNull();
    expect(channel!.type).toBe('public');
    expect(channel!.owner_id).toBe('system');
    expect(channel!.archived_at).toBeNull();
  });

  test('channel list shows seeded Public channel', async ({ page }) => {
    await connectAsGuest(page);
    await sendCommand(page, 'channel list', 'Public');
  });

  test('channel join adds membership and emits join event', async ({ page }) => {
    await connectAsGuest(page);

    // Get player info for DB verification
    const sessionId = await page.evaluate(() => {
      const raw = sessionStorage.getItem('holomush-session');
      return raw ? JSON.parse(raw).sessionId : null;
    });
    expect(sessionId).toBeTruthy();
    const session = await db.getSessionById(sessionId!);
    expect(session).not.toBeNull();
    const player = await db.getPlayerByCharacterId(session!.character_id);
    expect(player).not.toBeNull();

    await sendCommand(page, 'channel join Public', "joined channel 'Public'");

    // DB: membership row exists (retry for cross-connection visibility)
    const channel = await db.getChannelByName('Public');
    expect(channel).not.toBeNull();
    await expect(async () => {
      const membership = await db.getChannelMembership(channel!.id, player!.id);
      expect(membership, 'Membership row should exist after join').not.toBeNull();
      expect(membership!.role).toBe('member');
    }).toPass({ timeout: 5000 });

    // DB: channel_join event emitted to the event store
    const stream = `channel:${channel!.id}`;
    await expect(async () => {
      const events = await db.getEventsByStream(stream);
      const joinEvent = events.find((e) => e.type === 'channel_join');
      expect(joinEvent, 'channel_join event should be in event store').toBeDefined();
    }).toPass({ timeout: 5000 });
  });

  test('channel say stores message and emits event', async ({ page }) => {
    await connectAsGuest(page);

    // Join first
    await sendCommand(page, 'channel join Public', "joined channel 'Public'");

    const channel = await db.getChannelByName('Public');
    expect(channel).not.toBeNull();

    // Send a unique message
    const token = `e2e-say-${Date.now()}`;
    await sendCommand(page, `channel say Public=${token}`);

    // DB: message stored in plugin schema
    // Use retry because dual-write may have slight delay
    await expect(async () => {
      const messages = await db.getChannelMessages(channel!.id);
      const found = messages.find((m) => m.message === token);
      expect(found, `Message "${token}" should be in channel_messages`).toBeDefined();
      expect(found!.event_type).toBe('channel_say');
      expect(found!.source).toBe('game');
    }).toPass({ timeout: 5000 });

    // DB: event emitted to event store on channel stream
    const stream = `channel:${channel!.id}`;
    await expect(async () => {
      const events = await db.getEventsByStream(stream);
      const sayEvent = events.find(
        (e) => e.type === 'channel_say' && JSON.stringify(e.payload).includes(token),
      );
      expect(sayEvent, `channel_say event with "${token}" should be in stream`).toBeDefined();
    }).toPass({ timeout: 5000 });
  });

  test('channel say with : prefix creates pose event', async ({ page }) => {
    await connectAsGuest(page);
    await sendCommand(page, 'channel join Public', "joined channel 'Public'");

    const channel = await db.getChannelByName('Public');
    expect(channel).not.toBeNull();

    const token = `e2e-pose-${Date.now()}`;
    await sendCommand(page, `channel say Public=:${token}`);

    await expect(async () => {
      const messages = await db.getChannelMessages(channel!.id);
      const found = messages.find((m) => m.message === token);
      expect(found, `Pose message "${token}" should be in channel_messages`).toBeDefined();
      expect(found!.event_type).toBe('channel_pose');
    }).toPass({ timeout: 5000 });
  });

  test('channel who shows members after join', async ({ page }) => {
    await connectAsGuest(page);
    await sendCommand(page, 'channel join Public', "joined channel 'Public'");
    await sendCommand(page, 'channel who Public', 'Members of');
  });

  test('channel history shows messages after join', async ({ page }) => {
    await connectAsGuest(page);
    await sendCommand(page, 'channel join Public', "joined channel 'Public'");

    // Send a message so there's something in history
    const token = `e2e-hist-${Date.now()}`;
    await sendCommand(page, `channel say Public=${token}`);

    // Small delay to ensure message is stored
    await page.waitForTimeout(500);

    await sendCommand(page, 'channel history Public', token);
  });

  test('channel leave removes membership', async ({ page }) => {
    await connectAsGuest(page);

    const sessionId = await page.evaluate(() => {
      const raw = sessionStorage.getItem('holomush-session');
      return raw ? JSON.parse(raw).sessionId : null;
    });
    const session = await db.getSessionById(sessionId!);
    const player = await db.getPlayerByCharacterId(session!.character_id);
    expect(player).not.toBeNull();

    await sendCommand(page, 'channel join Public', "joined channel 'Public'");
    await sendCommand(page, 'channel leave Public', "left channel 'Public'");

    // DB: membership row removed
    const channel = await db.getChannelByName('Public');
    expect(channel).not.toBeNull();
    const membership = await db.getChannelMembership(channel!.id, player!.id);
    expect(membership, 'Membership should be removed after leave').toBeNull();
  });

  test('channel gag and ungag work for character', async ({ page }) => {
    await connectAsGuest(page);
    await sendCommand(page, 'channel gag Public', "gagged for this character");
    await sendCommand(page, 'channel ungag Public', "ungagged for this character");
  });

  test('channel create requires name argument', async ({ page }) => {
    await connectAsGuest(page);
    await sendCommand(page, 'channel create', 'Usage: channel create');
  });

  test('non-member cannot say on channel', async ({ page }) => {
    await connectAsGuest(page);
    // Don't join — try to say directly
    await sendCommand(page, 'channel say Public=hello', 'Channel not found');
  });
});
