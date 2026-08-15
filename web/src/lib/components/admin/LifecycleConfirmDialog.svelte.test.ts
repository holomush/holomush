// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// Named .svelte.test.ts: the suffix routes this file to the client Vitest
// project, whose resolve.conditions: ['browser'] is what makes `mount` work.

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import { ConnectError, Code } from '@connectrpc/connect';
import LifecycleConfirmDialog from './LifecycleConfirmDialog.svelte';

/**
 * The vocabulary this phase may not use about retiring a character, asserted
 * against RENDERED OUTPUT rather than file text — deliberately, so a source
 * comment explaining the prohibition can neither satisfy nor break it.
 *
 * None of these is true of retire: the public profile stays visible, published
 * history is unchanged, the name stays reserved, and the transition is
 * undoable. Copy that asserts a property of the system is a claim.
 */
const FORBIDDEN = /\bdelete\b|\bremove\b|\bpurge\b|\berase\b|permanent|irreversible|cannot be undone|take.?down/i;

type Props = {
  name?: string;
  intent?: 'retire' | 'unretire';
  onconfirm?: () => Promise<unknown>;
  oncancel?: () => void;
};

function render(props: Props = {}) {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(LifecycleConfirmDialog, {
    target,
    props: { name: 'Ashwood, Miren', intent: 'retire', onconfirm: async () => {}, ...props },
  });
  flushSync();
  return { target, component };
}

function content(): HTMLElement {
  const el = document.body.querySelector('[data-slot="alert-dialog-content"]');
  if (!el) throw new Error('confirm did not render');
  return el as HTMLElement;
}

function text(): string {
  return (content().textContent ?? '').replace(/\s+/g, ' ').trim();
}

function confirmButton(): HTMLButtonElement {
  return content().querySelector('[data-testid="lifecycle-confirm"]') as HTMLButtonElement;
}

function cancelButton(): HTMLButtonElement {
  return content().querySelector('[data-slot="alert-dialog-cancel"]') as HTMLButtonElement;
}

async function settle() {
  for (let i = 0; i < 4; i++) {
    await Promise.resolve();
    flushSync();
  }
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  promise.catch(() => {});
  return { promise, resolve, reject };
}

afterEach(() => {
  document.body.replaceChildren();
});

describe('LifecycleConfirmDialog — D-108: the retire body carries all four clauses', () => {
  it('says out of active play, not hidden, name reserved, and undoable', () => {
    const { component } = render();
    const body = text();
    expect(body).toContain('out of active play');
    expect(body).toContain('does not hide them');
    expect(body).toContain('name stays reserved');
    expect(body).toContain('undo this at any time');
    unmount(component);
  });

  it('renders the retire body verbatim', () => {
    const { component } = render();
    expect(text()).toContain(
      'Retiring takes this character out of active play. It does not hide them — their public ' +
        'profile stays visible and everything they have already posed or played in is unchanged. ' +
        'The name stays reserved. You can undo this at any time.',
    );
    unmount(component);
  });

  it('titles the retire confirm with the character name', () => {
    const { component } = render({ name: 'Ashwood, Miren' });
    expect(text()).toContain('Retire Ashwood, Miren?');
    expect(confirmButton().textContent?.trim()).toBe('Retire character');
    expect(cancelButton().textContent?.trim()).toBe('Cancel');
    unmount(component);
  });

  it('renders the un-retire copy for the other direction', () => {
    const { component } = render({ intent: 'unretire', name: 'Ashwood, Miren' });
    const body = text();
    expect(body).toContain('Return Ashwood, Miren to active play?');
    expect(body).toContain(
      'This character becomes playable again. Their public profile and history are unchanged.',
    );
    expect(confirmButton().textContent?.trim()).toBe('Un-retire character');
    unmount(component);
  });
});

describe('LifecycleConfirmDialog — ADMIN-05: retire is not framed as a takedown', () => {
  it('matches none of the forbidden vocabulary in either direction', () => {
    const a = render({ intent: 'retire' });
    expect(text()).not.toMatch(FORBIDDEN);
    unmount(a.component);
    document.body.replaceChildren();

    const b = render({ intent: 'unretire' });
    expect(text()).not.toMatch(FORBIDDEN);
    unmount(b.component);
  });

  it('proves the pattern can fail, on a string that asserts what retire is not', () => {
    // The control the assertion above needs: a negative match over rendered
    // output is only evidence if the pattern discriminates.
    expect('This permanently deletes the character and cannot be undone.').toMatch(FORBIDDEN);
  });
});

