// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import PoseCard from './PoseCard.svelte';
import type { LogEntry } from '$lib/scenes/types';

function entry(p: Partial<LogEntry>): LogEntry {
  return { id: 'e1', kind: 'say', actorId: 'c1', actorName: 'Bazian', text: 'Hold the line.', timestampMs: 1_717_000_000_000, ...p };
}

function render(e: LogEntry): { text: string; html: string } {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(PoseCard, { target, props: { entry: e } });
  const text = (target.textContent ?? '').replace(/\s+/g, ' ').trim();
  const html = target.innerHTML;
  unmount(component);
  target.remove();
  return { text, html };
}

afterEach(() => document.body.replaceChildren());

describe('PoseCard', () => {
  it('renders a say with canonical phrasing (actor once, quoted)', () => {
    const { text } = render(entry({ kind: 'say', actorName: 'Bazian', text: 'Hold the line.' }));
    expect(text).toContain('Bazian says, "Hold the line."');
    // actor appears exactly once in the body line (no separate name banner)
    expect((text.match(/Bazian says/g) ?? []).length).toBe(1);
  });
  it('renders a pose with the actor inline', () => {
    expect(render(entry({ kind: 'pose', actorName: 'Foob', text: 'draws his blade.' })).text).toContain('Foob draws his blade.');
  });
  it('renders ooc distinctly via the [OOC] prefix, not the ad-hoc form', () => {
    const { text } = render(entry({ kind: 'ooc', actorName: 'Foob', text: 'brb' }));
    expect(text).toContain('[OOC] Foob says, "brb"');
    expect(text).not.toContain('(ooc)');
  });
  it('renders system as bare narration', () => {
    expect(render(entry({ kind: 'system', actorName: '', text: 'The torches gutter.' })).text).toContain('The torches gutter.');
  });
  it('shows an understated timestamp on the left rail', () => {
    expect(render(entry({})).html).toMatch(/class="[^"]*\bts\b/);
  });
});
