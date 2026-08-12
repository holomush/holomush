// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/*
 * THE ABSENCE CONTRACT, ASSERTED.
 *
 * A blank field and a WITHHELD field must be indistinguishable, because the
 * difference discloses what *this viewer* may not see. 01-SPEC §8.9 makes that
 * a wire property — projectPublic drops an empty value rather than emitting it
 * present-and-empty — and this file pins the client half: the renderer draws
 * exactly the keys the response carried and holds no list of expected names to
 * diff against.
 *
 * The minimal-profile spec below asserts the FULL normalised textContent rather
 * than a substring, deliberately. A substring assertion passes with a stray
 * count, a lock glyph, an "N hidden" line or a heading with no body sitting
 * beside it — every one of which is the disclosure the contract exists to
 * prevent.
 */

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import PublicProfile from './PublicProfile.svelte';
import type { PublicCharacter } from '$lib/connect/holomush/characteraccess/v1/characteraccess_pb';

function character(p: Partial<PublicCharacter>): PublicCharacter {
	return {
		$typeName: 'holomush.characteraccess.v1.PublicCharacter',
		id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
		name: 'Bazian',
		description: '',
		profile: {},
		gallery: [],
		...p,
	};
}

function render(c: PublicCharacter): { text: string; html: string; target: HTMLElement; dispose: () => void } {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(PublicProfile, { target, props: { character: c } });
	return {
		text: (target.textContent ?? '').replace(/\s+/g, ' ').trim(),
		html: target.innerHTML,
		target,
		dispose: () => {
			unmount(component);
			target.remove();
		},
	};
}

// The two named slots are viewer-INVARIANT — they discose only "the platform
// has not built this yet", which is public — so D-94's named-slot rule governs
// them rather than the absence rule, and they appear on every rendering.
const NAMED_SLOTS = 'Direct messages Not available yet. Sheet No sheet system yet.';

afterEach(() => document.body.replaceChildren());

