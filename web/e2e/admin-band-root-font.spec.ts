// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// WHAT THIS BUYS THAT NOTHING ELSE DOES.
//
// The phone band has two halves: sixteen authored `@media` rules that read
// `theme(--breakpoint-md)`, and one JS media query, DESKTOP_MEDIA_QUERY, that
// every consumer reaches through `isDesktop()`. Both are now written in rem, so
// they resolve against the SAME reference — the browser's initial font size.
//
// That reference is the one axis along which the two halves can disagree, and
// every other proof in this tree is blind to it: `admin-portal.spec.ts`'s
// 767/768/1023/1024 boundary sweep, the five census RED-proofs, and the whole
// vitest suite all run at the default 16px root font, where rem and px are
// numerically identical. The unit split this spec was written to catch —
// DESKTOP_MEDIA_QUERY as `(min-width: 768px)` against a CSS half compiled to
// `48rem` — is INVISIBLE at 16px and produces, at 20px, a phone-shaped shell
// wearing a desktop side sheet anywhere in [768, 960).
//
// It therefore runs in its own Playwright project, `chromium-large-font`,
// launched with `--blink-settings=defaultFontSize=20`. The 20px root font size
// is asserted IN PAGE before any complement assertion runs: if that launch flag
// ever stops applying, this spec must fail rather than quietly return to the
// 16px blind spot it exists to leave.

import { readFileSync } from 'node:fs';
import { test, expect, signInAsAdmin, rowFor, sheet } from './helpers/admin';
import type { Page } from '@playwright/test';

/**
 * DESKTOP_MEDIA_QUERY, read out of the hook's SOURCE TEXT.
 *
 * The module is a `.svelte.ts` carrying runes and does not import under plain
 * Node, so the constant is parsed rather than imported. The parse is asserted
 * below before it is used: an unparsed constant would leave every assertion
 * here comparing a query against itself, which is the vacuous-pass shape this
 * whole spec exists to avoid.
 */
function readDesktopMediaQuery(): string {
  const src = readFileSync(new URL('../src/lib/hooks/mediaQuery.svelte.ts', import.meta.url), 'utf8');
  const m = src.match(/export const DESKTOP_MEDIA_QUERY = '([^']+)';/);
  // A plain throw, not an expect(): this runs at module load, before any test
  // exists to attribute a failure to, and a thrown error there fails the file
  // loudly. Silence here would be the worst outcome available.
  if (m === null) {
    throw new Error(
      'could not parse DESKTOP_MEDIA_QUERY out of web/src/lib/hooks/mediaQuery.svelte.ts — ' +
        'without it every assertion in this spec would compare a query against itself and pass vacuously',
    );
  }
  const value = m[1].trim();
  if (value.length === 0) {
    throw new Error('DESKTOP_MEDIA_QUERY parsed as an empty string');
  }
  return value;
}

const DESKTOP_MEDIA_QUERY = readDesktopMediaQuery();

/**
 * Read one element's laid-out width, polling until it settles.
 *
 * Both measured columns carry `transition: width 180ms ease`
 * (SectionRail.svelte, admin/+layout.svelte). Absorbing that in a RETRYING
 * assertion — `expect.poll`, below — rather than in a fixed wall-clock sleep is
 * what makes the proof both faster on a quick box and robust on a slow one.
 * Plan 06.1-08 adopts this shape for `admin-portal.spec.ts`'s `BAND_SETTLE_MS`,
 * which this spec deliberately does not copy.
 */
function widthOf(page: Page, selector: string) {
  return page.locator(selector).first().evaluate((el) => el.getBoundingClientRect().width);
}

/** `:not(.is-drawer)` is load-bearing: the drawer carries the same test id and is full-width when open. */
const RAIL = '[data-testid="rail"]:not(.is-drawer)';
const NAV_COL = '.adminnav-col';

