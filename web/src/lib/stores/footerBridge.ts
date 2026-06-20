// web/src/lib/stores/footerBridge.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import type { Snippet } from 'svelte';
import { writable } from 'svelte/store';

/**
 * Content the active section renders inside the shell's persistent footer.
 * `null` => the shell renders its baseline. Mirrors composerBridge: a section
 * registers on mount and clears on destroy, so the bar never renders a dead
 * snippet.
 */
export const footerContent = writable<Snippet | null>(null);

export function setFooter(snippet: Snippet): void {
  footerContent.set(snippet);
}

export function clearFooter(): void {
  footerContent.set(null);
}
