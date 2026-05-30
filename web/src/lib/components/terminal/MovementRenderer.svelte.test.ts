// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import MovementRenderer from './MovementRenderer.svelte';

// Movement events carry the full sentence in `event.text` (the server's
// formatMovementText composes "<actor> has arrived."), unlike communication
// events whose `text` is the bare body. The renderer MUST therefore emit
// `text` verbatim and MUST NOT prepend `actor` — doing so double-prints the
// name ("Opal Radon Opal Radon has arrived."). Regression guard for
// holomush-pzen.

interface MovementEvent {
  type: string;
  category: string;
  format: string;
  actor: string;
  text: string;
  metadata?: Record<string, unknown>;
}

function render(event: MovementEvent): { text: string; movementSpans: number } {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(MovementRenderer, { target, props: { event } });
  const text = (target.textContent ?? '').replace(/\s+/g, ' ').trim();
  const movementSpans = target.querySelectorAll('.movement').length;
  unmount(component);
  target.remove();
  return { text, movementSpans };
}

// `type` is the bare wire discriminator the server emits ("arrive" / "leave",
// per internal/web/translate.go), not a namespaced form.
function movement(type: 'arrive' | 'leave', actor: string, text: string): MovementEvent {
  return { type, category: 'movement', format: 'notification', actor, text, metadata: {} };
}

afterEach(() => {
  document.body.replaceChildren();
});

describe('MovementRenderer', () => {
  it('renders an arrival with the actor name exactly once', () => {
    const { text, movementSpans } = render(movement('arrive', 'Opal Radon', 'Opal Radon has arrived.'));
    expect(text).toBe('Opal Radon has arrived.');
    expect(text.match(/Opal Radon/g) ?? []).toHaveLength(1);
    // Exactly one node carries the sentence — no separate actor element that
    // could reintroduce the name with identical visible text.
    expect(movementSpans).toBe(1);
  });

  it('renders a departure with the actor name exactly once', () => {
    const { text } = render(movement('leave', 'Beryl Cobalt', 'Beryl Cobalt has left.'));
    expect(text).toBe('Beryl Cobalt has left.');
    expect(text.match(/Beryl Cobalt/g) ?? []).toHaveLength(1);
  });

  it('renders a departure-with-reason with the actor name exactly once', () => {
    const { text } = render(movement('leave', 'Beryl Cobalt', 'Beryl Cobalt has left (disconnected).'));
    expect(text).toBe('Beryl Cobalt has left (disconnected).');
    expect(text.match(/Beryl Cobalt/g) ?? []).toHaveLength(1);
  });
});
