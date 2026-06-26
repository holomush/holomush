// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';

const src = (rel: string) => readFileSync(`${process.cwd()}/src/lib/components/${rel}`, 'utf8');

// SEAM-2: message renderers MUST color via --mush-* tokens, never brand colors.
describe('SEAM-2 message renderers use --mush-* not brand colors', () => {
  it('PoseCard uses no --brand-* color', () => {
    expect(src('scenes/PoseCard.svelte')).not.toContain('--brand-');
  });
  it('CommunicationRenderer uses no --brand-* color', () => {
    expect(src('terminal/CommunicationRenderer.svelte')).not.toContain('--brand-');
  });
  it('the shared primitive uses --mush- tokens', () => {
    expect(readFileSync(`${process.cwd()}/src/lib/comm/CommunicationLine.svelte`, 'utf8')).toContain('var(--mush-say-speaker)');
  });
});
