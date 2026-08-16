// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/*
 * A REJECTED SUBMIT MUST COST THE PLAYER NOTHING BUT THE ROUND TRIP.
 *
 * Creation is submit-and-report (009-A): a pre-check cannot be honest across
 * check-then-insert, so a collision is learned only AFTER submitting. That
 * trade is defensible only while the round trip is cheap. A submit that
 * discards five typed fields to report one collision converts submit-and-report
 * into the hostile option, on the one surface where a first-time player forms
 * their impression — which is why the preservation specs read all six inputs
 * individually after each rejection rather than sampling one.
 *
 * THE ERROR SPECS ARE THE SHARP ONES, IN TWO DIRECTIONS.
 *
 * `InvalidArgument` is the ONE code whose server message is rendered — those
 * are authored player-facing strings from the name pipeline, including the two
 * distinct wordings for a submission with no visible characters. "Verbatim"
 * is load-bearing: ConnectError's `.message` is the raw string with a
 * `[code]` prefix bolted on, so the obvious spelling renders machine vocabulary
 * at a player in the one place the design promised not to.
 *
 * Every other code renders authored copy, and that spec asserts the ABSENCE of
 * the rejection's own message. A presence-only assertion passes with the leaked
 * text sitting right beside the authored line (T-05-06-03).
 *
 * THE COUNTER IS IN RUNES. Three unit systems collide in this phase: the name
 * is bounded in code points at 32, the five short profile values in BYTES at
 * 100, and the long ones at 4000. The astral fixture separates a code-point
 * count from a UTF-16 code-unit count — they agree on every ASCII fixture and
 * disagree for every player who writes outside it.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, tick, unmount } from 'svelte';
import { Code, ConnectError } from '@connectrpc/connect';
import CreateCharacterForm from './CreateCharacterForm.svelte';

const NAME_RULE =
	'Letters and single spaces, 2–32 characters. Full-width characters, invisible marks and extra spaces are folded, so the name you get may differ slightly from what you type.';
const NAME_REQUIRED = 'Enter a character name.';
const TAKEN_COPY = 'That name is taken. Try another.';
const INVALID_FALLBACK = "That name can't be used here.";
const GENERIC_COPY = "Couldn't create the character. Try again.";

const SUBMIT_LABEL = 'Create character';

/** The six field `name` attributes, in render order. */
const FIELD_NAMES = ['characterName', 'pronouns', 'concept', 'species', 'age', 'faction'] as const;

/** What the player typed, one distinct value per field so a swap cannot pass. */
const TYPED = {
	characterName: 'Wren',
	pronouns: 'she/her',
	concept: 'Archivist',
	species: 'Human',
	age: 'Thirties',
	faction: 'The Registry',
} as const;

type Submit = (fields: Record<string, string>) => Promise<unknown>;

function render(opts?: { submit?: Submit }) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(CreateCharacterForm, {
		target,
		props: { submit: opts?.submit ?? (() => Promise.resolve({})) },
	});
	const button = target.querySelector('button[type="submit"]') as HTMLButtonElement;
	return {
		target,
		button,
		field: (name: string) => target.querySelector(`[name="${name}"]`) as HTMLInputElement,
		alert: () => target.querySelector('[role="alert"]'),
		text: () => (target.textContent ?? '').replace(/\s+/g, ' ').trim(),
		dispose: () => {
			unmount(component);
			target.remove();
		},
	};
}

async function type(el: HTMLInputElement, value: string) {
	el.value = value;
	el.dispatchEvent(new Event('input', { bubbles: true }));
	await tick();
}

async function fillEverything(field: (n: string) => HTMLInputElement) {
	for (const n of FIELD_NAMES) {
		await type(field(n), TYPED[n]);
	}
}

async function submitForm(button: HTMLButtonElement) {
	button.click();
	await tick();
	await tick();
}

function deferred<T>() {
	let resolve!: (v: T) => void;
	const promise = new Promise<T>((res) => {
		resolve = res;
	});
	return { promise, resolve };
}

afterEach(() => document.body.replaceChildren());

