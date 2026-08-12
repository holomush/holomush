// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/*
 * THIS FILE IS THE ONLY WAY ANY OF THIS BEHAVIOUR IS EVER EXERCISED.
 *
 * 01-SPEC §7.3 ships the profile media MODEL — `profile.image.primary` and the
 * ten `profile.image.gallery.NN` slot names — with ZERO upload behaviour, and
 * nothing in v0.13 mints a media identifier. So `projectPublic`
 * (internal/grpc/characteraccess_projection.go) emits no image row in this
 * release, and no amount of playing the game can reach `ProfileMedia`'s render
 * path, its content-warning gate or its broken-handle fallback.
 *
 * That makes these assertions the 05-UI-SPEC backstop row rather than a
 * regression net: they are the specification of a renderer whose first real
 * input arrives in a later milestone. Deleting one does not make a test suite
 * faster — it deletes the only statement of what the renderer must do.
 */

import { afterEach, describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import ProfileMedia from './ProfileMedia.svelte';
import CharacterPortrait from './CharacterPortrait.svelte';
import type { ProfileImage } from '$lib/connect/holomush/characteraccess/v1/characteraccess_pb';

function image(p: Partial<ProfileImage>): ProfileImage {
	return {
		$typeName: 'holomush.characteraccess.v1.ProfileImage',
		mediaId: 'm-primary',
		altText: '',
		contentWarning: '',
		...p,
	};
}

type Mounted = { target: HTMLElement; dispose: () => void };

function mountMedia(props: {
	primaryImage?: ProfileImage;
	gallery?: ProfileImage[];
	name?: string;
}): Mounted {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(ProfileMedia, {
		target,
		props: { gallery: [], name: 'Bazian', ...props },
	});
	return {
		target,
		dispose: () => {
			unmount(component);
			target.remove();
		},
	};
}

/*
 * Svelte 5 emits an `<!---->` anchor comment around every `{#if}` / `{#each}`
 * block, so two renderings of the SAME element differ in raw innerHTML purely
 * by that bookkeeping. Stripping the anchors and collapsing whitespace leaves
 * the markup itself — every tag, attribute and text node — under exact
 * comparison, which is the property worth pinning.
 */
function markup(target: HTMLElement): string {
	return target.innerHTML.replace(/<!---->/g, '').replace(/\s+/g, ' ').trim();
}

function text(target: HTMLElement): string {
	return (target.textContent ?? '').replace(/\s+/g, ' ').trim();
}

function portraitMarkup(name: string, size: number): string {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(CharacterPortrait, { target, props: { name, size } });
	const html = markup(target);
	unmount(component);
	target.remove();
	return html;
}

afterEach(() => document.body.replaceChildren());

describe('ProfileMedia', () => {
	it('renders no element at all when there is neither a primary image nor a gallery row', () => {
		const { target, dispose } = mountMedia({ primaryImage: undefined, gallery: [] });
		// No wrapper, no heading, no slot, no placeholder — not one element and
		// not one character of text. A dashed "no images" slot is precisely the
		// signal the absence contract forbids.
		expect(target.querySelector('*')).toBeNull();
		expect(markup(target)).toBe('');
		expect(text(target)).toBe('');
		dispose();
	});

	it('renders the primary image with a src derived from media_id and alt taken from alt_text', () => {
		const { target, dispose } = mountMedia({
			primaryImage: image({ mediaId: 'abc123', altText: 'A tall figure in a grey coat.' }),
		});
		const img = target.querySelector('img');
		expect(img).not.toBeNull();
		expect(img?.getAttribute('src')).toContain('abc123');
		expect(img?.getAttribute('alt')).toBe('A tall figure in a grey coat.');
		dispose();
	});

	it('renders an empty alt when alt_text is empty, never a fabricated description', () => {
		const { target, dispose } = mountMedia({
			primaryImage: image({ mediaId: 'abc123', altText: '' }),
		});
		const img = target.querySelector('img');
		expect(img?.getAttribute('alt')).toBe('');
		// Nothing anywhere in the markup stands in for the missing description.
		expect(text(target)).toBe('');
		dispose();
	});

	it('blurs an image carrying a content warning behind a reveal control whose accessible name is the warning', () => {
		const warning = 'Depicts a battlefield injury.';
		const { target, dispose } = mountMedia({
			primaryImage: image({ mediaId: 'cw1', altText: 'A field medic at work.', contentWarning: warning }),
		});
		const img = target.querySelector('img');
		const control = target.querySelector('button');
		expect(img?.classList.contains('warned')).toBe(true);
		expect(control).not.toBeNull();
		expect((control?.textContent ?? '').trim()).toBe(warning);
		expect(control?.getAttribute('aria-expanded')).toBe('false');

		control?.click();
		flushSync();

		expect(target.querySelector('img')?.classList.contains('warned')).toBe(false);
		expect(target.querySelector('button')?.getAttribute('aria-expanded')).toBe('true');
		dispose();
	});

	it('falls back to the identical initial-letter portrait when the primary image fails to load', () => {
		const { target, dispose } = mountMedia({
			primaryImage: image({ mediaId: 'gone', altText: 'A portrait.' }),
			name: 'bazian',
		});
		const img = target.querySelector('img');
		expect(img).not.toBeNull();

		img?.dispatchEvent(new Event('error'));
		flushSync();

		expect(target.querySelector('img')).toBeNull();
		// Identical to what a bare CharacterPortrait mount produces: a broken
		// handle degrades to exactly the no-media rendering, not to a broken-image
		// glyph and not to a second, subtly different placeholder.
		expect(markup(target)).toBe(portraitMarkup('bazian', 80));
		dispose();
	});

	it('renders gallery rows in the order the array carried, not sorted or re-keyed', () => {
		const gallery = ['09', '00', '05', '01', '02', '03', '04', '06', '07', '08'].map((slot) =>
			image({ mediaId: `g-${slot}`, altText: `slot ${slot}` }),
		);
		const { target, dispose } = mountMedia({ gallery });
		const srcs = Array.from(target.querySelectorAll('img')).map((el) => el.getAttribute('src') ?? '');
		expect(srcs).toHaveLength(10);
		expect(srcs.map((s) => s.slice(s.lastIndexOf('/') + 1))).toEqual(gallery.map((g) => g.mediaId));
		dispose();
	});

	it('treats gallery media names as exact bytes — two ids differing only by a zero are two rows', () => {
		const gallery = [image({ mediaId: 'g-0' }), image({ mediaId: 'g-00' })];
		const { target, dispose } = mountMedia({ gallery });
		const srcs = Array.from(target.querySelectorAll('img')).map((el) => el.getAttribute('src') ?? '');
		expect(srcs).toHaveLength(2);
		expect(srcs[0]).not.toBe(srcs[1]);
		dispose();
	});
});

describe('CharacterPortrait', () => {
	it('renders exactly one glyph and leaves the source name unmutated, so CSS owns the casing', () => {
		const target = document.createElement('div');
		document.body.appendChild(target);
		const component = mount(CharacterPortrait, { target, props: { name: 'bazian', size: 80 } });
		// The DOM text is the SOURCE name's first character UNCHANGED. A
		// `toUpperCase()` in the script block fails here; `text-transform:
		// uppercase` in the stylesheet passes. §8.8 guarantees a letter exists,
		// not that it is uppercase, so mutating the string claims more than the
		// spec does.
		expect(target.textContent).toBe('b');
		unmount(component);
		target.remove();
	});

	it('marks the plate aria-hidden because the name is adjacent and the letter is decorative', () => {
		const target = document.createElement('div');
		document.body.appendChild(target);
		const component = mount(CharacterPortrait, { target, props: { name: 'Bazian', size: 44 } });
		expect(target.firstElementChild?.getAttribute('aria-hidden')).toBe('true');
		unmount(component);
		target.remove();
	});
});
