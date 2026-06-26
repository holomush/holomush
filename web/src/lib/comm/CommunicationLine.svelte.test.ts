// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import CommunicationLine from './CommunicationLine.svelte';
import type { CommLine } from './commLine';

function render(line: CommLine): { text: string; html: string } {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(CommunicationLine, { target, props: { line } });
  const text = (target.textContent ?? '').replace(/\s+/g, ' ').trim();
  const html = target.innerHTML;
  unmount(component);
  target.remove();
  return { text, html };
}

afterEach(() => document.body.replaceChildren());

describe('CommunicationLine', () => {
  it('renders a say as actor + says + quoted speech', () => {
    expect(render({ kind: 'say', actor: 'Bob', text: 'Hello there.' }).text).toBe('Bob says, "Hello there."');
  });
  it('honors a say label override', () => {
    expect(render({ kind: 'say', actor: 'Bob', text: 'psst', label: 'whispers' }).text).toBe('Bob whispers, "psst"');
  });
  it('renders a pose as actor inline with the action', () => {
    expect(render({ kind: 'pose', actor: 'Alice', text: 'smiles warmly.' }).text).toBe('Alice smiles warmly.');
  });
  it('omits the actor-action space for a semipose', () => {
    expect(render({ kind: 'pose', actor: 'Alice', text: "'s eyes narrow.", noSpace: true }).text).toBe("Alice's eyes narrow.");
  });
  it('renders ooc default style as prefixed speech', () => {
    expect(render({ kind: 'ooc', actor: 'Bob', text: 'brb' }).text).toBe('[OOC] Bob says, "brb"');
  });
  it('renders an ooc pose style as prefix + actor inline with the action', () => {
    expect(render({ kind: 'ooc', actor: 'Foob', text: 'waves', oocStyle: 'pose' }).text).toBe('[OOC] Foob waves');
  });
  it('omits the actor-action space for an ooc semipose', () => {
    expect(render({ kind: 'ooc', actor: 'Foob', text: "'s data is gone", oocStyle: 'semipose' }).text).toBe("[OOC] Foob's data is gone");
  });
  it('honors a custom ooc prefix', () => {
    expect(render({ kind: 'ooc', actor: 'Bob', text: 'brb', oocPrefix: '[ic-ooc]' }).text).toBe('[ic-ooc] Bob says, "brb"');
  });
  it('renders a channel prefix before a say', () => {
    expect(render({ kind: 'say', actor: 'Bob', text: 'hi', channel: 'public' }).text).toBe('[public] Bob says, "hi"');
  });
  it('renders emit as bare narration', () => {
    expect(render({ kind: 'emit', actor: '', text: 'A bell rings in the distance.' }).text).toBe('A bell rings in the distance.');
  });
  it('uses --mush-* tokens, not brand colors', () => {
    expect(render({ kind: 'say', actor: 'Bob', text: 'hi' }).html).toMatch(/class="[^"]*\bspeaker\b/);
  });
  it('escapes HTML in text (SEAM-3)', () => {
    const { html } = render({ kind: 'say', actor: 'Bob', text: '<img src=x onerror=alert(1)>' });
    expect(html).not.toContain('<img');
    expect(html).toContain('&lt;img');
  });
  it('linkifies a URL in text', () => {
    const { html } = render({ kind: 'emit', actor: '', text: 'see https://example.com now' });
    expect(html).toContain('<a href="https://example.com"');
  });
});
