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

// holomush-g1qcw.8: chip-purity regression guard. resolveComposerChip is a
// preview-only mapper (design INV-4, server-sourced recognition) — it MUST
// return a {kind,label} description of the text without ever rewriting it or
// mutating the CommandListState it was handed. If a future change turned this
// into a client-side text transformer (e.g. stripping the sigil and returning
// the rewritten command), the submitted command and its chip preview could
// silently diverge.
describe('resolveComposerChip purity (INV-4)', () => {
  it('leaves the submitted text and command-list state untouched', () => {
    const original = ':bows to the crowd';
    const inputState = {
      names: new Set(['look', 'scene', 'say', 'pose', 'ooc']),
      aliases: { l: 'look', '"': 'say', ':': 'pose', ';': 'pose' },
      incomplete: false,
    };
    const namesSnapshot = new Set(inputState.names);
    const aliasesSnapshot = { ...inputState.aliases };

    const chip = resolveComposerChip(original, inputState);

    // The chip is a {kind,label} preview — never the (possibly rewritten)
    // command text itself.
    expect(chip).toEqual({ kind: 'pose', label: 'pose' });
    expect(chip).not.toHaveProperty('text');

    // The caller's text is exactly what gets submitted — resolveComposerChip
    // must not have mutated it (strings are immutable, but this pins the
    // contract at the call-site level: no rewritten value ever finds its way
    // back into `original`).
    expect(original).toBe(':bows to the crowd');

    // The command-list state (names/aliases) must be read-only input, not a
    // scratch pad the resolver writes through.
    expect(inputState.names).toEqual(namesSnapshot);
    expect(inputState.aliases).toEqual(aliasesSnapshot);

    // Calling again with the same inputs is deterministic (no hidden state).
    expect(resolveComposerChip(original, inputState)).toEqual(chip);
  });
});
