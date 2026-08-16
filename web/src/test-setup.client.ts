// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Client-project setup: drain bits-ui's deferred body-scroll-lock cleanup
// before vitest disposes the jsdom environment.
//
// WHY THIS EXISTS. bits-ui does not release the body scroll lock synchronously
// on unmount. `scheduleCleanupIfNoNewLocks` arms a `window.setTimeout` (24ms by
// default) whose callback calls `resetBodyStyle()` →
// `document.body.setAttribute(...)`, deliberately deferred so a same-tick
// destroy/create pair does not flicker (huntabyte/bits-ui#1639). A component
// test that mounts an overlay, unmounts it, and then ENDS therefore leaves a
// timer armed. If vitest tears the environment down inside that window, the
// callback runs against a disposed global and throws
//
//   ReferenceError: document is not defined
//     ❯ resetBodyStyle  bits-ui/dist/internal/body-scroll-lock.svelte.js:34
//
// as an UNHANDLED error — which fails the whole run even though every test
// passed, because vitest treats an escaped async error as a result-invalidating
// event. Observed on CI as `Build` failing with `772 passed / 1 error`,
// including on a commit that changed nothing but markdown.
//
// The race is invisible on fast hardware: the observable precondition is not.
// Immediately after `unmount()` the lock is still on `document.body`; ~24ms
// later it is gone. So rather than trying to win or lose the race, this waits
// for the lock to actually clear while the environment is still alive.
//
// Cost is zero for the tests that do not open an overlay — `locked()` is false
// on the first check and it returns without yielding.

import { afterEach } from 'vitest';

/** bits-ui writes this custom property onto `document.body` while a lock is held. */
function locked(): boolean {
  return document.body.style.getPropertyValue('--scrollbar-width') !== '';
}

/**
 * Wait for a pending body-scroll-lock cleanup to run.
 *
 * Bounded: a test that deliberately ends with an overlay still MOUNTED holds a
 * live lock that no timer will clear, and must not stall the suite. That case is
 * harmless anyway — a lock with no scheduled cleanup has no callback to outlive
 * the environment.
 */
async function drainBodyScrollLock(timeoutMs = 250): Promise<void> {
  if (!locked()) return;
  const deadline = Date.now() + timeoutMs;
  while (locked() && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
}

afterEach(async () => {
  await drainBodyScrollLock();
});