describe('LifecycleConfirmDialog — it requires a decision', () => {
  it('is an alertdialog, which is what makes a backdrop tap not a dismissal', async () => {
    /*
     * THE ROLE IS THE ASSERTION, and the synthetic interaction below is NOT.
     *
     * Driving a backdrop dismissal in jsdom cannot discriminate: setting
     * `interactOutsideBehavior="close"` on the content — the one-token change
     * that genuinely makes this dialog dismissible — leaves every case in this
     * file green, verified by running it. bits-ui's outside-interaction
     * detection does not fire for a synthetic MouseEvent named 'pointerdown',
     * so a test that only clicked the overlay would pass identically before
     * and after the defect. The browser-level proof is the Playwright block in
     * web/e2e/admin-portal.spec.ts, where the pointer events are real.
     *
     * `role="alertdialog"` DOES discriminate — the plain `dialog` primitive
     * renders `role="dialog"` — and it is the property an assistive technology
     * reads as "this needs a decision".
     */
    const cancels: number[] = [];
    const { component } = render({ oncancel: () => void cancels.push(1) });
    expect(content().getAttribute('role')).toBe('alertdialog');
    expect(content().getAttribute('aria-modal')).toBe('true');
    const overlay = document.body.querySelector(
      '[data-slot="alert-dialog-overlay"]',
    ) as HTMLElement;
    expect(overlay).not.toBeNull();
    overlay.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }));
    overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle();
    expect(document.body.querySelector('[data-slot="alert-dialog-content"]')).not.toBeNull();
    expect(cancels).toEqual([]);
    unmount(component);
  });

  it('lands initial focus on Cancel, never on the destructive confirm', async () => {
    const { component } = render();
    await settle();
    expect(document.activeElement).toBe(cancelButton());
    expect(document.activeElement).not.toBe(confirmButton());
    unmount(component);
  });

  it('reports a cancel to the caller', async () => {
    const cancels: number[] = [];
    const { component } = render({ oncancel: () => void cancels.push(1) });
    await settle();
    cancelButton().click();
    await settle();
    expect(cancels).toEqual([1]);
    unmount(component);
  });
});

describe('LifecycleConfirmDialog — the in-flight and failure shapes', () => {
  it('keeps the confirm label and adds aria-busy while the mutation is in flight', async () => {
    const d = deferred<unknown>();
    const { component } = render({ onconfirm: () => d.promise });
    await settle();
    confirmButton().click();
    await settle();
    expect(confirmButton().textContent?.trim()).toBe('Retire character');
    expect(confirmButton().getAttribute('aria-busy')).toBe('true');
    expect(confirmButton().disabled).toBe(true);
    expect(text()).not.toContain('Retiring…');
    d.resolve(undefined);
    await settle();
    unmount(component);
  });

  it('renders the authored lifecycle failure and no server string', async () => {
    const { component } = render({
      onconfirm: async () => {
        throw new ConnectError('DENY_ADMIN_RETIRE: character owns a location', Code.Internal);
      },
    });
    await settle();
    confirmButton().click();
    await settle();
    expect(text()).toContain("Couldn't change this character's lifecycle. Try again.");
    expect(text()).not.toContain('DENY_ADMIN_RETIRE');
    expect(text()).not.toContain('owns a location');
    // Still open: a failure the operator can retry is not a dismissal.
    expect(confirmButton().disabled).toBe(false);
    unmount(component);
  });

  it('issues exactly one mutation per confirm click while one is in flight', async () => {
    const d = deferred<unknown>();
    let calls = 0;
    const { component } = render({
      onconfirm: () => {
        calls += 1;
        return d.promise;
      },
    });
    await settle();
    confirmButton().click();
    await settle();
    confirmButton().click();
    await settle();
    expect(calls).toBe(1);
    d.resolve(undefined);
    await settle();
    unmount(component);
  });
});
