// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect, db } from './helpers/fixtures';

// Content bootstrap on first boot may take a few extra seconds.
const CONTENT_TIMEOUT = 15000;

test.describe('Landing Page — Content', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
  });

  test('hero section displays "The Crossroads" as title', async ({ page }) => {
    await expect(page.getByTestId('hero-title')).toHaveText('The Crossroads', {
      timeout: CONTENT_TIMEOUT,
    });
  });

  test('hero section displays tagline "Where worlds collide"', async ({ page }) => {
    await expect(page.getByTestId('hero-tagline')).toHaveText('Where worlds collide', {
      timeout: CONTENT_TIMEOUT,
    });
  });

  test('pitch section contains rift/door narrative text', async ({ page }) => {
    const pitch = page.getByTestId('pitch');
    await expect(pitch).toBeVisible({ timeout: CONTENT_TIMEOUT });
    // The crossroads plugin supplies copy about rifts or doors opening without warning.
    const text = await pitch.textContent();
    const hasRiftText =
      text?.toLowerCase().includes('door') ||
      text?.toLowerCase().includes('rift') ||
      text?.toLowerCase().includes('without warning');
    expect(hasRiftText, `Pitch text was: ${text}`).toBe(true);
  });

  test('all four feature cards are visible with correct titles', async ({ page }) => {
    const expectedTitles = [
      'Collaborative Storytelling',
      'Any Character, Any World',
      'Web & Telnet',
      'Build Your Corner',
    ];

    const grid = page.getByTestId('feature-grid');
    await expect(grid).toBeVisible({ timeout: CONTENT_TIMEOUT });

    for (const title of expectedTitles) {
      await expect(grid.getByRole('heading', { name: title })).toBeVisible();
    }
  });

  test('feature cards appear in correct order (1, 2, 3, 4)', async ({ page }) => {
    const grid = page.getByTestId('feature-grid');
    await expect(grid).toBeVisible({ timeout: CONTENT_TIMEOUT });

    const headings = grid.getByRole('heading');
    const count = await headings.count();
    expect(count).toBe(4);

    const expectedOrder = [
      'Collaborative Storytelling',
      'Any Character, Any World',
      'Web & Telnet',
      'Build Your Corner',
    ];

    for (let i = 0; i < expectedOrder.length; i++) {
      await expect(headings.nth(i)).toHaveText(expectedOrder[i]);
    }
  });

  test('feature card bodies are non-empty', async ({ page }) => {
    const grid = page.getByTestId('feature-grid');
    await expect(grid).toBeVisible({ timeout: CONTENT_TIMEOUT });

    const cards = grid.locator('.feature-card');
    const count = await cards.count();
    expect(count).toBe(4);

    for (let i = 0; i < count; i++) {
      const card = cards.nth(i);
      const text = await card.textContent();
      // Text content includes the heading; strip heading to check body is non-empty.
      const heading = await card.getByRole('heading').textContent();
      const body = text?.replace(heading ?? '', '').trim();
      expect(body?.length ?? 0, `Card ${i} body was empty`).toBeGreaterThan(0);
    }
  });

  test('connect section is visible', async ({ page }) => {
    await expect(page.getByTestId('connect')).toBeVisible({ timeout: CONTENT_TIMEOUT });
  });
});

