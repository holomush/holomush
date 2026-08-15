// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// Named .svelte.test.ts: the suffix routes this file to the client Vitest
// project, whose resolve.conditions: ['browser'] is what makes `mount` work
// (web/vite.config.ts:24-32).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import { ConnectError, Code } from '@connectrpc/connect';
import EditCharacterSheet, { ADMIN_EDITABLE_FIELDS } from './EditCharacterSheet.svelte';
import type { CharacterDetail, CharacterRow } from '$lib/admin/client';

const CREATED = 1_700_000_000_000_000_000n;

const ROW: CharacterRow = {
  id: '01JQ7XABCDEFGHJKMNPQRS8F2',
  playerId: 'p1',
  playerUsername: 'mirenplayer',
  name: 'Ashwood, Miren',
  status: 'active',
  lastActiveAt: CREATED,
  createdAt: CREATED,
  version: 7,
};

/** Non-empty prose on every one of the thirteen writable paths. */
const DETAIL: CharacterDetail = {
  character: { version: 7 },
  description: 'A tall figure in a long grey coat.',
  profile: {
    'profile.pronouns': 'she/her',
    'profile.concept': 'Wandering archivist',
    'profile.species': 'Human',
    'profile.age': 'Late thirties',
    'profile.faction': 'The Registry',
    'profile.currently': 'Cataloguing the east wing',
    'profile.timezone': 'UTC+1',
    'profile.appearance': 'Ink-stained fingers, a permanent squint.',
    'profile.personality': 'Patient until she is not.',
    'profile.biography': 'Came up through the archive stacks.',
    'profile.rumors': 'Keeps a ledger nobody else may read.',
    'profile.rp_preferences': 'Slow burn, mystery, long scenes.',
  },
};

/**
 * A deferred promise, so a test can hold the detail fetch in flight and observe
 * the loading state rather than racing it.
 */
function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  // The rejection is attached in the component's own catch; this keeps node
  // from reporting an unhandled rejection before the component gets there.
  promise.catch(() => {});
  return { promise, resolve, reject };
}

/**
 * matchMedia does not exist in this jsdom AT ALL — verified: `typeof
 * window.matchMedia` is 'undefined'. That is exactly why the component's SSR /
 * test guard defaults to the desktop shape, and why these two cases have to
 * install a stub to observe either branch.
 */
function stubMatchMedia(matches: boolean) {
  const listeners: ((e: { matches: boolean }) => void)[] = [];
  const queries: string[] = [];
  const mql = {
    matches,
    media: '(max-width: 767px)',
    addEventListener: (_: string, l: (e: { matches: boolean }) => void) => void listeners.push(l),
    removeEventListener: (_: string, l: (e: { matches: boolean }) => void) => {
      const i = listeners.indexOf(l);
      if (i >= 0) listeners.splice(i, 1);
    },
  };
  vi.stubGlobal('matchMedia', (q: string) => {
    queries.push(q);
    return mql;
  });
  return { listeners, queries };
}

type Props = {
  row?: CharacterRow;
  fetchDetail?: (id: string) => Promise<CharacterDetail | undefined>;
  save?: (args: {
    paths: string[];
    values: Record<string, string>;
    expectedVersion: number;
  }) => Promise<unknown>;
  onlifecycle?: (intent: 'retire' | 'unretire') => void;
  onclose?: () => void;
};

function render(props: Props = {}) {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(EditCharacterSheet, {
    target,
    props: {
      row: ROW,
      fetchDetail: async () => DETAIL,
      ...props,
    },
  });
  flushSync();
  return { target, component };
}

/** The Sheet is portalled to document.body, deliberately — so query there. */
function content(): HTMLElement {
  const el = document.body.querySelector('[data-slot="sheet-content"]');
  if (!el) throw new Error('sheet content did not render');
  return el as HTMLElement;
}

/** Let a resolved promise chain settle and Svelte re-render. */
async function settle() {
  for (let i = 0; i < 4; i++) {
    await Promise.resolve();
    flushSync();
  }
}

function field(name: string): HTMLInputElement | HTMLTextAreaElement {
  const el = content().querySelector(`[name="${name}"]`);
  if (!el) throw new Error(`no control named ${name}`);
  return el as HTMLInputElement | HTMLTextAreaElement;
}

