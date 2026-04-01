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
 * Extended test fixture that tears down the DB pool after all tests.
 * Import `test` and `expect` from this module instead of @playwright/test.
 */
export const test = base.extend({});

// Close the shared pool after all workers finish.
base.afterAll(async () => {
  await db.closePool();
});

export { expect } from '@playwright/test';
