// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test as base, type Page } from '@playwright/test';
import * as db from './db';

export { db };

/** Extract the session ID from the browser's sessionStorage. */
export async function getClientSessionId(page: Page): Promise<string | null> {
  return page.evaluate(() => {
    const raw = sessionStorage.getItem('holomush-session');
    if (!raw) return null;
    try {
      return JSON.parse(raw).sessionId ?? null;
    } catch {
      return null;
    }
  });
}

/** Extract the character name from the browser's sessionStorage. */
export async function getClientCharacterName(page: Page): Promise<string | null> {
  return page.evaluate(() => {
    const raw = sessionStorage.getItem('holomush-session');
    if (!raw) return null;
    try {
      return JSON.parse(raw).characterName ?? null;
    } catch {
      return null;
    }
  });
}

/**
 * Extended test fixture that captures browser console logs and tears down the
 * DB pool after all tests. Import `test` and `expect` from this module instead
 * of @playwright/test.
 */
export const test = base.extend<{ _consoleCapture: void }>({
  _consoleCapture: [
    async ({ page }, use, testInfo) => {
      const logs: string[] = [];
      page.on('console', (msg) => {
        logs.push(`[${msg.type()}] ${msg.text()}`);
      });
      page.on('pageerror', (err) => {
        logs.push(`[error] ${err.message}`);
      });

      await use();

      if (logs.length > 0) {
        await testInfo.attach('browser-console-logs', {
          body: logs.join('\n'),
          contentType: 'text/plain',
        });
      }
    },
    { auto: true },
  ],
});

// Close the shared pool after all workers finish.
base.afterAll(async () => {
  await db.closePool();
});

export { expect } from '@playwright/test';
