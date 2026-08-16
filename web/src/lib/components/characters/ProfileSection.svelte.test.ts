// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/*
 * ONE SECTION IS THE UNIT OF FAILURE.
 *
 * UpdateCharacterProfile and UpdateCharacterDescription guard the SAME
 * characters.version and NO transaction spans them, so a whole-form save is a
 * two-call chain whose second call can lose a concurrent edit after the first
 * has committed. The specs below pin the properties that make per-section save
 * actually deliver what that buys: the mask a section sends, the isolation of
 * one section's conflict from its neighbours, and the refusal to hand a
 * server-authored string to the player.
 *
 * The error specs are the sharp ones. `CHARACTER_MASK_PATH_UNSUPPORTED` and
 * `CHARACTER_VERSION_REQUIRED` are CLIENT BUGS, not player errors — rendering
 * either at the player is threat T-05-04-04 — so the generic spec asserts the
 * absence of the server's message rather than merely the presence of the
 * authored copy. A presence-only assertion passes with the leaked text sitting
 * right beside the authored line.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, tick, unmount } from 'svelte';
import { Code, ConnectError } from '@connectrpc/connect';
import ProfileSection from './ProfileSection.svelte';

const CONFLICT_COPY =
	'This character changed somewhere else. Reload to get the latest, then re-apply your edits.';
const GENERIC_COPY = "Couldn't save. Try again.";

// The short cap, transcribed from world.MaxNameLength via the facade allowlist.
const SHORT_CAP = 100;

function identityFields() {
	return [
		{ path: 'profile.pronouns', label: 'Pronouns', name: 'pronouns', kind: 'line' as const, maxBytes: SHORT_CAP },
		{ path: 'profile.concept', label: 'Concept', name: 'concept', kind: 'line' as const, maxBytes: SHORT_CAP },
	];
}

type Save = (paths: string[], values: Record<string, string>) => Promise<unknown>;

function render(opts?: {
	save?: Save;
	values?: Record<string, string>;
	fields?: ReturnType<typeof identityFields>;
	saveLabel?: string;
}) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(ProfileSection, {
		target,
		props: {
			heading: 'Identity',
			saveLabel: opts?.saveLabel ?? 'Save identity',
			fields: opts?.fields ?? identityFields(),
			values: opts?.values ?? { 'profile.pronouns': 'she/her', 'profile.concept': 'Archivist' },
			save: opts?.save ?? (() => Promise.resolve({})),
		},
	});
	const button = target.querySelector('button[type="submit"]') as HTMLButtonElement;
	return {
		target,
		button,
		field: (name: string) =>
			target.querySelector(`[name="${name}"]`) as HTMLInputElement | HTMLTextAreaElement,
		status: () => target.querySelector('[role="status"]')?.textContent?.trim() ?? '',
		alert: () => target.querySelector('[role="alert"]'),
		text: () => (target.textContent ?? '').replace(/\s+/g, ' ').trim(),
		dispose: () => {
			unmount(component);
			target.remove();
		},
	};
}

async function type(el: HTMLInputElement | HTMLTextAreaElement, value: string) {
	el.value = value;
	el.dispatchEvent(new Event('input', { bubbles: true }));
	await tick();
}

async function submit(button: HTMLButtonElement) {
	button.click();
	await tick();
}

/** A promise whose settlement this test controls, so the in-flight DOM is observable. */
function deferred<T>() {
	let resolve!: (v: T) => void;
	let reject!: (e: unknown) => void;
	const promise = new Promise<T>((res, rej) => {
		resolve = res;
		reject = rej;
	});
	return { promise, resolve, reject };
}

afterEach(() => document.body.replaceChildren());

