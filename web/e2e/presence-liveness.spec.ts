// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect, db, getClientCharacterName, getClientSessionId } from './helpers/fixtures';
import type { Page } from '@playwright/test';

/**
 * presence-liveness.spec.ts — Playwright E2E spec for holomush-rsoe6.15
 *
 * Covers sub-specs (a) and (b) of the Task 15 plan.
 *
 * Sub-spec (c) — ghost clears within L + sweep after a hard transport kill —
 * is DESCOPED to follow-up bead (same bead as sub-spec (d)). Reason:
 * clearing a ghost requires the session-lease-ttl to lapse and the reaper to
 * sweep. Production defaults are 45s lease + 30s reaper, totalling up to 75s
 * of wait time per test run — unacceptably slow. Shortening the global TTL in
 * compose.e2e.yaml is unsafe because the web gateway heartbeats connections
 * every 15s (hardcoded in internal/web/handler.go:417); any lease-ttl shorter
 * than ~20s risks sweeping live connections during concurrent e2e specs. Making
 * the heartbeat interval configurable per-E2E-run would require invasive
 * harness changes (gateway flag, compose.e2e override). That work belongs in
 * the follow-up bead. See holomush-rsoe6 for tracking.
 *
 * Sub-spec (d) — extended ghost scenarios — also deferred to the follow-up bead.
 */

// ─── shared helper ──────────────────────────────────────────────────────────

/**
 * Connect as a guest from the landing page and wait for the terminal to load.
 * Returns the character name stamped into the session.
 */
async function connectAsGuest(page: Page): Promise<string> {
  await page.goto('/');
  await page.getByRole('main').getByRole('button', { name: 'Try as Guest' }).click();
  await expect(page).toHaveURL(/\/terminal$/, { timeout: 15_000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 15_000 });
  const name = await getClientCharacterName(page);
  if (!name) throw new Error('could not read character name from sessionStorage');
  return name;
}

// ─── (a) two-browser roster + clean drop (no ghost) ─────────────────────────

test('two guests each see the other in presence roster, dropping one removes them without a ghost', async ({
  browser,
}) => {
  const ctxA = await browser.newContext();
  const ctxB = await browser.newContext();
  try {
    const pageA = await ctxA.newPage();
    const pageB = await ctxB.newPage();

    // Connect both guests and capture character names.
    const nameA = await connectAsGuest(pageA);
    const nameB = await connectAsGuest(pageB);

    // Sanity: both sessions active at the same starting location.
    const sessionA = await db.getActiveSessionByCharacterName(nameA);
    const sessionB = await db.getActiveSessionByCharacterName(nameB);
    expect(sessionA).not.toBeNull();
    expect(sessionB).not.toBeNull();
    expect(sessionA!.location_id).toBe(sessionB!.location_id);
    expect(sessionA!.grid_present).toBe(true);
    expect(sessionB!.grid_present).toBe(true);

    // Both browsers see each other in the presence roster.
    // The card is hidden when empty; use data-testid="presence-card".
    const rosterA = pageA.locator('[data-testid="presence-card"]');
    const rosterB = pageB.locator('[data-testid="presence-card"]');
    await expect(rosterA).toContainText(nameA, { timeout: 15_000 });
    await expect(rosterA).toContainText(nameB, { timeout: 15_000 });
    await expect(rosterB).toContainText(nameA, { timeout: 15_000 });
    await expect(rosterB).toContainText(nameB, { timeout: 15_000 });

    // B sends "quit" — this triggers a clean server-side Disconnect RPC which
    // immediately emits a leave event and deletes the guest session. This is
    // preferable to ctx.close() which relies on the gateway detecting the gone
    // HTTP client at the next 15s heartbeat tick (too slow for a test timeout).
    const inputB = pageB.locator('textarea');
    await inputB.fill('quit');
    await inputB.press('Enter');

    // After B quits, A's roster must:
    //   1. NOT contain B's name (no ghost).
    //   2. Still contain A's own name (A is still connected).
    //
    // The leave event goes via JetStream → A's Subscribe stream → mirrorMovementPresence →
    // presence.remove(actorId). Allow 15s for the event to propagate end-to-end.
    await expect(rosterA).toContainText(nameA, { timeout: 15_000 });
    await expect(rosterA).not.toContainText(nameB, { timeout: 15_000 });

    // DB cross-check: B's guest session should be deleted (null) or expired.
    // getActiveSessionByCharacterName filters for status IN ('active','detached')
    // — a deleted guest session returns null.
    await expect
      .poll(
        async () => {
          const s = await db.getActiveSessionByCharacterName(nameB);
          // Null means deleted (guest clean quit) — expected.
          // grid_present=false also acceptable if status row persists briefly.
          return s?.grid_present ?? false;
        },
        { timeout: 15_000, message: 'expected B guest session to be gone after quit' },
      )
      .toBe(false);
  } finally {
    await ctxA.close().catch(() => {});
    await ctxB.close().catch(() => {});
  }
});