function type(el: HTMLInputElement | HTMLTextAreaElement, value: string) {
  el.value = value;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  flushSync();
}

function saveButton(): HTMLButtonElement {
  const el = content().querySelector('[data-testid="sheet-save"]');
  if (!el) throw new Error('no save control');
  return el as HTMLButtonElement;
}

beforeEach(() => {
  vi.unstubAllGlobals();
});

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

describe('EditCharacterSheet — the thirteen editable paths', () => {
  it('declares exactly thirteen writable paths, split across the two server caps', () => {
    expect(ADMIN_EDITABLE_FIELDS).toHaveLength(13);
    // world.MaxNameLength = 100 on the seven short single-line paths;
    // world.MaxDescriptionLength = 4000 on description plus the five long ones.
    expect(ADMIN_EDITABLE_FIELDS.filter((f) => f.maxBytes === 100)).toHaveLength(7);
    expect(ADMIN_EDITABLE_FIELDS.filter((f) => f.maxBytes === 4000)).toHaveLength(6);
    expect(ADMIN_EDITABLE_FIELDS.map((f) => f.path)).toEqual([
      'description',
      'profile.pronouns',
      'profile.concept',
      'profile.species',
      'profile.age',
      'profile.faction',
      'profile.currently',
      'profile.timezone',
      'profile.appearance',
      'profile.personality',
      'profile.biography',
      'profile.rumors',
      'profile.rp_preferences',
    ]);
  });
});

describe('EditCharacterSheet — the detail fetch is real and its loading state is honest', () => {
  it('populates all thirteen inputs from the resolved detail, not from the row', async () => {
    // The row carries NO profile prose — AdminCharacter is the list projection
    // and deliberately has none — so a Sheet seeded from row data alone renders
    // thirteen blanks and fails this.
    const { component } = render();
    await settle();
    for (const f of ADMIN_EDITABLE_FIELDS) {
      const expected =
        f.path === 'description' ? DETAIL.description : (DETAIL.profile[f.path] ?? '');
      expect(field(f.name).value).toBe(expected);
      expect(field(f.name).value).not.toBe('');
    }
    unmount(component);
  });

  it('renders skeleton rows and a disabled Save while the fetch is in flight', async () => {
    const d = deferred<CharacterDetail | undefined>();
    const { component } = render({ fetchDetail: () => d.promise });
    await settle();
    expect(content().querySelectorAll('[data-slot="skeleton"]').length).toBeGreaterThan(0);
    expect(saveButton().disabled).toBe(true);
    // No editable control exists yet, so no save can be composed against blanks.
    expect(content().querySelector('[name="description"]')).toBeNull();
    d.resolve(DETAIL);
    await settle();
    expect(content().querySelectorAll('[data-slot="skeleton"]')).toHaveLength(0);
    unmount(component);
  });

  it('renders the retry copy with Save still disabled when the fetch rejects', async () => {
    const { component } = render({
      fetchDetail: async () => {
        throw new ConnectError('boom', Code.Unavailable);
      },
    });
    await settle();
    expect(content().textContent).toContain("Couldn't load this character. Try again.");
    expect(saveButton().disabled).toBe(true);
    // Never an empty form that looks editable.
    expect(content().querySelector('[name="description"]')).toBeNull();
    expect(content().querySelector('[data-testid="detail-retry"]')).not.toBeNull();
    unmount(component);
  });

  it('keeps the draft when the page hands down a fresh row object for the same character', async () => {
    /*
     * REGRESSION. The page re-reads the list on every search, sort and page
     * turn and hands down a NEW row object each time. Keying the fetch on the
     * PROP rather than on the character meant each of those re-ran it, flipped
     * the form back to `loading` — disabling Save mid-edit — and then reseeded
     * `working` from the server, discarding the operator's typing. Caught in a
     * real browser as a permanently disabled Save with a draft still on screen.
     */
    let fetches = 0;
    const target = document.createElement('div');
    document.body.appendChild(target);
    const props = $state({
      row: { ...ROW },
      fetchDetail: async () => {
        fetches += 1;
        return DETAIL;
      },
    });
    const component = mount(EditCharacterSheet, { target, props });
    flushSync();
    await settle();
    expect(fetches).toBe(1);

    type(field('concept'), 'a draft mid-edit');
    expect(saveButton().disabled).toBe(false);

    // Same character, different object — exactly what a settled search does.
    props.row = { ...ROW };
    flushSync();
    await settle();

    expect(fetches).toBe(1);
    expect(field('concept').value).toBe('a draft mid-edit');
    expect(saveButton().disabled).toBe(false);
    unmount(component);
  });

  it('renders no server-supplied string on a failed fetch', async () => {
    const { component } = render({
      fetchDetail: async () => {
        throw new ConnectError('DENY_ADMIN_CHARACTER: index missing', Code.PermissionDenied);
      },
    });
    await settle();
    expect(content().textContent).not.toContain('DENY_ADMIN_CHARACTER');
    expect(content().textContent).not.toContain('index missing');
    unmount(component);
  });
});

