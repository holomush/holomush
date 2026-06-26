// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';

const src = (rel: string) => readFileSync(`${process.cwd()}/src/lib/components/${rel}`, 'utf8');

// SEAM-2: message renderers MUST NOT use brand colors. Message coloring lives in
// --mush-* tokens, centralized in the shared CommunicationLine primitive — the
// renderer components (PoseCard, CommunicationRenderer) delegate to it, so the
// guard asserts the brand-color fence on them and the --mush-* presence on the primitive.
describe('SEAM-2 message renderers carry no brand colors; coloring uses --mush-* tokens', () => {
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
