// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect, db, getClientSessionId, getClientCharacterName } from './helpers/fixtures';
import type { Page } from '@playwright/test';

/**
 * Connect as guest via the landing page and wait for the terminal to load.
 * All terminal tests must go through the auth flow since /terminal is
 * behind an auth guard.
 */
async function connectAsGuest(page: Page) {
  await page.goto('/');
  await page.getByRole('main').getByRole('button', { name: 'Try as Guest' }).click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
}

test.describe('Terminal UI', () => {
  test('connects and displays events', async ({ page }) => {
    await connectAsGuest(page);
    // Guest characters get random themed names like "Beryl Helium"
    await expect(page.locator('[data-testid="topbar-char-name"]')).toContainText(/\w+ \w+/);

    // DB: session is active with valid location at the starting location
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();
    const session = await db.getSessionById(sessionId!);
    expect(session).not.toBeNull();
    expect(session!.status).toBe('active');
    expect(db.isValidLocationId(session!.location_id)).toBe(true);

    // DB: player is a guest
    const player = await db.getPlayerByCharacterId(session!.character_id);
    expect(player).not.toBeNull();
    expect(player!.is_guest).toBe(true);
  });

  test('sends commands and receives output', async ({ page }) => {
    await connectAsGuest(page);
    const input = page.locator('textarea');
    // Use 'say' — it emits a say event to the stream (unlike 'look' which returns via RPC response)
    await input.fill('say hello world');
    await input.press('Enter');
    await expect(page.locator('[data-testid="event"]').first()).toBeVisible({ timeout: 10000 });
  });

  test('say command creates event in DB with correct actor and stream', async ({ page }) => {
    await connectAsGuest(page);
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();

    const session = await db.getSessionById(sessionId!);
    expect(session).not.toBeNull();

    // Send a unique say command
    const token = `dbcheck-${Date.now()}`;
    const input = page.locator('textarea');
    await input.fill(`say ${token}`);
    await input.press('Enter');
    // Wait for the event to appear in UI (confirms server processed it)
    await expect(
      page.locator('[data-testid="event"]').filter({ hasText: token }),
    ).toBeVisible({ timeout: 10000 });

    // DB: events table has row for this say event on the location stream
    const stream = `location:${session!.location_id}`;
    const events = await db.getEventsByStream(stream);
    const sayEvent = events.find(
      (e) => e.type === 'say' && JSON.stringify(e.payload).includes(token),
    );
    expect(sayEvent, `Expected say event with "${token}" in stream ${stream}`).toBeDefined();
    expect(sayEvent!.actor_id).toBe(session!.character_id);
  });

  test('command history is stored in sessions table', async ({ page }) => {
    await connectAsGuest(page);
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();

    // Send 3 commands with unique tokens
    const token = Date.now();
    const commands = [`say first-${token}`, `say second-${token}`, `say third-${token}`];
    const input = page.locator('textarea');

    for (const cmd of commands) {
      await input.fill(cmd);
      await input.press('Enter');
      // Wait for each event to appear before sending the next
      const keyword = cmd.replace('say ', '');
      await expect(
        page.locator('[data-testid="event"]').filter({ hasText: keyword }),
      ).toBeVisible({ timeout: 10000 });
    }

    // DB: sessions.command_history contains all 3 commands in order
    await expect(async () => {
      const history = await db.getCommandHistory(sessionId!);
      // History may contain earlier commands (e.g. from login flow), so check suffix
      const tail = history.slice(-3);
      expect(tail).toEqual(commands);
    }).toPass({ timeout: 5000 });
  });

  test('sidebar toggles with Cmd+.', async ({ page }) => {
    await connectAsGuest(page);
    await expect(page.locator('[data-testid="sidebar"][data-expanded="true"]')).toBeVisible();
    await page.keyboard.press('ControlOrMeta+.');
    await expect(page.locator('[data-testid="sidebar"][data-expanded="false"]')).toBeAttached();
    await page.keyboard.press('ControlOrMeta+.');
    await expect(page.locator('[data-testid="sidebar"][data-expanded="true"]')).toBeVisible();
  });

  test('responsive layout hides sidebar on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await connectAsGuest(page);
    await expect(page.locator('button[title="Toggle sidebar"]')).toBeVisible();
  });

  test('presence list shows self and other connections', async ({ browser }) => {
    // Two independent browser contexts (separate sessions)
    const context1 = await browser.newContext();
    const context2 = await browser.newContext();
    const page1 = await context1.newPage();
    const page2 = await context2.newPage();

    // Both connect as guests
    await connectAsGuest(page1);
    const name1 = await getClientCharacterName(page1);
    expect(name1).toBeTruthy();

    await connectAsGuest(page2);
    const name2 = await getClientCharacterName(page2);
    expect(name2).toBeTruthy();

    // DB: both sessions are active at the same location
    const session1 = await db.getActiveSessionByCharacterName(name1!);
    const session2 = await db.getActiveSessionByCharacterName(name2!);
    expect(session1).not.toBeNull();
    expect(session2).not.toBeNull();
    expect(session1!.location_id).toBe(session2!.location_id);

    // Sidebar defaults to expanded — presence list should be visible.
    // Page 1 should see BOTH characters in presence list (self + other).
    // Allow time for the arrive event to propagate via LISTEN/NOTIFY.
    const presence1 = page1.locator('.presence-list');
    await expect(presence1).toContainText(name1!, { timeout: 10000 });
    await expect(presence1).toContainText(name2!, { timeout: 10000 });

    // Page 2 should also see BOTH characters
    const presence2 = page2.locator('.presence-list');
    await expect(presence2).toContainText(name1!, { timeout: 10000 });
    await expect(presence2).toContainText(name2!, { timeout: 10000 });

    await context1.close();
    await context2.close();
  });

  test('session survives page reload', async ({ page }) => {
    await connectAsGuest(page);
    const nameBefore = await getClientCharacterName(page);
    expect(nameBefore).toBeTruthy();
    const sessionIdBefore = await getClientSessionId(page);

    // Reload — session persists, stream reconnects
    await page.reload();
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
    const nameAfter = await getClientCharacterName(page);
    expect(nameAfter).toBeTruthy();
    expect(nameAfter).toBe(nameBefore);

    // DB: same session still active after reload
    const session = await db.getSessionById(sessionIdBefore!);
    expect(session).not.toBeNull();
    expect(session!.status).toBe('active');
  });

  test('disconnect clears session so reload shows login', async ({ page }) => {
    await connectAsGuest(page);
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();

    // Send quit command to disconnect
    const input = page.locator('textarea');
    await input.fill('quit');
    await input.press('Enter');

    // Quit navigates to character picker; auth guard may redirect to /login
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Verify sessionStorage was cleared
    const session = await page.evaluate(() => sessionStorage.getItem('holomush-session'));
    expect(session).toBeNull();

    // DB: session row should be deleted after quit
    await expect(async () => {
      const dbSession = await db.getSessionById(sessionId!);
      expect(dbSession).toBeNull();
    }).toPass({ timeout: 5000 });
  });

  test('command history with up/down arrows', async ({ page }) => {
    await connectAsGuest(page);
    const input = page.locator('textarea');
    await input.fill('look');
    await input.press('Enter');
    await input.fill('say hello');
    await input.press('Enter');
    await input.press('ArrowUp');
    await expect(input).toHaveValue('say hello');
    await input.press('ArrowUp');
    await expect(input).toHaveValue('look');
  });

  test('reconnect receives live events after replay', async ({ page }) => {
    await connectAsGuest(page);

    // Reload — session persists, stream reconnects
    await page.reload();
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // Send a command with a unique token so we can distinguish it from replayed events
    const token = `live-${Date.now()}`;
    const input = page.locator('textarea');
    await input.fill(`say ${token}`);
    await input.press('Enter');
    await expect(
      page.locator('[data-testid="event"]').filter({ hasText: token })
    ).toBeVisible({ timeout: 10000 });
  });

  // B9: WebQueryStreamHistory is reachable through the web gateway and proxies
  // to CoreService.QueryStreamHistory with ABAC enforcement. The web client
  // does not yet call this RPC on mount (that's B13 scope), so this test
  // invokes it directly via fetch() inside the page context to exercise the
  // full stack: browser -> gateway -> core -> ABAC -> PostgresEventStore.
  test('WebQueryStreamHistory returns events through the web gateway', async ({ page }) => {
    await connectAsGuest(page);
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();

    const session = await db.getSessionById(sessionId!);
    expect(session).not.toBeNull();
    const stream = `location:${session!.location_id}`;

    // Emit a say event so there is at least one row on the location stream.
    const token = `history-${Date.now()}`;
    const input = page.locator('textarea');
    await input.fill(`say ${token}`);
    await input.press('Enter');
    await expect(
      page.locator('[data-testid="event"]').filter({ hasText: token }),
    ).toBeVisible({ timeout: 10000 });

    // Call WebQueryStreamHistory through the gateway using the Connect
    // JSON protocol. Cookies are carried automatically by the browser so the
    // gateway's auth middleware accepts the call.
    const resp = await page.evaluate(
      async ({ sid, streamName }: { sid: string; streamName: string }) => {
        const r = await fetch('/holomush.web.v1.WebService/WebQueryStreamHistory', {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ sessionId: sid, stream: streamName, count: 50 }),
        });
        const body = await r.text();
        return { status: r.status, body };
      },
      { sid: sessionId!, streamName: stream },
    );

    expect(resp.status, `unexpected status; body: ${resp.body}`).toBe(200);
    const payload = JSON.parse(resp.body) as {
      events?: Array<{ type?: string; payload?: unknown }>;
      hasMore?: boolean;
    };
    expect(Array.isArray(payload.events)).toBe(true);

    // At least our freshly-emitted say event must be in the history.
    const matched = (payload.events ?? []).some((e) =>
      JSON.stringify(e).includes(token),
    );
    expect(matched, `expected event with "${token}" in history response`).toBe(true);
  });

  test('page reload replays prior events from multiple guests', async ({ browser }) => {
    // Two independent browser contexts (separate sessions, same starting location)
    const ctx1 = await browser.newContext();
    const ctx2 = await browser.newContext();
    const page1 = await ctx1.newPage();
    const page2 = await ctx2.newPage();

    // Both connect as guests
    await connectAsGuest(page1);
    await connectAsGuest(page2);

    // Guest 1 says something unique
    const token = Date.now();
    const input1 = page1.locator('textarea');
    await input1.fill(`say alpha-${token}`);
    await input1.press('Enter');
    await expect(
      page1.locator('[data-testid="event"]').filter({ hasText: `alpha-${token}` }),
    ).toBeVisible({ timeout: 10000 });

    // Guest 2 says something unique — visible to Guest 1 (same location)
    const input2 = page2.locator('textarea');
    await input2.fill(`say bravo-${token}`);
    await input2.press('Enter');
    await expect(
      page1.locator('[data-testid="event"]').filter({ hasText: `bravo-${token}` }),
    ).toBeVisible({ timeout: 10000 });

    // Guest 1 says a third thing
    await input1.fill(`say charlie-${token}`);
    await input1.press('Enter');
    await expect(
      page1.locator('[data-testid="event"]').filter({ hasText: `charlie-${token}` }),
    ).toBeVisible({ timeout: 10000 });

    // Capture event order before reload
    const eventsBefore = await page1
      .locator('[data-testid="event"]')
      .filter({ hasText: new RegExp(`(alpha|bravo|charlie)-${token}`) })
      .allTextContents();
    expect(eventsBefore).toHaveLength(3);

    // --- Page reload ---
    await page1.reload();
    await expect(page1.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // All three events should reappear after replay
    await expect(
      page1.locator('[data-testid="event"]').filter({ hasText: `alpha-${token}` }),
    ).toBeVisible({ timeout: 10000 });
    await expect(
      page1.locator('[data-testid="event"]').filter({ hasText: `bravo-${token}` }),
    ).toBeVisible({ timeout: 10000 });
    await expect(
      page1.locator('[data-testid="event"]').filter({ hasText: `charlie-${token}` }),
    ).toBeVisible({ timeout: 10000 });

    // Verify replay order matches original order
    const eventsAfter = await page1
      .locator('[data-testid="event"]')
      .filter({ hasText: new RegExp(`(alpha|bravo|charlie)-${token}`) })
      .allTextContents();
    expect(eventsAfter).toHaveLength(3);
    expect(eventsAfter).toEqual(eventsBefore);

    // Replayed events should be dimmed (opacity 0.5 via .dimmed class)
    await expect(async () => {
      const dimmedCount = await page1
        .locator('.dimmed [data-testid="event"]')
        .filter({ hasText: new RegExp(`(alpha|bravo|charlie)-${token}`) })
        .count();
      expect(dimmedCount).toBe(3);
    }).toPass({ timeout: 5000 });

    // Live event after replay should NOT be dimmed
    const reloadedInput = page1.locator('textarea');
    await reloadedInput.fill(`say delta-${token}`);
    await reloadedInput.press('Enter');
    await expect(
      page1.locator('[data-testid="event"]').filter({ hasText: `delta-${token}` }),
    ).toBeVisible({ timeout: 10000 });
    const deltaDimmed = await page1
      .locator('.dimmed [data-testid="event"]')
      .filter({ hasText: `delta-${token}` })
      .count();
    expect(deltaDimmed).toBe(0);

    // Separator between replayed and live events.
    await expect(
      page1.locator('.sep-live').filter({ hasText: 'LIVE' }),
    ).toBeVisible({ timeout: 5000 });

    // DB: all 4 events exist on the location stream
    const sessionId = await getClientSessionId(page1);
    const session = await db.getSessionById(sessionId!);
    const stream = `location:${session!.location_id}`;
    const events = await db.getEventsByStream(stream);
    for (const label of ['alpha', 'bravo', 'charlie', 'delta']) {
      const found = events.find(
        (e) => e.type === 'say' && JSON.stringify(e.payload).includes(`${label}-${token}`),
      );
      expect(found, `Expected say event "${label}-${token}" in stream ${stream}`).toBeDefined();
    }

    await ctx1.close();
    await ctx2.close();
  });

  test('detach + accumulated events + reload produces no duplicate scrollback entries', async ({
    browser,
  }) => {
    // Two guests in the same location.
    const ctx1 = await browser.newContext();
    const ctx2 = await browser.newContext();
    const page1 = await ctx1.newPage();
    const page2 = await ctx2.newPage();

    await connectAsGuest(page1);
    await connectAsGuest(page2);

    // Guest 1 says one event to ensure its session is well-seeded.
    const token = Date.now();
    const input1 = page1.locator('textarea');
    await input1.fill(`say seed-${token}`);
    await input1.press('Enter');
    await expect(
      page1.locator('[data-testid="event"]').filter({ hasText: `seed-${token}` }),
    ).toBeVisible({ timeout: 10000 });

    // "Detach" page1 by capturing its sessionStorage (for re-seeding), then
    // closing the tab. Guest 2 emits three events while page1 is gone.
    const sessionId = await getClientSessionId(page1);
    expect(sessionId).toBeTruthy();
    const savedSession = await page1.evaluate(() =>
      sessionStorage.getItem('holomush-session'),
    );
    expect(savedSession).toBeTruthy();
    await page1.close();

    const input2 = page2.locator('textarea');
    for (const label of ['detached-a', 'detached-b', 'detached-c']) {
      await input2.fill(`say ${label}-${token}`);
      await input2.press('Enter');
      await expect(
        page2.locator('[data-testid="event"]').filter({ hasText: `${label}-${token}` }),
      ).toBeVisible({ timeout: 10000 });
    }

    // Reopen page1 with the captured session re-seeded into sessionStorage
    // BEFORE the SvelteKit auth guard runs. addInitScript fires on every
    // navigation, including the initial goto below.
    const page1Reopened = await ctx1.newPage();
    await page1Reopened.addInitScript((session) => {
      sessionStorage.setItem('holomush-session', session);
    }, savedSession!);
    await page1Reopened.goto('/terminal');
    await expect(page1Reopened.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // Wait for the detached events to appear via replay/backfill before
    // asserting counts. Use the last event as the sync point.
    await expect(
      page1Reopened
        .locator('[data-testid="event"]')
        .filter({ hasText: `detached-c-${token}` }),
    ).toBeVisible({ timeout: 10000 });

    // Each detached-* event must appear EXACTLY ONCE, even though Subscribe's
    // cursor-based replay AND QueryStreamHistory both deliver them.
    for (const label of ['detached-a', 'detached-b', 'detached-c']) {
      const count = await page1Reopened
        .locator('[data-testid="event"]')
        .filter({ hasText: `${label}-${token}` })
        .count();
      expect(count, `expected exactly one rendering of ${label}-${token}`).toBe(1);
    }

    await ctx1.close();
    await ctx2.close();
  });

  test('command history persists across reconnect', async ({ page }) => {
    await connectAsGuest(page);

    // Send commands with unique tokens to avoid collision with other tests
    const token = Date.now();
    const input = page.locator('textarea');
    await input.fill(`say first-${token}`);
    await input.press('Enter');
    await expect(
      page.locator('[data-testid="event"]').filter({ hasText: `first-${token}` })
    ).toBeVisible({ timeout: 10000 });
    await input.fill(`say second-${token}`);
    await input.press('Enter');
    await expect(
      page.locator('[data-testid="event"]').filter({ hasText: `second-${token}` })
    ).toBeVisible({ timeout: 10000 });

    // Reload — session persists, history loaded from server via GetCommandHistory RPC
    await page.reload();
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // Wait for command history to load from server (async RPC in CommandInput $effect)
    const inputAfter = page.locator('textarea');
    await expect(inputAfter).toBeVisible();
    // Poll: ArrowUp should eventually produce the last command once history loads
    await expect(async () => {
      await inputAfter.focus();
      await inputAfter.press('ArrowUp');
      const val = await inputAfter.inputValue();
      expect(val).toBe(`say second-${token}`);
    }).toPass({ timeout: 5000 });
    await inputAfter.press('ArrowUp');
    await expect(inputAfter).toHaveValue(`say first-${token}`);
  });

  test('in-progress input persists across page reload', async ({ page }) => {
    await connectAsGuest(page);

    // Type a partial command (don't press Enter)
    const input = page.locator('textarea');
    await input.fill('say this is a long pose that I do not want to lose');
    // Wait for debounced localStorage save (500ms + buffer)
    await page.waitForTimeout(700);

    // Reload — session persists
    await page.reload();
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // The draft should be restored
    const inputAfter = page.locator('textarea');
    await expect(inputAfter).toHaveValue('say this is a long pose that I do not want to lose');
  });

  test('draft does not leak across sessions', async ({ page }) => {
    await connectAsGuest(page);

    // Type a draft and wait for debounced save
    const input = page.locator('textarea');
    await input.fill('leaked draft from old session');
    await page.waitForTimeout(700);

    // Quit — navigates to character picker; auth guard may redirect to /login
    await input.fill('quit');
    await input.press('Enter');
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Reconnect as guest from landing page
    await connectAsGuest(page);

    // The textarea should be empty — no draft from the old session
    const inputAfter = page.locator('textarea');
    await expect(inputAfter).toHaveValue('');
  });

  test('Cmd+K opens palette, Escape closes it', async ({ page }) => {
    await connectAsGuest(page);
    await page.keyboard.press('ControlOrMeta+k');
    await expect(page.locator('[data-cmdk-dialog]')).toBeVisible({ timeout: 3000 });
    // Type to filter
    await page.keyboard.type('theme');
    await expect(page.locator('[data-cmdk-item]').first()).toContainText(/theme/i);
    await page.keyboard.press('Escape');
    await expect(page.locator('[data-cmdk-dialog]')).toBeHidden();
  });

  test('Cmd+B toggles rail visibility', async ({ page }) => {
    await connectAsGuest(page);
    const rail = page.locator('[data-testid="rail"]');
    await expect(rail).toBeVisible();
    // Rail starts visible (not is-hidden)
    await expect(rail).not.toHaveClass(/is-hidden/);
    await page.keyboard.press('ControlOrMeta+b');
    await expect(rail).toHaveClass(/is-hidden/);
    await page.keyboard.press('ControlOrMeta+b');
    await expect(rail).not.toHaveClass(/is-hidden/);
  });

  test('Cmd+Shift+E opens composer; text mirrors inline input', async ({ page }) => {
    await connectAsGuest(page);
    const input = page.locator('textarea').first();
    await input.fill('partial pose from inline');
    await page.waitForTimeout(700);  // allow draft debounce
    await page.keyboard.press('ControlOrMeta+Shift+KeyE');
    const composer = page.locator('[role="region"][aria-label="Command composer"]');
    await expect(composer).toBeVisible();
    // Composer textarea should see the draft
    const composerTA = composer.locator('textarea');
    await expect(composerTA).toHaveValue('partial pose from inline');
    // Esc closes composer
    await page.keyboard.press('Escape');
    await expect(composer).toBeHidden();
  });

  test('mode chip appears for say/pose/ooc prefixes', async ({ page }) => {
    await connectAsGuest(page);
    const input = page.locator('textarea').first();
    await input.fill(': smiles');
    await expect(page.locator('.mode-chip')).toContainText(/pose/i);
    await input.fill('say hello');
    await expect(page.locator('.mode-chip')).toContainText(/say/i);
    await input.fill('ooc brb');
    await expect(page.locator('.mode-chip')).toContainText(/ooc/i);
    await input.fill('look');
    await expect(page.locator('.mode-chip')).toHaveCount(0);
  });

  test('timestamps render on terminal lines', async ({ page }) => {
    await connectAsGuest(page);
    const input = page.locator('textarea').first();
    const token = `ts-${Date.now()}`;
    await input.fill(`say ${token}`);
    await input.press('Enter');
    await expect(
      page.locator('[data-testid="event"]').filter({ hasText: token }),
    ).toBeVisible({ timeout: 10000 });
    // Each line has a .tstamp in HH:MM form
    const tstamp = page.locator('.line .tstamp').first();
    await expect(tstamp).toBeVisible();
    await expect(tstamp).toHaveText(/^\d{2}:\d{2}$/);
  });

  test('IME composition does not trigger global shortcuts', async ({ page }) => {
    await connectAsGuest(page);
    // Dispatch a synthesized keydown with isComposing=true for the palette shortcut.
    await page.evaluate(() => {
      const ev = new KeyboardEvent('keydown', {
        key: 'k', code: 'KeyK', metaKey: true, ctrlKey: true, isComposing: true,
        bubbles: true, cancelable: true,
      });
      window.dispatchEvent(ev);
    });
    // Palette must not open
    await expect(page.locator('[data-cmdk-dialog]')).toBeHidden();
  });
});
