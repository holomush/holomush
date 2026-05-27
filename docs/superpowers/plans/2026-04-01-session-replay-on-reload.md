<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Session Replay on Page Reload — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix page reload so the web client replays prior events instead of showing a blank terminal.

**Architecture:** The web client always starts with an empty in-memory terminal store on page load. Currently it sends `replayFromCursor: true` on reconnect, which tells the server to only send events *after* the persisted cursor — resulting in zero events (since the client already received them all before reload). The fix: send `replayFromCursor: false` so the server replays from the beginning of the stream. One E2E test validates multi-guest replay correctness.

**Tech Stack:** SvelteKit (Svelte 5), Playwright, ConnectRPC, Go gRPC

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `web/src/routes/(authed)/terminal/+page.svelte:99` | Change `replayFromCursor` from `true` to `false` |
| Modify | `web/e2e/terminal.spec.ts` | Add multi-guest replay E2E test |

---

### Task 1: Fix `replayFromCursor` flag

**Files:**

- Modify: `web/src/routes/(authed)/terminal/+page.svelte:99`

- [ ] **Step 1: Change the flag**

In `startStreaming()`, change line 99:

```typescript
// Before:
{ sessionId, replayFromCursor: true },

// After:
{ sessionId, replayFromCursor: false },
```

- [ ] **Step 2: Verify dev build compiles**

Run: `cd web && npm run check`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
jj commit -m "fix(web): request full replay on reconnect instead of cursor-based

The web client's terminal store is purely in-memory (Svelte writable).
On page reload it's empty, but replayFromCursor: true told the server
to only send events after the persisted cursor — which meant zero
events after reload. Setting replayFromCursor: false makes the server
replay from the beginning of the stream, which is correct since the
client has no local state to preserve."
```

---

### Task 2: Write multi-guest replay E2E test

**Files:**

- Modify: `web/e2e/terminal.spec.ts`

- [ ] **Step 1: Write the E2E test**

Add the following test inside the existing `test.describe('Terminal UI', ...)` block, after the `'reconnect receives live events after replay'` test (after line 231):

```typescript
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
    const dimmedCount = await page1
      .locator('.dimmed [data-testid="event"]')
      .filter({ hasText: new RegExp(`(alpha|bravo|charlie)-${token}`) })
      .count();
    expect(dimmedCount).toBe(3);

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
```

- [ ] **Step 2: Run the E2E test locally to verify it passes**

Run: `task test:e2e -- --grep "page reload replays prior events"`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
jj commit -m "test(e2e): verify multi-guest session replay on page reload

Two guests send say commands, Guest 1 reloads, asserts:
- All 3 pre-reload events reappear in correct order
- Replayed events are dimmed, live events are not
- DB has all events on the location stream"
```

---

### Task 3: Final verification

- [ ] **Step 1: Run full E2E suite**

Run: `task test:e2e`
Expected: All tests pass

- [ ] **Step 2: Run unit tests and lint**

Run: `task test && task lint`
Expected: All pass
