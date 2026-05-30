// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { resolveComposerChip } from './composerChip';

const state = {
  names: new Set(['look', 'scene', 'say', 'pose', 'ooc']),
  aliases: { l: 'look', '"': 'say', ':': 'pose', ';': 'pose' },
  incomplete: false,
};

describe('resolveComposerChip', () => {
  it('returns a speech chip for say sigil', () => {
    expect(resolveComposerChip('"hi there', state)).toEqual({ kind: 'say', label: 'say' });
  });
  it('returns a speech chip for the say verb', () => {
    expect(resolveComposerChip('say hi', state)).toEqual({ kind: 'say', label: 'say' });
  });
  it('returns a pose chip for the : sigil', () => {
    expect(resolveComposerChip(':waves', state)).toEqual({ kind: 'pose', label: 'pose' });
  });
  it('returns a command chip with canonical name for a recognized command', () => {
    expect(resolveComposerChip('scene list', state)).toEqual({ kind: 'command', label: 'scene' });
  });
  it('resolves an alias to its canonical name on a command chip', () => {
    expect(resolveComposerChip('l', state)).toEqual({ kind: 'command', label: 'look' });
  });
  it('returns null for an unrecognized first token', () => {
    expect(resolveComposerChip('sceen list', state)).toBeNull();
  });
  it('returns null for empty input', () => {
    expect(resolveComposerChip('   ', state)).toBeNull();
  });
  it('returns null for prototype-chain keys (constructor)', () => {
    expect(resolveComposerChip('constructor', state)).toBeNull();
  });
  it('returns null for prototype-chain keys (__proto__)', () => {
    expect(resolveComposerChip('__proto__ foo', state)).toBeNull();
  });
  it('returns null for prototype-chain keys (toString)', () => {
    expect(resolveComposerChip('toString', state)).toBeNull();
  });
  it('returns null when an alias targets a command absent from the visible set', () => {
    const stale = { names: new Set(['look']), aliases: { g: 'ghost' }, incomplete: false };
    expect(resolveComposerChip('g', stale)).toBeNull();
  });
});
