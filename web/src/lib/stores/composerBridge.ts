// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, get } from 'svelte/store';

export const composerDraft = writable<string>('');
export const composerSubmit = writable<((cmd: string) => void) | null>(null);

export function setComposerDraft(text: string) {
  composerDraft.set(text);
}

export function registerComposerSubmit(fn: ((cmd: string) => void) | null) {
  composerSubmit.set(fn);
}

export function invokeComposerSubmit(cmd: string) {
  const fn = get(composerSubmit);
  if (fn) fn(cmd);
}
