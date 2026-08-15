// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// The admin PORTAL's browser-level proofs. This is a NEW file and is
// deliberately not folded into admin.spec.ts, which drives the telnet admin
// COMMANDS (`test.describe('Admin Commands')`) through the terminal and shares
// no surface with this one. authed-shell.spec.ts covers the shipped shell's
// rail, palette and mobile drawer for a NON-admin viewer; what is new here is
// the admin half of that drawer, the admin frame's own bands, the edit Sheet's
// geometry, the mutation loop, and the per-viewer indistinguishability of
// /admin/*.
//
// FOUR THINGS HERE CANNOT BE PROVEN BY A PASSING BUILD OR BY JSDOM:
//   1. the phone band, whose two properties have two independent mechanisms —
//      a Svelte matchMedia derivation for `side` and one @media block for the
//      height and the input font. jsdom applies no media query and computes no
//      layout, so both compile cleanly while dead.
//   2. the client's byte counter agreeing with the server's caps, at BOTH caps.
//   3. the row updating from the mutation response with no table re-read.
//   4. that a non-admin's /admin/* miss is the ordinary not-found.

import { test, expect, registerPlayer, createCharacter } from './helpers/fixtures';
// signInAsAdmin, gotoAdminCharacters, rowFor and sheet were local to this file
// until admin-band-root-font.spec.ts — a second spec, in its own Playwright
// project — needed them. They now live in ./helpers/admin verbatim.
import { signInAsAdmin, gotoAdminCharacters, rowFor, sheet } from './helpers/admin';
import type { Page, BrowserContext } from '@playwright/test';

const overlay = (page: Page) => page.locator('[data-slot="sheet-overlay"]');

/**
 * Tap the row from a cell that is NOT the primary one, and prove the tap
 * landed on the row-spanning target rather than on the cell.
 *
 * THE CELL CHOICE IS THE WHOLE POINT. The phone-band row target is a button in
 * the Name cell whose ::after must span the row; a tap on the Name cell hits
 * that button directly and passes whether or not the overlay spans anything.
 *
 * It goes through `page.mouse` at the Status cell's own centre rather than
 * through `locator.click()`, deliberately. Playwright's actionability check
 * REFUSES to click an element another element covers — and here that cover is
 * exactly the property under test, so `locator.click()` fails on a CORRECT
 * implementation with "the row button intercepts pointer events". A real tap
 * at that point is what a phone does, and `elementFromPoint` says in advance
 * what it will hit.
 */
async function tapRowOutsideNameCell(page: Page, name: string) {
  const cell = rowFor(page, name).locator('td.cell-status');
  await cell.scrollIntoViewIfNeeded();

  // The rect and the hit test are read in ONE evaluate, inside the page, so
  // they are the same viewport coordinates. A Playwright boundingBox is
  // page-relative and disagrees with elementFromPoint's viewport coordinates
  // the moment anything has scrolled — measured: it returned <html>.
  const hit = await cell.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const x = r.x + r.width / 2;
    const y = r.y + r.height / 2;
    const top = document.elementFromPoint(x, y);
    return { x, y, tag: top?.tagName ?? '', cls: top?.className?.toString() ?? '' };
  });

  // The overlay genuinely spans the row: the topmost element over the Status
  // cell is the Name cell's button, not the cell.
  expect(hit.tag).toBe('BUTTON');
  expect(hit.cls).toContain('rowbtn');

  await page.mouse.click(hit.x, hit.y);
  await expect(sheet(page)).toBeVisible({ timeout: 10000 });
}

/** Wait for the detail fetch to populate the form. */
async function awaitSheetReady(page: Page) {
  await expect(sheet(page).locator('[name="concept"]')).toBeVisible({ timeout: 15000 });
}

/**
 * The two row actions are revealed on hover and on focus-within, so a click has
 * to hover first — the same thing a mouse does.
 *
 * The hover and the click are retried TOGETHER: the debounced search settling
 * re-renders the table, which detaches the row mid-gesture and loses the hover,
 * and Playwright's own retry re-clicks without re-hovering.
 */