describe('ProfileSection', () => {
	it('disables its Save while every field still equals its loaded value', () => {
		const { button, dispose } = render();
		expect(button.disabled).toBe(true);
		dispose();
	});

	it('re-disables its Save when an edit is typed back to the loaded value', async () => {
		const { button, field, dispose } = render();
		await type(field('pronouns'), 'they/them');
		expect(button.disabled).toBe(false);

		await type(field('pronouns'), 'she/her');
		expect(button.disabled).toBe(true);
		dispose();
	});

	it('enables only its own Save when one of two mounted sections is edited', async () => {
		const one = render();
		const two = render({ saveLabel: 'Save out of character' });

		await type(one.field('pronouns'), 'they/them');

		expect(one.button.disabled).toBe(false);
		expect(two.button.disabled).toBe(true);
		one.dispose();
		two.dispose();
	});

	it('sends exactly the paths it declares, including the field the player never touched', async () => {
		const save = vi.fn<Save>().mockResolvedValue({});
		const { button, field, dispose } = render({ save });

		await type(field('pronouns'), 'they/them');
		await submit(button);

		expect(save).toHaveBeenCalledTimes(1);
		const [paths, values] = save.mock.calls[0];
		// The mask, not the value, is what selects what is written — so the
		// untouched `concept` still travels under its own path.
		expect(paths).toEqual(['profile.pronouns', 'profile.concept']);
		expect(values).toEqual({ 'profile.pronouns': 'they/them', 'profile.concept': 'Archivist' });
		dispose();
	});

	it('keeps the button label byte-identical in flight and carries aria-busy only while the call is open', async () => {
		const gate = deferred<unknown>();
		const { button, field, dispose } = render({ save: () => gate.promise });

		const before = button.textContent;
		expect(button.getAttribute('aria-busy')).toBeNull();

		await type(field('pronouns'), 'they/them');
		await submit(button);

		expect(button.textContent).toBe(before);
		expect(button.getAttribute('aria-busy')).toBe('true');
		expect(button.disabled).toBe(true);

		gate.resolve({});
		await gate.promise;
		await tick();

		expect(button.textContent).toBe(before);
		expect(button.getAttribute('aria-busy')).toBeNull();
		dispose();
	});

	it('renders Saved. in its own status region and clears it on the next edit', async () => {
		const { button, field, status, dispose } = render();

		await type(field('pronouns'), 'they/them');
		await submit(button);
		await tick();
		expect(status()).toBe('Saved.');

		await type(field('concept'), 'Cartographer');
		expect(status()).toBe('');
		dispose();
	});

	it('renders the concurrent-edit copy and a Reload control on Aborted, preserving the typed text', async () => {
		const save = vi.fn<Save>().mockRejectedValue(new ConnectError('conflict', Code.Aborted));
		const { button, field, alert, target, dispose } = render({ save });

		await type(field('pronouns'), 'they/them');
		await submit(button);
		await tick();

		expect(alert()?.textContent).toContain(CONFLICT_COPY);
		const reload = Array.from(target.querySelectorAll('button')).find(
			(b) => b.textContent?.trim() === 'Reload',
		);
		expect(reload).toBeDefined();
		// The typing survives the refusal: the player re-applies it, they do not
		// retype it.
		expect(field('pronouns').value).toBe('they/them');
		expect(field('concept').value).toBe('Archivist');
		dispose();
	});

	it('renders the generic copy on InvalidArgument and never the server-supplied message', async () => {
		// The shape a client bug actually arrives as: the facade refuses an
		// unlisted mask path with InvalidArgument and a message naming the code.
		const save = vi
			.fn<Save>()
			.mockRejectedValue(
				new ConnectError('CHARACTER_MASK_PATH_UNSUPPORTED: profile.secret', Code.InvalidArgument),
			);
		const { button, field, alert, text, dispose } = render({ save });

		await type(field('pronouns'), 'they/them');
		await submit(button);
		await tick();

		expect(alert()?.textContent).toContain(GENERIC_COPY);
		expect(text()).not.toContain('CHARACTER_MASK_PATH_UNSUPPORTED');
		expect(text()).not.toContain('profile.secret');
		expect(text()).not.toContain(CONFLICT_COPY);
		dispose();
	});

	it('moves focus to its first field after a failed save', async () => {
		const save = vi.fn<Save>().mockRejectedValue(new ConnectError('nope', Code.InvalidArgument));
		const { button, field, dispose } = render({ save });

		await type(field('concept'), 'Cartographer');
		await submit(button);
		await tick();

		expect(document.activeElement).toBe(field('pronouns'));
		dispose();
	});

	it('moves focus to its first field after a concurrent-edit refusal too', async () => {
		const save = vi.fn<Save>().mockRejectedValue(new ConnectError('conflict', Code.Aborted));
		const { button, field, dispose } = render({ save });

		await type(field('concept'), 'Cartographer');
		await submit(button);
		await tick();

		expect(document.activeElement).toBe(field('pronouns'));
		dispose();
	});

	it('renders a byte counter per field and still permits the Save at exactly the cap', async () => {
		const { button, field, target, dispose } = render();
		// 33 three-byte characters plus one ASCII byte is exactly 100 bytes — the
		// server compares strictly greater-than, so this value is acceptable.
		const atCap = '漢'.repeat(33) + 'x';
		expect(new TextEncoder().encode(atCap).length).toBe(SHORT_CAP);

		await type(field('pronouns'), atCap);
		await type(field('concept'), atCap);

		const counters = target.querySelectorAll('[data-testid="byte-counter"]');
		expect(counters.length).toBe(2);
		expect(counters[0].textContent?.trim()).toBe('100 / 100');
		expect(counters[0].getAttribute('data-over')).toBe('false');
		expect(button.disabled).toBe(false);
		dispose();
	});
});
