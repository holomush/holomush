// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import CommunicationRenderer from './CommunicationRenderer.svelte';

interface Ev { type: string; category: string; format: string; actor: string; text: string; metadata?: Record<string, unknown>; }

function render(event: Ev): { text: string; hasTestId: boolean } {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(CommunicationRenderer, { target, props: { event } });
  const text = (target.textContent ?? '').replace(/\s+/g, ' ').trim();
  const hasTestId = target.querySelector('[data-testid="event"]') !== null;
  unmount(component);
  target.remove();
  return { text, hasTestId };
}

afterEach(() => document.body.replaceChildren());

describe('CommunicationRenderer (post-seam, behavior-preserving)', () => {
  it('renders a speech event as actor says quote and keeps the event testid', () => {
    const { text, hasTestId } = render({ type: 'core-communication:say', category: 'communication', format: 'speech', actor: 'Bob', text: 'Hi.' });
    expect(text).toBe('Bob says, "Hi."');
    expect(hasTestId).toBe(true);
  });
  it('renders an action event with the actor inline', () => {
    expect(render({ type: 'core-communication:pose', category: 'communication', format: 'action', actor: 'Alice', text: 'waves.' }).text).toBe('Alice waves.');
  });
  it('renders an ooc event with the [OOC] prefix', () => {
    expect(render({ type: 'core-communication:ooc', category: 'communication', format: 'speech', actor: 'Bob', text: 'brb' }).text).toBe('[OOC] Bob says, "brb"');
  });
});