async function clickRowAction(page: Page, name: string, label: string) {
  await expect(async () => {
    const row = rowFor(page, name);
    await row.hover();
    await row.getByRole('button', { name: label, exact: true }).click({ timeout: 3000 });
  }).toPass({ timeout: 25000 });
}

/**
 * Open the Sheet from the row's primary target.
 *
 * At desktop widths the Name cell's button has no row-spanning ::after — that
 * rule lives inside the phone band — so this is a plain, stable click, and the
 * row-target geometry is proven separately by tapRowOutsideNameCell at 375px
 * where it actually applies.
 */
async function openSheet(page: Page, name: string) {
  await rowFor(page, name).locator('button.rowbtn').click();
  await expect(sheet(page)).toBeVisible({ timeout: 10000 });
  await awaitSheetReady(page);
}

/** The same Sheet, reached through the `Edit` row action instead. */
async function openSheetFromRowAction(page: Page, name: string) {
  await clickRowAction(page, name, 'Edit');
  await expect(sheet(page)).toBeVisible({ timeout: 10000 });
  await awaitSheetReady(page);
}

async function saveSheet(page: Page) {
  await sheet(page).locator('[data-testid="sheet-save"]').click();
}

// ─────────────────────────────────────────────────────────────────────────────
// Proof 1 — the phone band, at 375px.
// ─────────────────────────────────────────────────────────────────────────────

