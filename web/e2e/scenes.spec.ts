// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect, db, getClientSessionId } from './helpers/fixtures';
import type { Page, BrowserContext } from '@playwright/test';

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

// ── Scenes workspace helpers ─────────────────────────────────────────────────

/** Generate unique test credentials for registered-player scenarios. */
function uniqueSceneUser(prefix: string) {
  const suffix = `${Date.now()}_${crypto.randomUUID().slice(0, 4)}`;
  const charSuffix = crypto.randomUUID().replace(/[^a-z]/g, '').slice(0, 6);
  // Character names allow letters and spaces only — strip any non-letter chars
  // from the prefix (e.g. 'a11y' → 'ay') before using it in the name.
  const safePrefix = prefix.replace(/[^a-zA-Z]/g, '');
  const capitalised = safePrefix.charAt(0).toUpperCase() + safePrefix.slice(1);
  return {
    username: `e2e_sc_${prefix}_${suffix}`,
    charName: `Sc${capitalised} ${charSuffix}`,
    password: 'testpass123',
  };
}

/**
 * Register a new player, create a character, and land in the terminal.
 * Returns `{ username, password, charName }` for later re-login if needed.
 * Reuses the same form-fill pattern as auth.spec.ts and character-switcher.spec.ts.
 */
async function registerAndEnterTerminal(
  page: Page,
  prefix: string,
): Promise<{ username: string; password: string; charName: string }> {
  const { username, charName, password } = uniqueSceneUser(prefix);
  await page.goto('/register');
  await page.fill('input[name="username"]', username);
  await page.fill('input[name="password"]', password);
  await page.fill('input[name="confirmPassword"]', password);
  await page.locator('button[type="submit"]').click();
  await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });
  await page.locator('text=Create New Character').click();
  await page.fill('input[name="characterName"]', charName);
  await page.locator('button[role="checkbox"]').click();
  await page.locator('button:has-text("Create")').click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
  // Wait for the stream to be fully open (STREAM_OPENED → connectionId set →
  // REPLAY_COMPLETE → conn-pill shows "connected"). Without this, sendCommand
  // may carry an empty connectionId, causing `scene focus` to error with
  // "`scene focus` requires a live connection."
  await page
    .locator('[data-testid="conn-pill"][data-status="connected"]')
    .waitFor({ timeout: 15000 });
  return { username, password, charName };
}

/**
 * Create a scene via terminal command and return its ID.
 * Uses the same extractSceneIdFromOutput helper already defined in this file.
 */
async function createSceneViaTerminal(page: Page, title: string): Promise<string> {
  const before = await currentEventCount(page);
  await sendCommand(page, `scene create ${title}`);
  return extractSceneIdFromOutput(page, before);
}

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

// ── Scenes workspace suite (E9.5, holomush-5rh.8.19) ─────────────────────────
//
// A11y path taken: no axe/AxeBuilder dependency found in web/package.json or
// web/e2e/. Scenario 8 therefore asserts STRUCTURAL roles added in task .8.18:
//   - role="log" with aria-live="polite" exists in the workspace
//   - nav landmark (scene list) present
//   - main landmark present
//   - role="listbox" (roving-tabindex list) present