describe('EditCharacterSheet — the two groups and the header metadata', () => {
  it('renders Managed elsewhere before Editable here, collapsed on open', async () => {
    const { component } = render();
    await settle();
    const managed = content().querySelector('[data-group="managed-elsewhere"]') as HTMLDetailsElement;
    const editable = content().querySelector('[data-group="editable-here"]') as HTMLElement;
    expect(managed).not.toBeNull();
    expect(editable).not.toBeNull();
    expect(managed.open).toBe(false);
    expect(
      managed.compareDocumentPosition(editable) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(managed.textContent).toContain('managed by their own operations');
    unmount(component);
  });

  it('carries the version in the header metadata line and in no control at all', async () => {
    const { component } = render();
    await settle();
    const meta = content().querySelector('[data-testid="sheet-meta"]') as HTMLElement;
    expect(meta.textContent).toContain(ROW.id);
    expect(meta.textContent).toContain(`v${ROW.version}`);

    for (const el of content().querySelectorAll('input, select, textarea')) {
      const v = (el as HTMLInputElement).value;
      expect(v).not.toBe(String(ROW.version));
      expect(v).not.toBe(`v${ROW.version}`);
    }
    // Exactly one occurrence of the `v{n}` metadata token in the whole Sheet.
    const occurrences = (content().textContent ?? '').split(`v${ROW.version}`).length - 1;
    expect(occurrences).toBe(1);
    unmount(component);
  });

  it('states why name and status are not editable here when the group is expanded', async () => {
    const { component } = render();
    await settle();
    const managed = content().querySelector('[data-group="managed-elsewhere"]') as HTMLDetailsElement;
    managed.open = true;
    flushSync();
    // Whitespace-normalized: the copy is byte-exact, the source line wraps.
    const text = (managed.textContent ?? '').replace(/\s+/g, ' ');
    expect(text).toContain(
      'Names go through the normalization pipeline and a uniqueness check, so they are not editable from this form.',
    );
    expect(text).toContain(
      'Use the lifecycle control below — it sends the transition, not a status value.',
    );
    unmount(component);
  });
});

describe('EditCharacterSheet — the lifecycle picker never sends a status value', () => {
  it('routes a Retired selection to the confirm and issues no RPC', async () => {
    const seen: string[] = [];
    const saves: unknown[] = [];
    const { component } = render({
      onlifecycle: (intent) => void seen.push(intent),
      save: async (a) => void saves.push(a),
    });
    await settle();
    const picker = content().querySelector('select[name="lifecycle"]') as HTMLSelectElement;
    picker.value = 'retired';
    picker.dispatchEvent(new Event('change', { bubbles: true }));
    flushSync();
    await settle();

    expect(seen).toEqual(['retire']);
    expect(saves).toEqual([]);
    // The transition is NOT applied by the selection: the control snaps back to
    // the character's current lifecycle until the confirm resolves it.
    expect(picker.value).toBe('active');
    unmount(component);
  });

  it('renders idle with its reason and refuses to make it selectable', async () => {
    const seen: string[] = [];
    const { component } = render({ onlifecycle: (i) => void seen.push(i) });
    await settle();
    const picker = content().querySelector('select[name="lifecycle"]') as HTMLSelectElement;
    const idle = Array.from(picker.options).find((o) => o.value === 'idle');
    expect(idle).toBeDefined();
    expect(idle!.disabled).toBe(true);
    expect(content().textContent).toContain(
      'idle — system-invoked on inactivity. Not implemented in this release.',
    );
    unmount(component);
  });

  it('names the RPC it will send, and never a status field', async () => {
    const { component } = render();
    await settle();
    expect(content().textContent).toContain('Sends AdminRetireCharacter — never a status value.');
    unmount(component);
  });

  it('names the un-retire RPC for a retired character', async () => {
    const { component } = render({ row: { ...ROW, status: 'retired' } });
    await settle();
    expect(content().textContent).toContain('Sends AdminUnretireCharacter — never a status value.');
    unmount(component);
  });

  it('routes an Active selection on a retired character to the un-retire confirm', async () => {
    const seen: string[] = [];
    const { component } = render({
      row: { ...ROW, status: 'retired' },
      onlifecycle: (i) => void seen.push(i),
    });
    await settle();
    const picker = content().querySelector('select[name="lifecycle"]') as HTMLSelectElement;
    picker.value = 'active';
    picker.dispatchEvent(new Event('change', { bubbles: true }));
    flushSync();
    expect(seen).toEqual(['unretire']);
    unmount(component);
  });
});

describe('EditCharacterSheet — the mask is the difference, never a flag', () => {
  it('disables Save on open and reports an empty mask', async () => {
    const { component } = render();
    await settle();
    expect(saveButton().disabled).toBe(true);
    expect(content().querySelector('[data-testid="mask-footer"]')!.textContent).toContain(
      'update_mask: empty — no-op',
    );
    unmount(component);
  });

  it('re-disables Save when a field is edited back to its original value', async () => {
    // A dirty flag set by an input handler survives this and FAILS here.
    const { component } = render();
    await settle();
    const original = DETAIL.profile['profile.concept']!;
    type(field('concept'), 'Something else');
    expect(saveButton().disabled).toBe(false);
    type(field('concept'), original);
    expect(saveButton().disabled).toBe(true);
    expect(content().querySelector('[data-testid="mask-footer"]')!.textContent).toContain(
      'update_mask: empty — no-op',
    );
    unmount(component);
  });

  it('counts edits, not filled fields, in the mask footer', async () => {
    const { component } = render();
    await settle();
    type(field('concept'), 'a');
    expect(content().querySelector('[data-testid="mask-footer"]')!.textContent).toContain(
      'update_mask: 1 paths',
    );
    type(field('rumors'), 'b');
    expect(content().querySelector('[data-testid="mask-footer"]')!.textContent).toContain(
      'update_mask: 2 paths',
    );
    unmount(component);
  });

  it('sends only the changed paths, with the row version as expected_version', async () => {
    const calls: { paths: string[]; values: Record<string, string>; expectedVersion: number }[] = [];
    const { component } = render({ save: async (a) => void calls.push(a) });
    await settle();
    type(field('concept'), 'Archivist, retired');
    (content().querySelector('form') as HTMLFormElement).requestSubmit();
    await settle();
    expect(calls).toHaveLength(1);
    expect(calls[0].paths).toEqual(['profile.concept']);
    expect(calls[0].values['profile.concept']).toBe('Archivist, retired');
    expect(calls[0].expectedVersion).toBe(7);
    unmount(component);
  });
});

describe('EditCharacterSheet — a wholly blank profile is a normal state', () => {
  const BLANK: CharacterDetail = { character: { version: 7 }, description: '', profile: {} };

  it('renders thirteen empty inputs with counters at 0 of their cap and an inert Save', async () => {
    const { component } = render({ fetchDetail: async () => BLANK });
    await settle();
    for (const f of ADMIN_EDITABLE_FIELDS) {
      expect(field(f.name).value).toBe('');
      const counter = content().querySelector(`[data-counter-for="${f.path}"]`) as HTMLElement;
      expect(counter.textContent?.replace(/\s+/g, ' ').trim()).toBe(`0 of ${f.maxBytes}`);
    }
    expect(saveButton().disabled).toBe(true);
    // No validation renders before a save.
    expect(content().querySelector('[data-slot="field-error"]')).toBeNull();
    unmount(component);
  });
});

describe('EditCharacterSheet — over the cap is the server’s refusal to make', () => {
  it('keeps Save enabled one byte over a short cap and changes only the counter', async () => {
    // Binding `disabled` to the over-cap predicate makes this FAIL, and makes
    // the server-agreement E2E unreachable: the client would refuse to send
    // the very request whose refusal the proof observes.
    const { component } = render();
    await settle();
    type(field('concept'), 'a'.repeat(101));
    const counter = content().querySelector('[data-counter-for="profile.concept"]') as HTMLElement;
    expect(counter.getAttribute('data-over')).toBe('true');
    expect(counter.textContent?.replace(/\s+/g, ' ').trim()).toBe('101 of 100');
    expect(saveButton().disabled).toBe(false);
    unmount(component);
  });

  it('does not call a 100-byte value over the cap', async () => {
    const { component } = render();
    await settle();
    type(field('concept'), 'a'.repeat(100));
    const counter = content().querySelector('[data-counter-for="profile.concept"]') as HTMLElement;
    expect(counter.getAttribute('data-over')).toBe('false');
    unmount(component);
  });

  it('measures a CJK value in bytes, so a short field goes over at 34 codepoints', async () => {
    const { component } = render();
    await settle();
    type(field('concept'), '三'.repeat(34));
    const counter = content().querySelector('[data-counter-for="profile.concept"]') as HTMLElement;
    expect(counter.textContent?.replace(/\s+/g, ' ').trim()).toBe('102 of 100');
    expect(counter.getAttribute('data-over')).toBe('true');
    expect(saveButton().disabled).toBe(false);
    unmount(component);
  });

  it('leaves a 101-byte value comfortably under the long cap', async () => {
    const { component } = render();
    await settle();
    type(field('biography'), 'a'.repeat(101));
    const counter = content().querySelector(
      '[data-counter-for="profile.biography"]',
    ) as HTMLElement;
    expect(counter.getAttribute('data-over')).toBe('false');
    expect(counter.textContent?.replace(/\s+/g, ' ').trim()).toBe('101 of 4000');
    unmount(component);
  });
});

describe('EditCharacterSheet — the in-flight and conflict shapes', () => {
  it('keeps the submit label and adds aria-busy while a save is in flight', async () => {
    const d = deferred<unknown>();
    const { component } = render({ save: () => d.promise });
    await settle();
    type(field('concept'), 'x');
    const before = saveButton().textContent?.trim();
    expect(before).toBe('Save changes');
    (content().querySelector('form') as HTMLFormElement).requestSubmit();
    await settle();
    expect(saveButton().textContent?.trim()).toBe('Save changes');
    expect(saveButton().getAttribute('aria-busy')).toBe('true');
    expect(saveButton().disabled).toBe(true);
    // No spinner and no label swap.
    expect(content().textContent).not.toContain('Saving');
    d.resolve(undefined);
    await settle();
    unmount(component);
  });

  it('keeps every typed value and names both versions on a version conflict', async () => {
    let fetches = 0;
    const { component } = render({
      fetchDetail: async () => {
        fetches += 1;
        return fetches === 1 ? DETAIL : { ...DETAIL, character: { version: 9 } };
      },
      save: async () => {
        throw new ConnectError('stale', Code.Aborted);
      },
    });
    await settle();
    type(field('concept'), 'Still typing this');
    type(field('rumors'), 'And this');
    (content().querySelector('form') as HTMLFormElement).requestSubmit();
    await settle();

    const alert = content().querySelector('[role="alert"]') as HTMLElement;
    expect(alert).not.toBeNull();
    const text = alert.textContent ?? '';
    expect(text).toContain('version 7');
    expect(text).toContain('9');
    expect(text).toContain('were not applied');
    // Every typed value survives.
    expect(field('concept').value).toBe('Still typing this');
    expect(field('rumors').value).toBe('And this');
    unmount(component);
  });

  it('moves focus to the first conflicting field', async () => {
    let fetches = 0;
    const { component } = render({
      fetchDetail: async () => {
        fetches += 1;
        if (fetches === 1) return DETAIL;
        // The server changed `rumors` underneath us; `concept` is untouched.
        return {
          ...DETAIL,
          character: { version: 9 },
          profile: { ...DETAIL.profile, 'profile.rumors': 'Someone else wrote this' },
        };
      },
      save: async () => {
        throw new ConnectError('stale', Code.Aborted);
      },
    });
    await settle();
    type(field('concept'), 'mine');
    type(field('rumors'), 'also mine');
    (content().querySelector('form') as HTMLFormElement).requestSubmit();
    await settle();
    expect(document.activeElement).toBe(field('rumors'));
    unmount(component);
  });

  it('renders the authored generic failure for any other refusal', async () => {
    const { component } = render({
      save: async () => {
        throw new ConnectError('CHARACTER_MASK_PATH_UNSUPPORTED: profile.secret', Code.Internal);
      },
    });
    await settle();
    type(field('concept'), 'x');
    (content().querySelector('form') as HTMLFormElement).requestSubmit();
    await settle();
    expect(content().textContent).toContain("Couldn't save. Try again.");
    expect(content().textContent).not.toContain('CHARACTER_MASK_PATH_UNSUPPORTED');
    expect(content().textContent).not.toContain('profile.secret');
    unmount(component);
  });
});

describe('EditCharacterSheet — the phone band is a Svelte derivation, not a CSS rule', () => {
  it('renders data-side=bottom when the 767px query matches', async () => {
    const { queries } = stubMatchMedia(true);
    const { component } = render();
    await settle();
    expect(content().getAttribute('data-side')).toBe('bottom');
    expect(queries).toContain('(max-width: 767px)');
    unmount(component);
  });

  it('renders data-side=right when the 767px query does not match', async () => {
    stubMatchMedia(false);
    const { component } = render();
    await settle();
    expect(content().getAttribute('data-side')).toBe('right');
    unmount(component);
  });

  it('flips on a change event from the MediaQueryList', async () => {
    const { listeners } = stubMatchMedia(false);
    const { component } = render();
    await settle();
    expect(content().getAttribute('data-side')).toBe('right');
    expect(listeners.length).toBeGreaterThan(0);
    for (const l of listeners) l({ matches: true });
    flushSync();
    expect(content().getAttribute('data-side')).toBe('bottom');
    unmount(component);
  });

  it('defaults to the desktop shape when matchMedia is absent', async () => {
    // No stub: this jsdom has no matchMedia at all. Flickering through the
    // bottom shape on a desktop first paint is the failure this guard closes.
    const { component } = render();
    await settle();
    expect(content().getAttribute('data-side')).toBe('right');
    unmount(component);
  });

  it('removes its MediaQueryList listener on teardown', async () => {
    const { listeners } = stubMatchMedia(false);
    const { component } = render();
    await settle();
    expect(listeners.length).toBeGreaterThan(0);
    unmount(component);
    flushSync();
    expect(listeners).toHaveLength(0);
  });
});

describe('EditCharacterSheet — D-109: no gesture is promised that bits-ui cannot honor', () => {
  it('renders no drag affordance and no separator above the header', async () => {
    // The PROPERTY, not a name for it: a grab handle promises swipe-dismiss,
    // and bits-ui has none.
    const { component } = render();
    await settle();
    expect(content().querySelector('[role="separator"]')).toBeNull();
    expect(content().querySelector('[aria-roledescription]')).toBeNull();
    // No back arrow either — that would make the sheet a route.
    expect(content().textContent).not.toMatch(/‹|←/);
    unmount(component);
  });

  it('offers Cancel and the generated close control as close paths', async () => {
    const closes: number[] = [];
    const { component } = render({ onclose: () => void closes.push(1) });
    await settle();
    expect(content().querySelector('[data-slot="sheet-close"]')).not.toBeNull();
    const cancel = content().querySelector('[data-testid="sheet-cancel"]') as HTMLButtonElement;
    expect(cancel).not.toBeNull();
    cancel.click();
    flushSync();
    expect(closes).toHaveLength(1);
    unmount(component);
  });
});

describe('EditCharacterSheet — form participation', () => {
  it('gives every control a name and the submit control type=submit', async () => {
    const { component } = render();
    await settle();
    const controls = content().querySelectorAll('input, select, textarea');
    expect(controls.length).toBeGreaterThan(13);
    for (const el of controls) {
      expect((el as HTMLInputElement).getAttribute('name')).toBeTruthy();
    }
    expect(saveButton().getAttribute('type')).toBe('submit');
    unmount(component);
  });
});