describe('CreateCharacterForm', () => {
	it('renders six named inputs and one submit button carrying the authored label', () => {
		const { target, button, dispose } = render();

		for (const n of FIELD_NAMES) {
			expect(target.querySelector(`input[name="${n}"]`), n).not.toBeNull();
		}
		expect(target.querySelectorAll('input[name]').length).toBe(6);
		expect(button.getAttribute('type')).toBe('submit');
		expect(button.textContent?.trim()).toBe(SUBMIT_LABEL);
		dispose();
	});

	it('shows the static name rule on first render with no interaction and no validation copy', () => {
		const { text, alert, dispose } = render();

		expect(text()).toContain(NAME_RULE);
		// A wholly-unfilled form is the normal first state: nothing has been
		// submitted, so nothing is wrong yet.
		expect(alert()).toBeNull();
		expect(text()).not.toContain(NAME_REQUIRED);
		dispose();
	});

	it('does not call the submit callback on an empty name and renders the required-field copy', async () => {
		const submit = vi.fn<Submit>().mockResolvedValue({});
		const { button, alert, dispose } = render({ submit });

		await submitForm(button);

		expect(submit).not.toHaveBeenCalled();
		expect(alert()?.textContent).toContain(NAME_REQUIRED);
		dispose();
	});

	it('submits all six values once when the name is present', async () => {
		const submit = vi.fn<Submit>().mockResolvedValue({});
		const { button, field, dispose } = render({ submit });

		await fillEverything(field);
		await submitForm(button);

		expect(submit).toHaveBeenCalledTimes(1);
		expect(submit.mock.calls[0][0]).toEqual({
			name: 'Wren',
			pronouns: 'she/her',
			concept: 'Archivist',
			species: 'Human',
			age: 'Thirties',
			faction: 'The Registry',
		});
		dispose();
	});

	it('renders the authored taken copy on AlreadyExists and keeps every one of the six values', async () => {
		const submit = vi
			.fn<Submit>()
			.mockRejectedValue(new ConnectError('character name already taken', Code.AlreadyExists));
		const { button, field, alert, text, dispose } = render({ submit });

		await fillEverything(field);
		await submitForm(button);

		expect(alert()?.textContent).toContain(TAKEN_COPY);
		// The refusal names no colliding character, and this side adds nothing
		// to it (§6.1.2's enumeration oracle).
		expect(text()).not.toContain('character name already taken');
		for (const n of FIELD_NAMES) {
			expect(field(n).value, n).toBe(TYPED[n]);
		}
		dispose();
	});

	it('renders an InvalidArgument message verbatim and keeps every one of the six values', async () => {
		// One of the two authored empty-normal-form strings, which is the case
		// a player cannot otherwise distinguish from a blank box.
		const authored = 'that name contains no visible characters; please use letters';
		const submit = vi
			.fn<Submit>()
			.mockRejectedValue(new ConnectError(authored, Code.InvalidArgument));
		const { button, field, alert, text, dispose } = render({ submit });

		await fillEverything(field);
		await submitForm(button);

		expect(alert()?.textContent?.trim()).toBe(authored);
		// Verbatim means verbatim: no code prefix, no added framing.
		expect(text()).not.toContain('invalid_argument');
		expect(text()).not.toContain('[');
		for (const n of FIELD_NAMES) {
			expect(field(n).value, n).toBe(TYPED[n]);
		}
		dispose();
	});

	it('falls back to authored copy when an InvalidArgument carries no message', async () => {
		const submit = vi.fn<Submit>().mockRejectedValue(new ConnectError('', Code.InvalidArgument));
		const { button, field, alert, text, dispose } = render({ submit });

		await type(field('characterName'), 'Wren');
		await submitForm(button);

		expect(alert()?.textContent).toContain(INVALID_FALLBACK);
		expect(text()).not.toContain('invalid_argument');
		dispose();
	});

	it('renders the generic create-failure copy on any other code and never the rejection message', async () => {
		const submit = vi
			.fn<Submit>()
			.mockRejectedValue(new ConnectError('CHARACTER_CREATE_FAILED: pool exhausted', Code.Internal));
		const { button, field, alert, text, dispose } = render({ submit });

		await type(field('characterName'), 'Wren');
		await submitForm(button);

		expect(alert()?.textContent).toContain(GENERIC_COPY);
		expect(text()).not.toContain('CHARACTER_CREATE_FAILED');
		expect(text()).not.toContain('pool exhausted');
		expect(text()).not.toContain('internal');
		dispose();
	});

	it('moves focus to the name input after a rejection', async () => {
		const submit = vi
			.fn<Submit>()
			.mockRejectedValue(new ConnectError('nope', Code.AlreadyExists));
		const { button, field, dispose } = render({ submit });

		await fillEverything(field);
		// Focus starts elsewhere, so the assertion cannot pass by accident.
		field('faction').focus();
		await submitForm(button);

		expect(document.activeElement).toBe(field('characterName'));
		dispose();
	});

	it('keeps the button label byte-identical in flight and carries aria-busy only while the call is open', async () => {
		const gate = deferred<unknown>();
		const { button, field, dispose } = render({ submit: () => gate.promise });

		const before = button.textContent;
		expect(button.getAttribute('aria-busy')).toBeNull();

		await type(field('characterName'), 'Wren');
		await submitForm(button);

		expect(button.textContent).toBe(before);
		expect(button.getAttribute('aria-busy')).toBe('true');
		expect(button.disabled).toBe(true);

		gate.resolve({});
		await gate.promise;
		await tick();

		expect(button.textContent).toBe(before);
		expect(button.getAttribute('aria-busy')).toBeNull();
		expect(button.disabled).toBe(false);
		dispose();
	});

	it('counts the name in code points, not UTF-16 code units', async () => {
		// Astral code points: each is ONE rune but TWO UTF-16 code units, so a
		// counter reading the string's own length reports double for a player
		// writing outside the Basic Multilingual Plane — and is right for every
		// ASCII fixture anyone would check it against.
		//
		// The length is chosen to clear the 80%-of-cap display gate (26 >= 25.6);
		// below it the counter is intentionally absent and there is nothing to
		// read. The rune-vs-code-unit discrimination is unaffected — 26 and 52 are
		// still different numbers, which is the whole point of the assertion.
		const astral = '𝔄'.repeat(26);
		expect([...astral].length).toBe(26);
		expect(astral.length).toBe(52);

		const { target, field, dispose } = render();
		await type(field('characterName'), astral);

		const counter = target.querySelector('[data-testid="name-counter"]');
		expect(counter?.textContent?.trim()).toBe('26 / 32');
		dispose();
	});

	it('hides the name counter until the name is within 20% of the cap', async () => {
		const { target, field, dispose } = render();

		// Below the gate: no numeric chrome, matching the five sibling fields and
		// UI-SPEC:423's "authoring form, all fields blank | no copy".
		expect(target.querySelector('[data-testid="name-counter"]')).toBeNull();
		await type(field('characterName'), 'a'.repeat(25));
		expect(target.querySelector('[data-testid="name-counter"]')).toBeNull();

		// Positive control — without this the assertions above would also pass if
		// the counter had been deleted outright rather than gated.
		await type(field('characterName'), 'a'.repeat(26));
		const counter = target.querySelector('[data-testid="name-counter"]');
		expect(counter?.textContent?.trim()).toBe('26 / 32');
		dispose();
	});

	it('renders a byte counter for a short profile field at its own cap, not the name cap', async () => {
		const { target, field, dispose } = render();
		// 33 three-byte characters plus one ASCII byte is exactly 100 bytes.
		const atCap = '漢'.repeat(33) + 'x';
		expect(new TextEncoder().encode(atCap).length).toBe(100);

		await type(field('concept'), atCap);

		const counters = target.querySelectorAll('[data-testid="byte-counter"]');
		expect(counters.length).toBe(1);
		expect(counters[0].textContent?.trim()).toBe('100 / 100');
		expect(counters[0].getAttribute('data-over')).toBe('false');
		dispose();
	});
});
