// web/e2e/scenes-panels.spec.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { test, expect, registerAndEnterTerminal } from './helpers/fixtures';

// Verifies holomush-5rh.29: the scenes desktop layout's left scene-list and
// right context-rail panes collapse/expand with a slide transition. The panes
// exist regardless of scene data, so no scene seeding is needed.
//
// Collapse is asserted on paneforge's own state contract (the [data-pane]
// element gets data-collapsed / data-expanded) rather than child visibility —
// the context-rail <aside> has a border-l, so even at 0 width its 1px border-box
// reads as "visible" to a pixel check though the pane's overflow:hidden clips it.
test.describe('scenes workspace collapsible panels', () => {
  test('left list and right rail collapse/expand with a slide transition', async ({ page }, testInfo) => {
    await registerAndEnterTerminal(page, 'pnl');

    // Desktop viewport by default → the three-pane Resizable layout mounts.
    await page.getByTestId('rail').first().getByRole('link', { name: 'Scenes' }).click();
    await expect(page).toHaveURL(/\/scenes/);
    await expect(page.getByTestId('scenes-workspace')).toBeVisible();

    const listPane = page.locator('[data-pane]:has(nav[aria-label="Scene list"])');
    const railPane = page.locator('[data-pane]:has(aside[aria-label="Scene context"])');
    await expect(listPane).toHaveAttribute('data-expanded', '');
    await expect(railPane).toHaveAttribute('data-expanded', '');
    await page.screenshot({ path: testInfo.outputPath('scenes-panels-expanded.png') });

    // The slide is wired: paneforge sizes panes via flex-grow and we transition it.
    const transitionProp = await listPane.evaluate(
      (el) => getComputedStyle(el).transitionProperty,
    );
    expect(transitionProp).toContain('flex-grow');

    // Collapse the left scene list → paneforge marks the pane collapsed.
    await page.getByRole('button', { name: 'Hide scene list' }).click();
    await expect(listPane).toHaveAttribute('data-collapsed', '');
    await page.screenshot({ path: testInfo.outputPath('scenes-panels-list-collapsed.png') });

    // Collapse the right context rail too.
    await page.getByRole('button', { name: 'Hide scene context' }).click();
    await expect(railPane).toHaveAttribute('data-collapsed', '');
    await page.screenshot({ path: testInfo.outputPath('scenes-panels-both-collapsed.png') });

    // Re-expand both.
    await page.getByRole('button', { name: 'Show scene list' }).click();
    await expect(listPane).toHaveAttribute('data-expanded', '');
    await page.getByRole('button', { name: 'Show scene context' }).click();
    await expect(railPane).toHaveAttribute('data-expanded', '');
  });

  test('keyboard shortcut and command palette toggle the panes', async ({ page }) => {
    await registerAndEnterTerminal(page, 'kbd');
    await page.goto('/scenes');
    await expect(page.getByTestId('scenes-workspace')).toBeVisible();

    const listPane = page.locator('[data-pane]:has(nav[aria-label="Scene list"])');
    const railPane = page.locator('[data-pane]:has(aside[aria-label="Scene context"])');
    await expect(listPane).toHaveAttribute('data-expanded', '');

    // ⌘⇧, (detected via e.code 'Comma') toggles the left scene list.
    await page.keyboard.press('ControlOrMeta+Shift+Comma');
    await expect(listPane).toHaveAttribute('data-collapsed', '');
    await page.keyboard.press('ControlOrMeta+Shift+Comma');
    await expect(listPane).toHaveAttribute('data-expanded', '');

    // ⌘⇧. (e.code 'Period') toggles the right context rail.
    await page.keyboard.press('ControlOrMeta+Shift+Period');
    await expect(railPane).toHaveAttribute('data-collapsed', '');
    await page.keyboard.press('ControlOrMeta+Shift+Period');
    await expect(railPane).toHaveAttribute('data-expanded', '');

    // The command palette also toggles the right context rail.
    await page.keyboard.press('ControlOrMeta+k');
    await page.getByPlaceholder('Type a command…').fill('Toggle scene context');
    await page.getByRole('option', { name: 'Toggle scene context' }).click();
    await expect(railPane).toHaveAttribute('data-collapsed', '');
  });

  test('drag-collapsing the left pane persists across reload', async ({ page }) => {
    await registerAndEnterTerminal(page, 'drg');
    await page.goto('/scenes');
    await expect(page.getByTestId('scenes-workspace')).toBeVisible();

    const listPane = page.locator('[data-pane]:has(nav[aria-label="Scene list"])');
    await expect(listPane).toHaveAttribute('data-expanded', '');

    // Drag the first resize handle to the far left, past the collapse threshold —
    // paneforge snaps the pane to collapsedSize (0) and fires onCollapse, which
    // writes back to uiPrefs.
    const handle = page.locator('[data-pane-resizer]').first();
    const box = await handle.boundingBox();
    if (!box) throw new Error('resize handle has no bounding box');
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(2, box.y + box.height / 2, { steps: 12 });
    await page.mouse.up();
    await expect(listPane).toHaveAttribute('data-collapsed', '');

    // Reload — the drag-collapsed state persists (uiPrefs write-back + autoSaveId).
    await page.reload();
    await expect(page.getByTestId('scenes-workspace')).toBeVisible();
    await expect(page.locator('[data-pane]:has(nav[aria-label="Scene list"])')).toHaveAttribute(
      'data-collapsed',
      '',
    );
  });

  test('collapsed state persists across reload (uiPrefs → localStorage)', async ({ page }) => {
    await registerAndEnterTerminal(page, 'prs');
    await page.goto('/scenes');
    await expect(page.getByTestId('scenes-workspace')).toBeVisible();

    const listPane = page.locator('[data-pane]:has(nav[aria-label="Scene list"])');
    await expect(listPane).toHaveAttribute('data-expanded', '');
    await page.getByRole('button', { name: 'Hide scene list' }).click();
    await expect(listPane).toHaveAttribute('data-collapsed', '');

    await page.reload();
    await expect(page.getByTestId('scenes-workspace')).toBeVisible();
    // Persisted collapsed: the pane comes back collapsed and the toggle offers "Show".
    await expect(page.locator('[data-pane]:has(nav[aria-label="Scene list"])')).toHaveAttribute(
      'data-collapsed',
      '',
    );
    await expect(page.getByRole('button', { name: 'Show scene list' })).toBeVisible();
  });
});