test.describe('Landing Page — Content from DB', () => {
  test('hero and pitch content matches content_items in database', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByTestId('hero-title')).toBeVisible({ timeout: CONTENT_TIMEOUT });

    // DB: content_items table has entries for the landing page
    const items = await db.getContentItemsByPrefix('landing.');
    expect(items.length, 'Expected landing.* content items in DB').toBeGreaterThan(0);

    // Hero content: single landing.hero item, title/tagline in metadata
    const hero = items.find((i) => i.key === 'landing.hero');
    expect(hero, 'Expected landing.hero content item in DB').toBeDefined();
    const heroMeta = hero!.metadata as Record<string, string>;

    const displayedTitle = await page.getByTestId('hero-title').textContent();
    expect(displayedTitle?.trim()).toBe(heroMeta.title);

    const displayedTagline = await page.getByTestId('hero-tagline').textContent();
    expect(displayedTagline?.trim()).toBe(heroMeta.tagline);

    // Pitch content: landing.pitch item, rendered from markdown body
    const pitch = items.find((i) => i.key === 'landing.pitch');
    expect(pitch, 'Expected landing.pitch content item in DB').toBeDefined();
    const displayedPitch = await page.getByTestId('pitch').textContent();
    // Strip markdown heading syntax (# Title) to get plain text for comparison.
    // The browser renders markdown → HTML, so headings become <h1>, etc.
    const plainBody = pitch!.body.replace(/^#+\s+.*\n/gm, '').trim();
    expect(displayedPitch).toContain(plainBody.slice(0, 40));
  });

  test('feature card titles and count match content_items in database', async ({ page }) => {
    await page.goto('/');
    const grid = page.getByTestId('feature-grid');
    await expect(grid).toBeVisible({ timeout: CONTENT_TIMEOUT });

    // DB: landing.features.* items — each has metadata.title and body
    const featureItems = await db.getContentItemsByPrefix('landing.features.');
    expect(featureItems.length, 'Expected landing.features.* content items in DB').toBeGreaterThan(0);

    const cards = grid.locator('.feature-card');
    const cardCount = await cards.count();
    expect(featureItems.length).toBe(cardCount);

    // Verify each feature's metadata.title appears as a heading on the page
    for (const item of featureItems) {
      const meta = item.metadata as Record<string, string>;
      const title = meta.title ?? item.key;
      await expect(
        grid.getByRole('heading', { name: title }),
        `Expected feature heading "${title}" from DB key ${item.key}`,
      ).toBeVisible();
    }
  });
});

test.describe('Landing Page — Navigation', () => {
  test('login link navigates to /login', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('login-link').click();
    await expect(page).toHaveURL(/\/login/);
  });

  test('register link navigates to /register', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('register-link').click();
    await expect(page).toHaveURL(/\/register/);
  });

  test('guest button triggers auth and navigates to /terminal', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('guest-button').click();
    await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 15000 });
  });

  test('all anchor hrefs resolve without 404', async ({ page }) => {
    await page.goto('/');

    const hrefs = await page.evaluate(() => {
      const anchors = Array.from(document.querySelectorAll('a[href]'));
      return [
        ...new Set(
          anchors
            .map((a) => (a as HTMLAnchorElement).href)
            .filter((href) => href && !href.startsWith('mailto:') && !href.startsWith('tel:')),
        ),
      ];
    });

    for (const href of hrefs) {
      const response = await page.request.get(href);
      expect(
        response.status(),
        `Expected ${href} to not return 404, got ${response.status()}`,
      ).not.toBe(404);
    }
  });
});

test.describe('Landing Page — Theme', () => {
  test('dark theme is applied by default (background is dark)', async ({ browser }) => {
    const context = await browser.newContext({ colorScheme: 'dark' });
    const page = await context.newPage();
    await page.goto('/');
    const landing = page.getByTestId('landing');
    await expect(landing).toBeVisible();

    const bg = await landing.evaluate((el) => {
      return window.getComputedStyle(el).backgroundColor;
    });

    // Parse rgb(r, g, b) and verify average channel is below 128 (dark).
    const match = bg.match(/\d+/g);
    expect(match, `Could not parse background color: ${bg}`).not.toBeNull();
    const [r, g, b] = match!.map(Number);
    const brightness = (r + g + b) / 3;
    expect(brightness, `Expected dark background, got: ${bg}`).toBeLessThan(128);
  });
});

test.describe('Landing Page — Graceful Degradation', () => {
  test('page renders without crash when server returns empty content', async ({ page }) => {
    // Collect page errors before navigation so load-time errors are captured.
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    // Intercept the content API and return an empty list.
    await page.route('**/holomush.web.v1.WebService/ListContent', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [] }),
      });
    });

    await page.goto('/');

    // The hero with fallback text must still render.
    await expect(page.getByTestId('hero-title')).toBeVisible();
    await expect(page.getByTestId('hero-tagline')).toBeVisible();

    // No JS error should have been thrown.
    expect(errors).toHaveLength(0);
  });
});
