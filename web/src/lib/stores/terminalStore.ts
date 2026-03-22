// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, get } from 'svelte/store';

export interface TerminalLine {
  id: string;
  event: { type: string; characterName: string; text: string; channel?: number; metadata?: unknown };
  replayed: boolean;
}

const DEFAULT_BUFFER_SIZE = 2048;

function getBufferSize(): number {
  if (typeof window === 'undefined') return DEFAULT_BUFFER_SIZE;
  const saved = parseInt(localStorage.getItem('holomush-buffer-size') ?? '', 10);
  return saved >= 512 && saved <= 8192 ? saved : DEFAULT_BUFFER_SIZE;
}

export const lines = writable<TerminalLine[]>([]);
export const replayActive = writable<boolean>(false);
export const newMessageCount = writable<number>(0);
export const isAtBottom = writable<boolean>(true);

let lineCounter = 0;

export function appendLine(event: TerminalLine['event'], replayed: boolean) {
  const id = `line-${++lineCounter}`;
  const bufferSize = getBufferSize();
  lines.update((current) => {
    const next = [...current, { id, event, replayed }];
    return next.length > bufferSize ? next.slice(next.length - bufferSize) : next;
  });
  if (!get(isAtBottom)) {
    newMessageCount.update((n) => n + 1);
  }
}

export function clearLines() {
  lines.set([]);
  replayActive.set(false);
  isAtBottom.set(true);
  newMessageCount.set(0);
}

export function scrolledToBottom() {
  isAtBottom.set(true);
  newMessageCount.set(0);
}

export function scrolledAway() {
  isAtBottom.set(false);
}
