// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/*
 * THE ONE PLACE THE CLIENT AND THE SERVER CAN SILENTLY DISAGREE ABOUT A LIMIT.
 *
 * `validateProfileValue` (internal/grpc/characteraccess_write.go:213-224)
 * compares `len(value) > maxBytes` on a Go string — that is BYTES. A client
 * counter written as `value.length` counts UTF-16 code units, so the two agree
 * on every ASCII value and disagree on every multi-byte one. Nothing in the
 * running game forces the agreement: no test drives a CJK profile field through
 * both halves, and a mismatch surfaces to the player as a field the counter
 * called acceptable and the server refused. This file IS the agreement (05-UI-SPEC
 * backstop row 2).
 *
 * Every fixture asserts its OWN encoded byte length before asserting the
 * counter's output. A fixture that drifts off 99 / 100 / 101 then fails at the
 * setup assertion, loudly, rather than quietly testing a boundary that is no
 * longer the boundary.
 *
 * The at-cap cases assert NOT-over deliberately: the server's comparison is
 * strictly greater-than, so a client that marked exactly-at-cap as over would
 * refuse a value the server accepts.
 */

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import ByteCounter from './ByteCounter.svelte';

// The two shipped caps, transcribed from internal/world/validation.go:19-20 via
// the facade's allowlist: seven short single-line fields cap at MaxNameLength,
// five long multi-paragraph fields and the description at MaxDescriptionLength.
const SHORT_CAP = 100;
const LONG_CAP = 4000;

// U+6F22 encodes to three UTF-8 bytes and one UTF-16 code unit, which is the
// whole discrimination: 33 of them are 99 bytes and 33 characters.
const THREE_BYTE = '漢';
// U+1D11E is outside the BMP: four UTF-8 bytes, TWO UTF-16 code units.
const ASTRAL = '𝄞';

function encodedLength(v: string): number {
	return new TextEncoder().encode(v).length;
}

function render(value: string, maxBytes: number) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(ByteCounter, { target, props: { value, maxBytes } });
	return {
		el: target.querySelector('[data-testid="byte-counter"]'),
		text: (target.textContent ?? '').replace(/\s+/g, ' ').trim(),
		dispose: () => {
			unmount(component);
			target.remove();
		},
	};
}

afterEach(() => document.body.replaceChildren());