describe('PublicProfile', () => {
	it('renders a name-and-pronouns profile as a complete card and adds not one other word', () => {
		const { text, dispose } = render(
			character({ name: 'Bazian', profile: { 'profile.pronouns': 'she/her' } }),
		);
		// The leading `B` is the aria-hidden portrait glyph, which is in the DOM
		// text even though it is hidden from assistive technology.
		expect(text).toBe(`B Bazian she/her ${NAMED_SLOTS}`);
		dispose();
	});

	it('renders no description element when the description is empty', () => {
		const { target, dispose } = render(character({ description: '' }));
		expect(target.querySelector('[data-testid="description"]')).toBeNull();
		dispose();
	});

	it('renders the description exactly as the response carried it, wrapping rather than truncating', () => {
		const prose = 'A tall figure.\n\n  Two spaces, one blank line, and a trailing tab.\t';
		const { target, dispose } = render(character({ description: prose }));
		expect(target.querySelector('[data-testid="description"]')?.textContent).toBe(prose);
		dispose();
	});

	it('renders present fields and no heading at all for an absent one', () => {
		const { text, html, dispose } = render(
			character({
				profile: {
					'profile.pronouns': 'they/them',
					'profile.rumors': 'Owes money in three ports.',
					'profile.currently': 'Laying low.',
				},
			}),
		);
		expect(text).toContain('Owes money in three ports.');
		expect(text).toContain('Laying low.');
		// Absent means absent: no heading, no placeholder, no "N hidden".
		expect(html).not.toContain('Biography');
		expect(html).not.toContain('Appearance');
		expect(html).not.toContain('Personality');
		expect(text).not.toMatch(/hidden|withheld|private|\d+ (field|more)/i);
		dispose();
	});

	it('orders the five long-form sections with out-of-character last', () => {
		const { html, dispose } = render(
			character({
				profile: {
					'profile.appearance': 'Weathered.',
					'profile.personality': 'Wry.',
					'profile.biography': 'Born inland.',
					'profile.rumors': 'Owes money.',
					'profile.rp_preferences': 'Prefers slow scenes.',
				},
			}),
		);
		const at = (s: string) => html.indexOf(s);
		expect(at('Appearance')).toBeGreaterThan(-1);
		expect(at('Appearance')).toBeLessThan(at('Personality'));
		expect(at('Personality')).toBeLessThan(at('Biography'));
		expect(at('Biography')).toBeLessThan(at('Rumours'));
		// profile.rp_preferences is OUT-OF-CHARACTER text about the player. It is
		// labelled so, and it renders after every in-character section.
		expect(at('Rumours')).toBeLessThan(at('Out of character'));
		expect(at('Prefers slow scenes.')).toBeGreaterThan(at('Owes money.'));
		dispose();
	});

	it('renders the five short fields as fact pills carrying their full values', () => {
		const { target, text, html, dispose } = render(
			character({
				profile: {
					'profile.species': 'Tiefling',
					'profile.age': 'Somewhere past forty',
					'profile.faction': 'The Longshore Compact',
					'profile.currently': 'Between contracts, mostly sober',
					'profile.timezone': 'UTC+2 (evenings)',
				},
			}),
		);
		for (const value of [
			'Tiefling',
			'Somewhere past forty',
			'The Longshore Compact',
			'Between contracts, mostly sober',
			'UTC+2 (evenings)',
		]) {
			expect(text).toContain(value);
		}
		// The row wraps; it does not ellipsize. A truncated pill would make a long
		// value indistinguishable from a shorter one.
		expect(target.querySelector('[data-testid="fact-pills"]')).not.toBeNull();
		expect(html).not.toMatch(/text-overflow|truncate|ellipsis/);
		dispose();
	});

	it('renders the sheet as a named empty section for every input, minimal included', () => {
		for (const c of [
			character({ profile: { 'profile.pronouns': 'she/her' } }),
			character({ description: 'Full.', profile: { 'profile.biography': 'Long.' } }),
		]) {
			const { target, dispose } = render(c);
			const sheet = target.querySelector('[data-testid="sheet"]');
			expect(sheet).not.toBeNull();
			expect((sheet?.textContent ?? '').replace(/\s+/g, ' ').trim()).toBe('Sheet No sheet system yet.');
			dispose();
		}
	});

	it('renders the web-DM slot as a non-interactive labelled slot — never a control, never a dead link', () => {
		const { target, dispose } = render(character({}));
		const slot = target.querySelector('[data-testid="web-dm"]');
		expect(slot).not.toBeNull();
		expect(slot?.querySelector('button')).toBeNull();
		expect(slot?.querySelector('a')).toBeNull();
		expect(slot?.querySelector('[disabled]')).toBeNull();
		expect(slot?.hasAttribute('disabled')).toBe(false);
		expect((slot?.textContent ?? '').replace(/\s+/g, ' ').trim()).toBe(
			'Direct messages Not available yet.',
		);
		dispose();
	});

	it('renders nothing for a profile key it does not lay out, and does not crash', () => {
		const { text, dispose } = render(
			character({
				name: 'Bazian',
				profile: { 'profile.pronouns': 'she/her', 'profile.unlaid_out': 'SENTINEL_VALUE' },
			}),
		);
		// The client renders what arrived and asserts nothing about what should
		// have: an unlaid-out key is neither drawn nor reported as unexpected.
		expect(text).not.toContain('SENTINEL_VALUE');
		expect(text).toBe(`B Bazian she/her ${NAMED_SLOTS}`);
		dispose();
	});

	it('renders no interactive element of its own — the only control on this surface lives in the media renderer', () => {
		const { target, dispose } = render(
			character({
				description: 'Full.',
				profile: {
					'profile.pronouns': 'she/her',
					'profile.concept': 'Dock rat turned broker',
					'profile.species': 'Tiefling',
					'profile.biography': 'Born inland.',
				},
			}),
		);
		expect(target.querySelector('button')).toBeNull();
		expect(target.querySelector('a')).toBeNull();
		dispose();
	});
});