test.describe('admin portal — the phone band', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('collapses both rails, keeps one hamburger, and carries the admin group in the drawer', async ({
    page,
  }) => {
    const admin = await signInAsAdmin(page, 'apa');

    // Both columns are collapsed to zero width — not merely hidden by a
    // parent, which a display:none check could not tell apart from a rule that
    // never matched.
    const railWidth = await page
      .getByTestId('rail')
      .first()
      .evaluate((el) => el.getBoundingClientRect().width);
    expect(railWidth).toBe(0);
    const navWidth = await page
      .locator('.adminnav-col')
      .evaluate((el) => el.getBoundingClientRect().width);
    expect(navWidth).toBe(0);

    // The breadcrumb strip owned by admin/+layout.svelte renders, and it
    // carries NO hamburger of its own: the drawer is opened by the control the
    // top bar already ships. Two controls opening one drawer, inches apart, is
    // worse than none — so there must be exactly one.
    await expect(page.locator('.mobilebar')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Open navigation' })).toHaveCount(1);

    // The drawer carries BOTH groups, and the admin one is NAVIGABLE. An
    // assertion that only checked the mobilebar and the zero widths passes
    // while the Admin group is entirely absent.
    await page.getByRole('button', { name: 'Open navigation' }).click();
    const drawer = page.getByRole('dialog', { name: 'Navigation' });
    await expect(drawer).toBeVisible();
    await expect(drawer.getByText('Workspace', { exact: true })).toBeVisible();
    await expect(drawer.getByText('Admin', { exact: true }).first()).toBeVisible();

    const entries = drawer.locator('.rail-admin-item');
    await expect(entries.first()).toBeVisible();
    const entryCount = await entries.count();
    expect(entryCount).toBeGreaterThan(1);

    const firstHref = await entries.nth(0).getAttribute('href');
    await entries.nth(0).click();
    await expect(page).toHaveURL(new RegExp(`${firstHref}$`));

    await page.getByRole('button', { name: 'Open navigation' }).click();
    const drawer2 = page.getByRole('dialog', { name: 'Navigation' });
    const secondHref = await drawer2.locator('.rail-admin-item').nth(1).getAttribute('href');
    expect(secondHref).not.toBe(firstHref);
    await drawer2.locator('.rail-admin-item').nth(1).click();
    await expect(page).toHaveURL(new RegExp(`${secondHref}$`));

    // Off /admin/*, the group is gone: adminNavStore clears on the admin
    // layout's teardown, which is the half no component test can reach.
    await page.goto('/characters');
    await expect(page.getByTestId('roster-card').first()).toBeVisible({ timeout: 15000 });
    await page.getByRole('button', { name: 'Open navigation' }).click();
    const drawer3 = page.getByRole('dialog', { name: 'Navigation' });
    await expect(drawer3).toBeVisible();
    await expect(drawer3.locator('.rail-admin-item')).toHaveCount(0);

    expect(admin.charName.length).toBeGreaterThan(0);
  });

  test('drops Created and Ver, and the row tap target opens the Sheet from a non-primary cell', async ({
    page,
  }) => {
    const admin = await signInAsAdmin(page, 'apb');

    await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Created' })).not.toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Ver' })).not.toBeVisible();

    await tapRowOutsideNameCell(page, admin.charName);
  });

  test('renders a real bottom sheet: data-side, 84vh, 16px inputs, full-viewport overlay', async ({
    page,
  }) => {
    const admin = await signInAsAdmin(page, 'apc');
    await tapRowOutsideNameCell(page, admin.charName);
    await awaitSheetReady(page);

    // MECHANISM ONE — the Svelte matchMedia derivation. CSS cannot produce
    // this: `side` is a prop emitted as data-side.
    await expect(sheet(page)).toHaveAttribute('data-side', 'bottom');

    // MECHANISM TWO — the one @media block. Independent of the above, which is
    // why each has its own demonstrated RED.
    const geometry = await sheet(page).evaluate((el) => ({
      height: el.getBoundingClientRect().height,
      viewport: window.innerHeight,
    }));
    expect(Math.abs(geometry.height - 0.84 * geometry.viewport)).toBeLessThanOrEqual(2);

    const fontSize = await sheet(page)
      .locator('[name="concept"]')
      .evaluate((el) => window.getComputedStyle(el).fontSize);
    expect(fontSize).toBe('16px');

    // The overlay covers the FULL viewport, top bar included. The Sheet is not
    // portalled anywhere, so its fixed geometry resolves against the viewport;
    // a containing block below the top bar would leave that band undimmed and
    // clickable behind an open modal.
    const rect = await overlay(page).evaluate((el) => {
      const r = el.getBoundingClientRect();
      return { top: r.top, height: r.height, viewport: window.innerHeight };
    });
    expect(Math.abs(rect.top)).toBeLessThanOrEqual(1);
    expect(Math.abs(rect.height - rect.viewport)).toBeLessThanOrEqual(1);

    // …and the Sheet is a direct child of document.body, NOT of the shell.
    const placement = await sheet(page).evaluate((el) => ({
      inBody: el.parentElement === document.body || document.body.contains(el),
      inShell: !!document.querySelector('.shell')?.contains(el),
    }));
    expect(placement.inBody).toBe(true);
    expect(placement.inShell).toBe(false);

    // A click inside the 44px top-bar band dismisses the Sheet rather than
    // reaching the top bar.
    await page.mouse.click(200, 10);
    await expect(sheet(page)).toHaveCount(0, { timeout: 10000 });
  });

  test('closes on Escape, on the close control and on Cancel', async ({ page }) => {
    const admin = await signInAsAdmin(page, 'apd');

    await tapRowOutsideNameCell(page, admin.charName);
    await page.keyboard.press('Escape');
    await expect(sheet(page)).toHaveCount(0, { timeout: 10000 });

    await tapRowOutsideNameCell(page, admin.charName);
    await sheet(page).locator('[data-slot="sheet-close"]').click();
    await expect(sheet(page)).toHaveCount(0, { timeout: 10000 });

    await tapRowOutsideNameCell(page, admin.charName);
    await sheet(page).locator('[data-testid="sheet-cancel"]').click();
    await expect(sheet(page)).toHaveCount(0, { timeout: 10000 });
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// Proof 1b — the 768–815px band, untested at every viewport the plans named.
// ─────────────────────────────────────────────────────────────────────────────

test.describe('admin portal — the 780px band', () => {
  test.use({ viewport: { width: 780, height: 900 } });

  test('keeps a non-zero rail, merges the admin nav into a rail-width strip, and keeps the Sheet on the right', async ({
    page,
  }) => {
    const admin = await signInAsAdmin(page, 'bpa');

    const rail = page.getByTestId('rail').first();
    await expect(rail).toBeVisible();
    const railWidth = await rail.evaluate((el) => el.getBoundingClientRect().width);
    expect(railWidth).toBeGreaterThan(0);

    // The admin nav sits adjacent at the SAME width, so the two read as one
    // continuous column. This is the band where a mechanism mismatch between
    // the shipped rail's @media and the admin shell's own rules shows up first.
    const navWidth = await page
      .locator('.adminnav-col')
      .evaluate((el) => el.getBoundingClientRect().width);
    expect(navWidth).toBeGreaterThan(0);
    expect(Math.abs(navWidth - railWidth)).toBeLessThanOrEqual(1);

    // The rail's Admin entry carries its context treatment on an admin route.
    await expect(rail.locator('a.rail-btn.is-context')).toHaveCount(1);

    // The section entries are still there and still navigable at this band —
    // narrowed, not hidden.
    const entries = page.locator('.adminnav-col a[href^="/admin/"]');
    expect(await entries.count()).toBeGreaterThan(1);
    const href = await entries.nth(1).getAttribute('href');
    await entries.nth(1).click();
    await expect(page).toHaveURL(new RegExp(`${href}$`));

    // Reached through the `Edit` ROW ACTION here — the affordance the table
    // reveals on hover — rather than the primary cell target the other blocks
    // use, so both entrances to the Sheet are exercised in a real browser.
    await gotoAdminCharacters(page, admin.charName);
    await openSheetFromRowAction(page, admin.charName);
    await expect(sheet(page)).toHaveAttribute('data-side', 'right');
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// Proof 1c — the BOUNDARY itself, at 767/768 and 1023/1024.
//
// The in-band proofs above (375, 780, 1280) each sit comfortably inside one
// band. This block sits ON the edges, which is the only place the two
// independent mechanisms can be observed disagreeing: the CSS half now reads
// Tailwind's --breakpoint-md/-lg tokens and the JS half reads the shared hook's
// DESKTOP_MEDIA_QUERY, and nothing but a browser can tell you they still fire
// at the same width.
//
// It is a REGRESSION DETECTOR, not a reproduction: the pre-fix literals already
// agreed at integer widths, so this block was green before the conversion too.
// Its teeth were shown by mutating DESKTOP_MEDIA_QUERY and by mutating
// --breakpoint-md, each of which makes the first test below fail.
//
// It does NOT cover the fractional-width case (767.5px) that the range form
// actually buys — Playwright cannot set a fractional viewport. That improvement
// is reasoned about in the plan and in EditCharacterSheet.svelte, not asserted.
// ─────────────────────────────────────────────────────────────────────────────

test.describe('admin portal — the band boundaries', () => {
  // Sign in at a desktop width and resize inside each test.
  test.use({ viewport: { width: 1280, height: 900 } });

  // `:not(.is-drawer)` is load-bearing: the drawer carries the same test id
  // and is full-width when open.
  const RAIL = '[data-testid="rail"]:not(.is-drawer)';
  const NAV_COL = '.adminnav-col';

  /**
   * Read BOTH column widths in ONE evaluate.
   *
   * One evaluate, not two, because every assertion below compares the two
   * columns to each other (`Math.abs(nav - rail) <= 1`, `full.nav > full.rail`).
   * Two separate reads can land in two different animation frames, at which
   * point the comparison is between two different moments and a passing layout
   * can read as a divergence.
   *
   * A missing element reports -1 rather than throwing, so its absence fails the
   * assertion that names the column instead of the measurement helper.
   */
  async function readColumns(page: Page) {
    return page.evaluate(
      ([railSel, navSel]) => {
        const width = (sel: string) => {
          const el = document.querySelector(sel);
          return el === null ? -1 : el.getBoundingClientRect().width;
        };
        return { rail: width(railSel), nav: width(navSel) };
      },
      [RAIL, NAV_COL] as const,
    );
  }

  /**
   * Set the viewport and return both column widths, sampled once they have
   * stopped moving.
   *
   * Both measured columns carry `transition: width 180ms ease`
   * (SectionRail.svelte, admin/+layout.svelte:119). This used to wait a fixed
   * 300ms — a wall-clock guess with 120ms of headroom over a 180ms animation,
   * which under CI load reads mid-animation and fails looking exactly like a
   * real divergence. It already invalidated one UAT pass.
   *
   * Settling is now OBSERVED: poll the pair until two consecutive samples are
   * identical, then take one final paired sample. The poll decides only WHEN to
   * measure, never WHAT the answer should be — the expectations below still do
   * all the judging, so a slower transition costs time rather than correctness
   * and a genuinely wrong width still fails.
   */
  async function columnsAt(page: Page, width: number) {
    await page.setViewportSize({ width, height: 900 });

    let previous = '';
    await expect
      .poll(
        async () => {
          const current = JSON.stringify(await readColumns(page));
          const stable = previous !== '' && current === previous;
          previous = current;
          return stable;
        },
        {
          message:
            `the rail and admin nav column widths never stopped changing at ${width}px — ` +
            'a transition that never completes, or a layout thrashing between two states',
          intervals: [50, 50, 100, 100, 250],
          timeout: 10_000,
        },
      )
      .toBe(true);

    return readColumns(page);
  }

  test('collapses both columns at 767 and restores both at 768, and the Sheet flips with them', async ({
    page,
  }) => {
    const admin = await signInAsAdmin(page, 'bnd');

    // Resizing an OPEN Sheet is deliberate: it exercises the live listener, and
    // it avoids the phone-band row-overlay tap target, which is proven at 375px.
    await openSheetFromRowAction(page, admin.charName);
    await expect(sheet(page)).toHaveAttribute('data-side', 'right');

    const wide = await columnsAt(page, 768);
    expect(wide.rail).toBeGreaterThan(0);
    expect(wide.nav).toBeGreaterThan(0);
    expect(Math.abs(wide.nav - wide.rail)).toBeLessThanOrEqual(1);
    await expect(sheet(page)).toHaveAttribute('data-side', 'right');
    const wideGeometry = await sheet(page).evaluate((el) => ({
      height: el.getBoundingClientRect().height,
      viewport: window.innerHeight,
    }));
    expect(Math.abs(wideGeometry.height - 0.84 * wideGeometry.viewport)).toBeGreaterThan(2);

    const narrow = await columnsAt(page, 767);
    // One expect per column: "one collapsed and the other did not" is the exact
    // divergence this block closes, so the failure must name which one.
    expect(narrow.rail, 'the persistent rail must be zero-width at 767').toBe(0);
    expect(narrow.nav, 'the admin nav column must be zero-width at 767').toBe(0);
    await expect(sheet(page)).toHaveAttribute('data-side', 'bottom');
    const narrowGeometry = await sheet(page).evaluate((el) => ({
      height: el.getBoundingClientRect().height,
      viewport: window.innerHeight,
    }));
    expect(Math.abs(narrowGeometry.height - 0.84 * narrowGeometry.viewport)).toBeLessThanOrEqual(2);
  });

  test('merges the nav into a rail-width strip at 1023 and restores the full nav at 1024', async ({
    page,
  }) => {
    await signInAsAdmin(page, 'lgb');

    const merged = await columnsAt(page, 1023);
    expect(merged.rail).toBeGreaterThan(0);
    expect(merged.nav).toBeGreaterThan(0);
    expect(Math.abs(merged.nav - merged.rail)).toBeLessThanOrEqual(1);

    const full = await columnsAt(page, 1024);
    // The claim is the RELATION between the two columns. Pinning 48 and 232 here
    // would duplicate --rail-w and --adminnav-w into a third place, which is the
    // defect this work removes.
    expect(full.rail).toBe(merged.rail);
    expect(full.nav).toBeGreaterThan(full.rail);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// Proof 2 — the byte caps, at BOTH of them. The admin Sheet is a SECOND editor
// for these thirteen paths, so this proof is its own rather than folded into
// Phase 5's, which tests a different component against a different surface.
// ─────────────────────────────────────────────────────────────────────────────

test.describe('admin portal — the client counter agrees with the server at both caps', () => {
  test.use({ viewport: { width: 1280, height: 900 } });

  /** Type a value into a field, save, and report what the Sheet did. */
  async function saveField(page: Page, name: string, value: string) {
    await sheet(page).locator(`[name="${name}"]`).fill(value);
    // The edit registered before the save is attempted: without this a
    // disabled Save reads as a save failure rather than as a lost draft.
    await expect(sheet(page).locator('[data-testid="mask-footer"]')).toContainText(
      'update_mask: 1 paths',
    );
    await saveSheet(page);
  }

  test('accepts 99 and 100 bytes on a short-cap field and surfaces the refusal at 101', async ({
    page,
  }) => {
    const admin = await signInAsAdmin(page, 'cpa');

    for (const n of [99, 100]) {
      await openSheet(page, admin.charName);
      await saveField(page, 'concept', 'a'.repeat(n));
      // The Sheet closes only on success.
      await expect(sheet(page)).toHaveCount(0, { timeout: 15000 });
    }

    await openSheet(page, admin.charName);
    const counter = sheet(page).locator('[data-counter-for="profile.concept"]');
    await sheet(page).locator('[name="concept"]').fill('a'.repeat(101));
    await expect(counter).toHaveAttribute('data-over', 'true');
    await expect(counter).toContainText('101 of 100');
    // The client does NOT refuse: the server is the enforcer, and its refusal
    // is what this proof observes.
    await expect(sheet(page).locator('[data-testid="sheet-save"]')).toBeEnabled();
    await saveSheet(page);
    await expect(sheet(page)).toContainText("Couldn't save. Try again.", { timeout: 15000 });
    await expect(sheet(page)).toBeVisible();
  });

  test('accepts 3999, 4000 and a mere 101 bytes on a long-cap field and refuses 4001', async ({
    page,
  }) => {
    const admin = await signInAsAdmin(page, 'cpb');

    // 101 bytes is ACCEPTED here. Asserting otherwise would demand behaviour
    // the server correctly declines to exhibit: the thirteen paths do not
    // share one cap.
    for (const n of [101, 3999, 4000]) {
      await openSheet(page, admin.charName);
      await saveField(page, 'biography', 'b'.repeat(n));
      await expect(sheet(page)).toHaveCount(0, { timeout: 20000 });
    }

    await openSheet(page, admin.charName);
    const counter = sheet(page).locator('[data-counter-for="profile.biography"]');
    await sheet(page).locator('[name="biography"]').fill('b'.repeat(4001));
    await expect(counter).toHaveAttribute('data-over', 'true');
    await expect(counter).toContainText('4001 of 4000');
    await saveSheet(page);
    await expect(sheet(page)).toContainText("Couldn't save. Try again.", { timeout: 20000 });
  });

  test('measures CJK in bytes, so each field goes over at a rune count well under its cap', async ({
    page,
  }) => {
    const admin = await signInAsAdmin(page, 'cpc');

    // 34 codepoints × 3 UTF-8 bytes = 102 > 100. A rune counter reads 34 and
    // calls this comfortably short.
    await openSheet(page, admin.charName);
    const shortCounter = sheet(page).locator('[data-counter-for="profile.concept"]');
    await sheet(page).locator('[name="concept"]').fill('三'.repeat(34));
    await expect(shortCounter).toContainText('102 of 100');
    await expect(shortCounter).toHaveAttribute('data-over', 'true');
    await saveSheet(page);
    await expect(sheet(page)).toContainText("Couldn't save. Try again.", { timeout: 15000 });
    await sheet(page).locator('[data-testid="sheet-cancel"]').click();
    await expect(sheet(page)).toHaveCount(0, { timeout: 10000 });

    // 1334 codepoints × 3 = 4002 > 4000.
    await openSheet(page, admin.charName);
    const longCounter = sheet(page).locator('[data-counter-for="profile.biography"]');
    await sheet(page).locator('[name="biography"]').fill('三'.repeat(1334));
    await expect(longCounter).toContainText('4002 of 4000');
    await expect(longCounter).toHaveAttribute('data-over', 'true');
    await saveSheet(page);
    await expect(sheet(page)).toContainText("Couldn't save. Try again.", { timeout: 20000 });
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// Proof 3 — the mutation loop over the real stack.
// ─────────────────────────────────────────────────────────────────────────────

test.describe('admin portal — D-110 over the real stack', () => {
  test.use({ viewport: { width: 1280, height: 900 } });

  test('closes the Sheet, updates the row from the response with no list re-read, and fires one receipt', async ({
    page,
  }) => {
    const admin = await signInAsAdmin(page, 'dpa');

    let listCalls = 0;
    page.on('request', (req) => {
      if (req.url().includes('WebAdminListCharacters') || req.url().includes('WebAdminSearchCharacters')) {
        listCalls += 1;
      }
    });

    await openSheet(page, admin.charName);
    const before = listCalls;
    await sheet(page).locator('[name="concept"]').fill('Wandering archivist');
    await expect(sheet(page).locator('[data-testid="mask-footer"]')).toContainText(
      'update_mask: 1 paths',
    );
    await saveSheet(page);

    await expect(sheet(page)).toHaveCount(0, { timeout: 15000 });
    // Not a re-read: an assertion that the row merely changed would pass under
    // one.
    expect(listCalls - before).toBe(0);

    const toast = page.locator('[data-sonner-toast]');
    await expect(toast).toHaveCount(1);
    await expect(toast).toContainText('AdminUpdateCharacter');
    await expect(toast).toContainText('update_mask: 1 paths');
  });

  test('keeps the Sheet open with its typed text on a version conflict, and fires no receipt', async ({
    page,
    context,
  }: {
    page: Page;
    context: BrowserContext;
  }) => {
    const admin = await signInAsAdmin(page, 'dpb');

    // Open the Sheet, which composes against the version it reads now.
    await openSheet(page, admin.charName);
    await sheet(page).locator('[name="concept"]').fill('My draft, still mine');
    // The draft is real before the out-of-band write happens: without this the
    // save below can fail as "Save is disabled" and look like a conflict bug.
    await expect(sheet(page).locator('[data-testid="mask-footer"]')).toContainText(
      'update_mask: 1 paths',
    );

    // A SECOND TAB in the same session moves the row underneath it. This is
    // the honest shape of "someone else edited this character": a real write
    // through the real RPC, not a hand-poked column.
    const other = await context.newPage();
    await other.setViewportSize({ width: 1280, height: 900 });
    await gotoAdminCharacters(other, admin.charName);
    await openSheet(other, admin.charName);
    await other.locator('[data-slot="sheet-content"] [name="species"]').fill('Elsewhere');
    await other.locator('[data-slot="sheet-content"] [data-testid="sheet-save"]').click();
    await expect(other.locator('[data-slot="sheet-content"]')).toHaveCount(0, { timeout: 15000 });
    await other.close();

    await saveSheet(page);

    // The Sheet stayed open, the typing survived, both versions are named, and
    // no receipt fired — nothing finished.
    await expect(sheet(page).locator('[role="alert"]')).toBeVisible({ timeout: 15000 });
    await expect(sheet(page).locator('[role="alert"]')).toContainText('were not applied');
    await expect(sheet(page).locator('[name="concept"]')).toHaveValue('My draft, still mine');
    await expect(page.locator('[data-sonner-toast]')).toHaveCount(0);
  });

  test('retires through the confirm and un-retires from the receipt’s Undo', async ({ page }) => {
    const admin = await signInAsAdmin(page, 'dpc');

    await clickRowAction(page, admin.charName, 'Retire…');
    const confirm = page.locator('[data-slot="alert-dialog-content"]');
    await expect(confirm).toBeVisible();
    await expect(confirm).toHaveAttribute('role', 'alertdialog');
    // All four D-108 clauses.
    await expect(confirm).toContainText('out of active play');
    await expect(confirm).toContainText('does not hide them');
    await expect(confirm).toContainText('name stays reserved');
    await expect(confirm).toContainText('undo this at any time');
    // Initial focus is Cancel, never the destructive confirm.
    await expect(confirm.locator('[data-slot="alert-dialog-cancel"]')).toBeFocused();

    // A backdrop click does NOT dismiss it — the assertion jsdom could not
    // make, because a synthetic pointerdown does not reach bits-ui's outside
    // detection.
    await page.mouse.click(5, 400);
    await expect(confirm).toBeVisible();

    await confirm.locator('[data-testid="lifecycle-confirm"]').click();
    await expect(confirm).toHaveCount(0, { timeout: 15000 });
    await expect(rowFor(page, admin.charName)).toContainText('retired');

    const toast = page.locator('[data-sonner-toast]');
    await expect(toast).toContainText('AdminRetireCharacter');
    await toast.getByRole('button', { name: 'Undo' }).click();

    // Undo sends the un-retire RPC, never a status value.
    await expect(rowFor(page, admin.charName)).toContainText('active', { timeout: 15000 });
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// Proof 4 — indistinguishability at PAGE level, and only page level.
//
// ⚠ SCOPE, STATED SO NOBODY "STRENGTHENS" IT INTO AN ASSERTION THAT CANNOT
// PASS. The assertions below are scoped INSIDE the +error.svelte boundary's own
// subtree and deliberately do not touch the surrounding chrome, because the
// chrome genuinely differs and the difference is structural: SvelteKit renders
// every layout ABOVE the failed segment, so a denial thrown in admin/+layout.ts
// renders inside (authed)/+layout.svelte's shell and rail — that layout's own
// load succeeded — while an unknown /c/[id] sits outside (authed) per D-85 and
// renders bare. For ONE logged-in non-admin, two of the four miss kinds
// therefore carry shell chrome and two do not.
//
// That residual is ACCEPTED, not hidden: it is the `verification: backstop`
// truth in 06.1-04-PLAN.md's must_haves that names the mechanism and says why
// closing it is out of this phase's budget (it would mean either routing every
// miss through a chrome-less boundary or rendering shell chrome on a route the
// viewer has no session for).
// ─────────────────────────────────────────────────────────────────────────────

test.describe('admin portal — a non-admin cannot tell an /admin miss from any other miss', () => {
  test.use({ viewport: { width: 1280, height: 900 } });

  test('renders the ordinary not-found for every /admin path, with no path echo and no permission vocabulary', async ({
    page,
  }) => {
    // A registered player with a character and NO admin role.
    const creds = await registerPlayer(page, 'epa');
    await createCharacter(page, creds.charName);

    const paths = ['/admin', '/admin/characters', '/admin/moderation'];
    const renderings: string[] = [];

    for (const path of paths) {
      await page.goto(path);
      const boundary = page.locator('.notfound');
      await expect(boundary).toBeVisible({ timeout: 15000 });
      await expect(boundary.getByRole('heading', { name: 'Not found' })).toBeVisible();
      await expect(boundary).toContainText("We couldn't find that page.");

      const rendered = ((await boundary.textContent()) ?? '').replace(/\s+/g, ' ').trim();
      expect(rendered).not.toMatch(
        /admin|moderation|permission|denied|forbidden|unauthoriz|not allowed|403|401/i,
      );
      renderings.push(rendered);
    }

    // A genuinely unknown path under the same layout renders the same page.
    await page.goto('/admin/definitely-not-a-section');
    await expect(page.locator('.notfound')).toBeVisible({ timeout: 15000 });
    renderings.push(((await page.locator('.notfound').textContent()) ?? '').replace(/\s+/g, ' ').trim());

    // BY SET EQUALITY, not by four separate contains-checks: the renderer does
    // not branch on which kind of miss occurred, so every one of these is the
    // same string. The negative control for this assertion — making the admin
    // denial render a distinct string and watching it fail — is recorded in
    // 06.1-04-SUMMARY.md, because an opacity test whose green has never been
    // earned is not evidence.
    expect(new Set(renderings).size).toBe(1);
  });
});
