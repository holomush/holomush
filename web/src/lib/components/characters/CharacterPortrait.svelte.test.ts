// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/*
 * THE PLATE'S GLYPH IS THE NAME'S FIRST CODE POINT.
 *
 * The component's doc block promises the first character reaches the DOM as
 * the stored bytes carried it. `charAt(0)` cannot keep that promise: it
 * returns one UTF-16 code unit, so for any name beginning with an
 * astral-plane letter it hands back a LONE HIGH SURROGATE — half a character,
 * which renders as U+FFFD.
 *
 * The astral spec below is the load-bearing one. It compares against the WHOLE
 * character rather than merely asserting non-empty, because a lone surrogate is
 * non-empty and would satisfy any weaker check. `internal/charname/syntax`
 * admits `^[\p{L}]+( [\p{L}]+)*$`, and Go's `\p{L}` includes Deseret, Adlam,
 * Osage and Gothic — so an astral first letter is an admissible name, not a
 * hypothetical.
 */

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import CharacterPortrait from './CharacterPortrait.svelte';

/** U+10400 DESERET CAPITAL LETTER LONG I — category Lu, two UTF-16 code units. */
const DESERET_LONG_I = '\u{10400}';

function render(name: string, size?: number): { text: string; dispose: () => void } {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(CharacterPortrait, { target, props: size === undefined ? { name } : { name, size } });
	return {
		text: target.textContent ?? '',
		dispose: () => {
			unmount(component);
			target.remove();
		},
	};
}

afterEach(() => document.body.replaceChildren());

describe('CharacterPortrait', () => {
	it('draws the first letter of a Basic-Multilingual-Plane name', () => {
		expect(render('Bazian').text).toBe('B');
	});

	it('draws the WHOLE astral-plane first letter rather than a lone surrogate', () => {
		const { text } = render(`${DESERET_LONG_I}shan`);
		expect(text).toBe(DESERET_LONG_I);
		// The discriminating clause: a lone high surrogate is one code unit and
		// would pass a bare equality against `text[0]`, so the length is asserted
		// against the code point's own width.
		expect(text.length).toBe(2);
		expect(text).not.toBe(DESERET_LONG_I.charAt(0));
	});

	it('does not case the glyph in script, leaving that to text-transform', () => {
		// 01-SPEC §8.8 promises a name, not a cased one. Uppercasing here would
		// rewrite scripts that have no case at all.
		expect(render('ada').text).toBe('a');
	});

	it('draws no glyph at all for an empty name rather than the string undefined', () => {
		const { text } = render('');
		expect(text).toBe('');
		expect(text).not.toContain('undefined');
	});
});