test.describe('Scenes workspace (E9.5)', () => {
  // ── S1: Browse board — open scene listed; title filter narrows ──────────────
  //
  // Creates an active (visibility=open) scene, navigates to /scenes/browse,
  // confirms the scene appears, then uses the title-search input to narrow the
  // list to just that scene — and confirms it persists, while entering a
  // non-matching string hides it.
  //
  // Note on tag filtering: `scene set` does not expose the `tags` field and
  // `handleCreate` treats the full arg string as the title — there is no terminal
  // command surface for setting tags. Tag-filter behaviour is therefore tested
  // via the title-search input which is also part of TagFilter and exercises the
  // same narrowing path (`filteredScenes` derived from `titleQuery`).
  test('browse board lists seeded open scene and title filter narrows results', async ({ page }) => {
    await registerAndEnterTerminal(page, 'brw');

    // Unique title for deterministic filtering.
    const ts = Date.now();
    const title = `BrowseTest${ts}`;
    const sceneId = await createSceneViaTerminal(page, title);
    expect(sceneId).toMatch(/^[0-9A-Z]{26}$/);

    // DB: scene is active (visibility defaults to open).
    const scene = await db.getSceneById(sceneId);
    expect(scene?.state).toBe('active');

    // Navigate to browse board.
    await page.goto('/scenes/browse');
    await expect(page.getByRole('list', { name: 'Scene list' })).toBeVisible({ timeout: 10000 });

    // Our scene must appear in the unfiltered list.
    await expect(page.getByRole('listitem').filter({ hasText: title })).toBeVisible({
      timeout: 10000,
    });

    // Apply the title-search filter with the exact title — scene stays visible.
    const searchInput = page.getByRole('searchbox', { name: 'Filter scenes by title' });
    await searchInput.fill(title);
    await expect(page.getByRole('listitem').filter({ hasText: title })).toBeVisible({
      timeout: 5000,
    });

    // Change filter to something that doesn't match — scene disappears.
    await searchInput.fill('__nomatch__e2e__');
    await expect(page.getByRole('listitem').filter({ hasText: title })).not.toBeVisible({
      timeout: 5000,
    });
  });

  // ── S2: Watch flow — workspace shows scene, log visible, composer present ──
  //
  // The creator of a scene is automatically joined as the owner (participant,
  // not observer). Navigating to /scenes?watch=<id> selects that scene in the
  // workspace. The log region and composer textarea must be present.
  test('workspace shows selected scene with log region and composer for the owner', async ({
    page,
  }) => {
    await registerAndEnterTerminal(page, 'wch');

    const title = `WatchTest ${Date.now()}`;
    const sceneId = await createSceneViaTerminal(page, title);

    // Navigate to workspace with the scene pre-selected via query param.
    await page.goto(`/scenes?watch=${sceneId}`);

    // Workspace container must load.
    await expect(page.locator('[data-testid="scenes-workspace"]')).toBeVisible({ timeout: 15000 });

    // The scene title appears in the center-pane title bar (a <header> inside
    // <main> — not the page-level banner; use a text-based locator).
    await expect(page.locator('.font-semibold').filter({ hasText: title })).toBeVisible({
      timeout: 10000,
    });

    // Log region (role=log, aria-label="scene log") must be visible.
    await expect(page.getByRole('log', { name: 'scene log' })).toBeVisible({ timeout: 10000 });

    // Owner is a participant — composer textarea is present and enabled (not the
    // Join CTA that observers see).
    await expect(page.locator('textarea[name="scene-composer"]')).toBeVisible({ timeout: 10000 });
    await expect(page.locator('textarea[name="scene-composer"]')).toBeEnabled();
  });

  // ── S3: Participate — workspace composer submit flow ─────────────────────
  //
  // Owner enters the workspace, types a pose in the composer, clicks Pose,
  // and verifies the submit path completes cleanly: the draft clears (button
  // goes disabled again) and no client-side error is shown.
  //
  // Scope: this test covers the composer UI submit flow ONLY — it does NOT
  // verify that the pose persists or appears live. The end-to-end flow
  // (SetSceneFocus → JoinFocus → pose routes to scene_log) is proven by the
  // integration test in test/integration/scenes/set_scene_focus_participation_test.go
  // (holomush-5rh.8.26). Asserting the pose card appears live in E2E requires
  // E2E crypto key provisioning, deferred to holomush-5rh.8.27.
  //
  // QUARANTINED (holomush-5rh.8.27): the pose submit POSTs a sensitive scene
  // event, which the production live Subscribe/Publish path cannot yet encrypt
  // (DEK/KEK wiring deferred to holomush-5rh.8.29) — the send 500s, so the draft
  // never clears and the button stays enabled. Un-quarantine when the crypto
  // follow-up (.8.27/.8.29) lands. Runs locally with HOLOMUSH_RUN_QUARANTINED=1.
  test('workspace composer accepts pose and clears draft without error', { tag: ['@quarantine', '@holomush-5rh.8.27'] }, async ({ page }) => {
    await registerAndEnterTerminal(page, 'prt');

    const title = `PoseTest ${Date.now()}`;
    const sceneId = await createSceneViaTerminal(page, title);

    await page.goto(`/scenes?watch=${sceneId}`);
    await expect(page.locator('[data-testid="scenes-workspace"]')).toBeVisible({ timeout: 15000 });
    await expect(page.locator('.font-semibold').filter({ hasText: title })).toBeVisible({
      timeout: 10000,
    });

    // Wait for composer to be ready.
    const composer = page.locator('textarea[name="scene-composer"]');
    await expect(composer).toBeVisible({ timeout: 15000 });
    await expect(composer).toBeEnabled();

    // Write and submit a pose.
    const poseText = `PoseE2E-${Date.now()}`;
    await composer.fill(poseText);
    // Wait for Svelte's reactive draftText to reflect the filled value so the
    // Send Pose button (disabled when draftText is empty) becomes enabled.
    const sendPoseBtn = page.getByRole('button', { name: 'Send pose' });
    await expect(sendPoseBtn).toBeEnabled({ timeout: 5000 });
    await sendPoseBtn.click();

    // Successful send() invocation clears the draft → button goes disabled.
    // No client-side errorMsg paragraph should be rendered.
    await expect(sendPoseBtn).toBeDisabled({ timeout: 5000 });
    await expect(page.locator('p.text-destructive')).not.toBeVisible();
  });

  // ── S4: Terminal isolation — workspace focus doesn't disturb terminal ──────
  //
  // Opens terminal in tab 1 (registered player), workspace in tab 2 (same
  // browser context = shared auth cookie). After the workspace loads, the
  // terminal tab must still receive `say` events — the workspace's alt-session
  // stream must not interfere with the terminal's own stream.
  test('workspace open in a second tab does not disturb the terminal stream', async ({
    browser,
  }) => {
    const ctx: BrowserContext = await browser.newContext();
    try {
      // ── Tab 1: register and enter terminal ──
      const terminalPage = await ctx.newPage();
      await registerAndEnterTerminal(terminalPage, 'iso');

      // Create a scene from the terminal so the workspace has something to load.
      const title = `IsoTest ${Date.now()}`;
      const sceneId = await createSceneViaTerminal(terminalPage, title);

      // Confirm the terminal stream is live before opening the workspace.
      const token1 = `iso-before-${Date.now()}`;
      const sayBefore = await currentEventCount(terminalPage);
      await sendCommand(terminalPage, `say ${token1}`);
      await waitForOutputMatching(terminalPage, new RegExp(token1), sayBefore);

      // ── Tab 2: workspace tab (same auth context → shared player cookie) ──
      const workspacePage = await ctx.newPage();
      await workspacePage.goto(`/scenes?watch=${sceneId}`);
      await expect(
        workspacePage.locator('[data-testid="scenes-workspace"]'),
      ).toBeVisible({ timeout: 20000 });
      // Workspace is loaded — the alt-session stream is now running in tab 2.

      // ── Assert terminal tab still receives events after workspace is live ──
      await terminalPage.bringToFront();
      const token2 = `iso-after-${Date.now()}`;
      const sayAfter = await currentEventCount(terminalPage);
      await sendCommand(terminalPage, `say ${token2}`);
      await waitForOutputMatching(terminalPage, new RegExp(token2), sayAfter);

      // Terminal page shows no scenes-workspace UI.
      await expect(
        terminalPage.locator('[data-testid="scenes-workspace"]'),
      ).not.toBeVisible();
    } finally {
      await ctx.close();
    }
  });

  // ── S5: Export — ended scene → md + jsonl downloads with correct filenames ─
  //
  // The scene read page (`/scenes/[id]`) exposes two download buttons. For an
  // ended scene where the caller is a participant, it uses the exportScene RPC
  // path. Playwright's `waitForEvent('download')` captures the triggered download
  // without relying on browser navigation — no sleep needed.
  //
  // Note: only create + end are exercised here (no pose step). The scene creator
  // is automatically a participant, so exportScene is accessible. An empty log
  // still produces a valid downloadable file (sentinel content); the buttons'
  // existence and the filename pattern are what this test asserts.
  test('scene read page triggers md and jsonl downloads with correct filenames', async ({
    page,
  }) => {
    await registerAndEnterTerminal(page, 'exp');

    // Create then immediately end the scene.
    const title = `ExportTest ${Date.now()}`;
    const sceneId = await createSceneViaTerminal(page, title);

    const endBefore = await currentEventCount(page);
    await sendCommand(page, `scene end ${sceneId}`);
    await waitForOutputMatching(page, /ended/, endBefore);

    // DB: confirm ended.
    await expect(async () => {
      const s = await db.getSceneById(sceneId);
      expect(s?.state).toBe('ended');
    }).toPass({ timeout: 5000 });

    // Navigate to the scene read page.
    await page.goto(`/scenes/${sceneId}`);
    await expect(page.getByRole('heading').filter({ hasText: title })).toBeVisible({
      timeout: 15000,
    });

    // Slug mirrors the app's slugify(): lowercase, non-alphanum runs → '-'.
    const slug = title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');

    // ── Download JSONL ──
    const [downloadJsonl] = await Promise.all([
      page.waitForEvent('download'),
      page.getByRole('button', { name: 'Download scene as JSONL' }).click(),
    ]);
    expect(downloadJsonl.suggestedFilename()).toBe(`${slug}.jsonl`);

    // ── Download Markdown ──
    const [downloadMd] = await Promise.all([
      page.waitForEvent('download'),
      page.getByRole('button', { name: 'Download scene as Markdown' }).click(),
    ]);
    expect(downloadMd.suggestedFilename()).toBe(`${slug}.md`);
  });

  // ── S6: Guest guard — guest login → /scenes redirects to /terminal ────────
  //
  // The scenes +layout.ts calls webCheckSession and throws redirect(302,
  // '/terminal') when session.isGuest is true. This is the client-side
  // convenience guard for INV-SCENE-64.
  test('guest player navigating to /scenes is redirected to /terminal', async ({ page }) => {
    // Enter as guest using the same connectAsGuest flow defined above.
    await page.goto('/');
    await page.getByRole('main').getByRole('button', { name: 'Try as Guest' }).click();
    await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // Direct navigation to /scenes must redirect back to /terminal.
    await page.goto('/scenes');
    await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });

    // Scenes workspace must NOT render.
    await expect(page.locator('[data-testid="scenes-workspace"]')).not.toBeVisible();
  });

  // ── S7: Mobile viewport — sheets open/close; log readable ────────────────
  //
  // At 390×844 the workspace hides the desktop three-pane layout and shows a
  // mobile header bar with two trigger buttons. Each triggers a shadcn Sheet
  // (role=dialog). The log region must remain reachable in the center pane.
  test('mobile viewport sheet triggers open dialogs and log is readable', async ({ page }) => {
    await registerAndEnterTerminal(page, 'mob');
    const title = `MobileTest ${Date.now()}`;
    const sceneId = await createSceneViaTerminal(page, title);

    // Set mobile viewport before navigating so layout renders at mobile size.
    await page.setViewportSize({ width: 390, height: 844 });

    await page.goto(`/scenes?watch=${sceneId}`);
    await expect(page.locator('[data-testid="scenes-workspace"]')).toBeVisible({ timeout: 15000 });

    // Wait for the scene to be selected (title visible in mobile header text).
    await expect(page.locator('text=' + title).first()).toBeVisible({ timeout: 10000 });

    // ── Scene list sheet ──
    const listTrigger = page.getByRole('button', { name: 'Open scene list' });
    await expect(listTrigger).toBeVisible();
    await listTrigger.click();

    // Sheet (role=dialog) opens.
    const listDialog = page.getByRole('dialog').first();
    await expect(listDialog).toBeVisible({ timeout: 5000 });

    // The sheet contains the scene list nav.
    await expect(listDialog.getByRole('navigation', { name: 'Scene list' })).toBeVisible();

    // Dismiss with Escape.
    await page.keyboard.press('Escape');
    await expect(listDialog).not.toBeVisible({ timeout: 5000 });

    // ── Context rail sheet ──
    const contextTrigger = page.getByRole('button', { name: 'Open scene context' });
    await expect(contextTrigger).toBeVisible();
    await contextTrigger.click();

    const contextDialog = page.getByRole('dialog').first();
    await expect(contextDialog).toBeVisible({ timeout: 5000 });

    await page.keyboard.press('Escape');
    await expect(contextDialog).not.toBeVisible({ timeout: 5000 });

    // ── Log region readable ──
    await expect(page.getByRole('log', { name: 'scene log' })).toBeVisible({ timeout: 5000 });
  });

  // ── S8: A11y — structural roles on /scenes ───────────────────────────────
  //
  // A11y path: NO axe/AxeBuilder dependency found in web/package.json or
  // web/e2e/ (confirmed by `rg -ln "axe|AxeBuilder" web/e2e/ web/package.json`
  // returning no matches). Asserting STRUCTURAL roles added in task .8.18:
  //   • role="log" with aria-live="polite" in the center pane
  //   • nav landmark (scene list sidebar)
  //   • main landmark (center pane)
  //   • role="listbox" (roving-tabindex scene list)
  //   • role="option" items within the listbox (when a scene is present)
  test('structural a11y roles are present on the scenes workspace', async ({ page }) => {
    await registerAndEnterTerminal(page, 'a11y');
    const title = `A11yTest ${Date.now()}`;
    const sceneId = await createSceneViaTerminal(page, title);

    await page.goto(`/scenes?watch=${sceneId}`);
    await expect(page.locator('[data-testid="scenes-workspace"]')).toBeVisible({ timeout: 15000 });

    // Wait for the scene to be selected (title visible).
    await expect(page.locator('.font-semibold').filter({ hasText: title })).toBeVisible({
      timeout: 10000,
    });

    // role="log" with aria-live="polite" — the scene log region (center pane).
    const logEl = page.getByRole('log', { name: 'scene log' });
    await expect(logEl).toBeVisible();
    await expect(logEl).toHaveAttribute('aria-live', 'polite');

    // nav landmark — scene list sidebar. Both desktop and mobile navs are in the
    // DOM; at least one must be attached (desktop is hidden md:flex but in DOM).
    const navs = page.getByRole('navigation', { name: 'Scene list' });
    await expect(navs.first()).toBeAttached();

    // main landmark — center pane. The workspace has two <main> elements (the
    // scene-info panel and the center pane); use .first() to avoid strict-mode
    // violation while still asserting at least one main landmark is visible.
    await expect(page.getByRole('main').first()).toBeVisible();

    // role="listbox" — roving-tabindex scene list. Both desktop/mobile share the
    // same listbox role; at least one must be attached.
    const listboxes = page.getByRole('listbox', { name: 'My scenes' });
    await expect(listboxes.first()).toBeAttached();

    // role="option" within the listbox — our scene is listed as an option.
    const options = page.getByRole('option');
    await expect(options.first()).toBeAttached();
  });
});
