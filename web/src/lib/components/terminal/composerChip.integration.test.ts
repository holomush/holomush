// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { resolveComposerChip } from './composerChip';

describe('composer chip across kinds (INV-5/6/7)', () => {
  const denied = { names: new Set(['look', 'say']), aliases: { '"': 'say' }, incomplete: true };
  it('omits the chip for a command absent from the ABAC-filtered set (INV-5)', () => {
    expect(resolveComposerChip('scene list', denied)).toBeNull();
  });
  it('still chips present commands when incomplete (INV-6)', () => {
    expect(resolveComposerChip('look', denied)).toEqual({ kind: 'command', label: 'look' });
  });
  it('preserves speech chips distinct from command chips (INV-7)', () => {
    expect(resolveComposerChip('"hi', denied)).toEqual({ kind: 'say', label: 'say' });
  });
});