// ─── (b) reload keeps character present exactly once ────────────────────────

test('reloading within the reattach window keeps the character present exactly once', async ({
  browser,
}) => {
  // Two contexts: the reloading character (ctxSelf) and a peer observer (ctxPeer).
  const ctxSelf = await browser.newContext();
  const ctxPeer = await browser.newContext();
  try {
    const pageSelf = await ctxSelf.newPage();
    const pagePeer = await ctxPeer.newPage();

    // Connect both guests.
    const nameSelf = await connectAsGuest(pageSelf);
    const namePeer = await connectAsGuest(pagePeer);

    // Capture session ID for DB assertions.
    const sessionIdBefore = await getClientSessionId(pageSelf);
    expect(sessionIdBefore).toBeTruthy();

    // Both see each other before reload.
    const rosterSelf = pageSelf.locator('[data-testid="presence-card"]');
    const rosterPeer = pagePeer.locator('[data-testid="presence-card"]');
    await expect(rosterSelf).toContainText(nameSelf, { timeout: 15_000 });
    await expect(rosterSelf).toContainText(namePeer, { timeout: 15_000 });
    await expect(rosterPeer).toContainText(nameSelf, { timeout: 15_000 });
    await expect(rosterPeer).toContainText(namePeer, { timeout: 15_000 });

    // Reload pageSelf within the reattach TTL (default 30m) — this simulates
    // a real browser reload, not a hard kill. The client sends a clean
    // Disconnect, then reconnects with the stored session ID.
    await pageSelf.reload();
    await expect(pageSelf.locator('.terminal-layout')).toBeVisible({ timeout: 15_000 });

    // After reload, the character name must be the same (reattached session).
    const nameAfterReload = await getClientCharacterName(pageSelf);
    expect(nameAfterReload).toBe(nameSelf);

    // DB: same session still active after reload. Use expect.poll because
    // the server updates grid_present asynchronously when the connection
    // re-registers after the Subscribe RPC completes.
    await expect
      .poll(
        async () => {
          const s = await db.getSessionById(sessionIdBefore!);
          return s ? { status: s.status, grid_present: s.grid_present } : null;
        },
        { timeout: 15_000, message: 'expected session to be active with grid_present=true after reload' },
      )
      .toEqual({ status: 'active', grid_present: true });

    // Self's roster must contain nameSelf EXACTLY ONCE.
    // The PresenceList card shows one <li> row per presenceStore.map entry;
    // a duplicate would mean the store has two entries for the same character.
    // innerText includes all rendered text; counting regex matches gives the
    // number of roster rows for this character.
    const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

    await expect(rosterSelf).toContainText(nameSelf, { timeout: 15_000 });
    await expect
      .poll(
        async () => {
          const text = await pageSelf
            .locator('[data-testid="presence-card"]')
            .innerText()
            .catch(() => '');
          return (text.match(new RegExp(escapeRe(nameSelf), 'g')) ?? []).length;
        },
        {
          timeout: 15_000,
          message: `expected ${nameSelf} to appear exactly once in self roster`,
        },
      )
      .toBe(1);

    // Peer also sees nameSelf exactly once (no duplicate entry for the reloaded character).
    await expect(rosterPeer).toContainText(nameSelf, { timeout: 15_000 });
    await expect
      .poll(
        async () => {
          const text = await pagePeer
            .locator('[data-testid="presence-card"]')
            .innerText()
            .catch(() => '');
          return (text.match(new RegExp(escapeRe(nameSelf), 'g')) ?? []).length;
        },
        {
          timeout: 15_000,
          message: `expected ${nameSelf} to appear exactly once in peer roster`,
        },
      )
      .toBe(1);
  } finally {
    await ctxSelf.close().catch(() => {});
    await ctxPeer.close().catch(() => {});
  }
});
