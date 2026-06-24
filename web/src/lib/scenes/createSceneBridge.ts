// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable } from 'svelte/store';

/**
 * Cross-component signal for the "create scene" sheet.
 *
 * The sheet itself is owned by `ScenesShell` (so it is available on every
 * `/scenes/*` route), but it can be opened from outside the shell — e.g. the
 * `/scenes` index empty-state "+ New scene" button, which renders as the
 * shell's center content. `ScenesShell` binds the sheet's `open` to this store;
 * any descendant opens it via `openCreateScene()`. Mirrors the footerBridge /
 * composerBridge bridge-store pattern (holomush-q41kr).
 */
export const createSceneOpen = writable(false);

/** Open the shell-owned create-scene sheet. */
export const openCreateScene = () => createSceneOpen.set(true);