describe('ByteCounter', () => {
	describe('at the 100-byte short-field cap', () => {
		it('reports 99 of 100 for a 99-byte multi-byte value and does not mark it over cap', () => {
			const value = THREE_BYTE.repeat(33);
			expect(encodedLength(value)).toBe(99);

			const { el, text, dispose } = render(value, SHORT_CAP);
			expect(text).toBe('99 / 100');
			expect(el?.getAttribute('data-over')).toBe('false');
			dispose();
		});

		it('reports exactly 100 of 100 at the cap and does not mark it over — the server compares strictly greater-than', () => {
			const value = THREE_BYTE.repeat(33) + 'x';
			expect(encodedLength(value)).toBe(100);

			const { el, text, dispose } = render(value, SHORT_CAP);
			expect(text).toBe('100 / 100');
			expect(el?.getAttribute('data-over')).toBe('false');
			dispose();
		});

		it('reports 101 of 100 one byte past the cap and marks it over', () => {
			const value = THREE_BYTE.repeat(33) + 'xx';
			expect(encodedLength(value)).toBe(101);

			const { el, text, dispose } = render(value, SHORT_CAP);
			expect(text).toBe('101 / 100');
			expect(el?.getAttribute('data-over')).toBe('true');
			dispose();
		});
	});

	describe('at the 4000-byte long-field cap', () => {
		it('reports 3999 of 4000 for a 3999-byte multi-byte value and does not mark it over cap', () => {
			const value = THREE_BYTE.repeat(1333);
			expect(encodedLength(value)).toBe(3999);

			const { el, text, dispose } = render(value, LONG_CAP);
			expect(text).toBe('3999 / 4000');
			expect(el?.getAttribute('data-over')).toBe('false');
			dispose();
		});

		it('reports exactly 4000 of 4000 at the cap and does not mark it over', () => {
			const value = THREE_BYTE.repeat(1333) + 'x';
			expect(encodedLength(value)).toBe(4000);

			const { el, text, dispose } = render(value, LONG_CAP);
			expect(text).toBe('4000 / 4000');
			expect(el?.getAttribute('data-over')).toBe('false');
			dispose();
		});

		it('reports 4001 of 4000 one byte past the cap and marks it over', () => {
			const value = THREE_BYTE.repeat(1333) + 'xx';
			expect(encodedLength(value)).toBe(4001);

			const { el, text, dispose } = render(value, LONG_CAP);
			expect(text).toBe('4001 / 4000');
			expect(el?.getAttribute('data-over')).toBe('true');
			dispose();
		});
	});

	it('renders nothing at all below 80% of the cap', () => {
		const value = 'x'.repeat(79);
		expect(encodedLength(value)).toBe(79);

		const { el, text, dispose } = render(value, SHORT_CAP);
		expect(el).toBeNull();
		expect(text).toBe('');
		dispose();
	});

	it('counts an ASCII value as its string length, so the common case is unchanged', () => {
		const value = 'x'.repeat(90);
		expect(encodedLength(value)).toBe(value.length);

		const { el, text, dispose } = render(value, SHORT_CAP);
		expect(text).toBe('90 / 100');
		expect(el?.getAttribute('data-over')).toBe('false');
		dispose();
	});

	it('counts an astral-plane character as its four UTF-8 bytes, not as its two UTF-16 code units', () => {
		const value = 'x'.repeat(80) + ASTRAL;
		expect(value.length).toBe(82);
		expect(encodedLength(value)).toBe(84);

		const { text, dispose } = render(value, SHORT_CAP);
		expect(text).toBe('84 / 100');
		dispose();
	});

	/*
	 * THE ANNOUNCEMENT REGION HAS TO OUTLIVE ITS OWN CONTENT.
	 *
	 * Assistive technologies announce mutations to a live region that is ALREADY
	 * in the accessibility tree; a region inserted wholesale together with its
	 * content is generally not announced. With `aria-live` inside the `{#if}`,
	 * the appearance moment — the one the component's comment leads with — was
	 * never announced, while the over-cap flip was, because by then the region
	 * already existed. These drive a live prop change so the SAME node can be
	 * identified across the transition; a re-mount would prove nothing.
	 */
	describe('the announcement region', () => {
		function live(target: HTMLElement) {
			return target.querySelector('[aria-live]');
		}

		it('is already in the tree while the counter is still below its display threshold', () => {
			const props = $state({ value: 'x'.repeat(79), maxBytes: SHORT_CAP });
			const target = document.createElement('div');
			document.body.appendChild(target);
			const component = mount(ByteCounter, { target, props });

			const region = live(target);
			expect(region).not.toBeNull();
			expect(region!.textContent?.trim()).toBe('');
			// The DISPLAY rule is untouched: nothing visible below 80% of cap.
			expect(target.querySelector('[data-testid="byte-counter"]')).toBeNull();
			expect((target.textContent ?? '').replace(/\s+/g, ' ').trim()).toBe('');

			props.value = 'x'.repeat(80);
			flushSync();

			// THE SAME NODE, now populated — an announceable mutation.
			expect(live(target)).toBe(region);
			expect(region!.textContent?.trim()).toBe('80 / 100');
			expect(
				target.querySelector('[data-testid="byte-counter"]')?.textContent?.trim(),
			).toBe('80 / 100');

			unmount(component);
			target.remove();
		});

		it('carries the over-cap flip through that same region', () => {
			const atCap = THREE_BYTE.repeat(33) + 'x';
			expect(encodedLength(atCap)).toBe(100);
			const props = $state({ value: atCap, maxBytes: SHORT_CAP });
			const target = document.createElement('div');
			document.body.appendChild(target);
			const component = mount(ByteCounter, { target, props });

			const region = live(target);
			expect(region!.textContent?.trim()).toBe('100 / 100');
			expect(
				target.querySelector('[data-testid="byte-counter"]')?.getAttribute('data-over'),
			).toBe('false');

			props.value = atCap + 'x';
			flushSync();

			expect(live(target)).toBe(region);
			expect(region!.textContent?.trim()).toBe('101 / 100');
			expect(
				target.querySelector('[data-testid="byte-counter"]')?.getAttribute('data-over'),
			).toBe('true');

			unmount(component);
			target.remove();
		});

		it('carries the value exactly once, with no duplicate copy in the tree', () => {
			// A separate hidden region would announce correctly and read the number
			// twice. The region IS the counter.
			const { dispose } = render('x'.repeat(90), SHORT_CAP);
			const regions = document.body.querySelectorAll('[aria-live]');
			expect(regions).toHaveLength(1);
			expect(regions[0]).toBe(document.body.querySelector('[data-testid="byte-counter"]'));
			expect(document.body.textContent?.replace(/\s+/g, ' ').trim()).toBe('90 / 100');
			dispose();
		});
	});
});