test.describe('the phone band at a 20px root font size', () => {
  test.use({ viewport: { width: 1280, height: 900 } });

  test('the JS query and the CSS token are exact complements at a 20px root font', async ({
    page,
  }) => {
    await page.goto('/');

    const env = await page.evaluate(() => {
      const cs = getComputedStyle(document.documentElement);
      return {
        rootFontSize: parseFloat(cs.fontSize),
        token: cs.getPropertyValue('--breakpoint-md').trim(),
      };
    });

    // FIRST, and before anything downstream depends on it: the harness itself.
    expect(
      env.rootFontSize,
      'the chromium-large-font project must run at a 20px root font size — if this reads 16 the ' +
        '--blink-settings launch arg stopped applying and this spec has silently become a ' +
        'duplicate of the 16px sweep it exists to complement',
    ).toBe(20);

    // The token IS emitted to :root,:host by the build. An earlier census
    // comment asserted the opposite; this assertion is what keeps that
    // correction honest.
    expect(
      env.token,
      '--breakpoint-md must be readable off :root at runtime and expressed in rem',
    ).toMatch(/^[\d.]+rem$/);

    for (const width of [640, 767, 768, 900, 959, 960, 961, 1024]) {
      await page.setViewportSize({ width, height: 900 });
      const seen = await page.evaluate(
        ([q, token]) => ({
          js: window.matchMedia(q).matches,
          atOrAbove: window.matchMedia('(width >= ' + token + ')').matches,
          below: window.matchMedia('(width < ' + token + ')').matches,
        }),
        [DESKTOP_MEDIA_QUERY, env.token] as const,
      );

      expect(
        seen.js,
        `at width ${width} the JS query ${DESKTOP_MEDIA_QUERY} and the CSS token form ` +
          `(width >= ${env.token}) disagree — the two halves of the phone band are not in one unit`,
      ).toBe(seen.atOrAbove);

      expect(
        seen.js,
        `at width ${width} the JS query ${DESKTOP_MEDIA_QUERY} is not the exact complement of ` +
          `(width < ${env.token})`,
      ).toBe(!seen.below);
    }
  });

  test('the shell columns and the Sheet agree inside the band the token decides', async ({
    page,
  }) => {
    const admin = await signInAsAdmin(page, 'rfs');

    await rowFor(page, admin.charName).locator('button.rowbtn').click();
    await expect(sheet(page)).toBeVisible();

    // 900px is INSIDE the band at a 20px root font (the CSS boundary sits at
    // 960px) and OUTSIDE it at 16px (where it sits at 768px). That is exactly
    // why this project exists: before the fix, the shell collapsed here while
    // the Sheet stayed on its desktop side.
    // One poll per column: "one collapsed and the other did not" is the exact
    // divergence this proof closes, so a failure must name which one.
    const inBandRail = 'at width 900 with a 20px root font the persistent rail must be zero-width';
    const inBandNav = 'at width 900 with a 20px root font the admin nav column must be zero-width';

    await page.setViewportSize({ width: 900, height: 900 });
    await expect.poll(() => widthOf(page, RAIL), { message: inBandRail }).toBe(0);
    await expect.poll(() => widthOf(page, NAV_COL), { message: inBandNav }).toBe(0);
    await expect(
      sheet(page),
      'at width 900 the shell is in the phone band, so the edit Sheet must be the bottom sheet',
    ).toHaveAttribute('data-side', 'bottom');

    const aboveRail = 'at width 1000 with a 20px root font the persistent rail must be drawn';
    const aboveNav = 'at width 1000 with a 20px root font the admin nav column must be drawn';

    await page.setViewportSize({ width: 1000, height: 900 });
    await expect.poll(() => widthOf(page, RAIL), { message: aboveRail }).toBeGreaterThan(0);
    await expect.poll(() => widthOf(page, NAV_COL), { message: aboveNav }).toBeGreaterThan(0);
    await expect(
      sheet(page),
      'at width 1000 the shell is above the band, so the edit Sheet must be the side sheet',
    ).toHaveAttribute('data-side', 'right');
  });
});
