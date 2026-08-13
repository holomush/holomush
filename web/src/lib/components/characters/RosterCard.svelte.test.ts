// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/*
 * THE SUPPRESSION RULE, ASSERTED IN THE TEMPLATE RATHER THAN IN THE DATA.
 *
 * A character whose lifecycle is not `active` wears the `Retired` badge and NO
 * session word. `Retired · Offline` is meaningless, and the two vocabularies
 * collide on the token `active` (sketch 008 Finding, D-96). The load-bearing
 * spec below is the one that supplies session data DELIBERATELY and still
 * demands the session vocabulary be absent: it is what fails if a future
 * refactor moves the suppression upstream into the join, where it would hold
 * only for as long as the join keeps remembering to do it.
 *
 * The absence assertions read `textContent`, not a class list. A class-based
 * assertion passes with the word rendered beside a differently-styled badge.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount } from 'svelte';
import RosterCard from './RosterCard.svelte';

/** Every word the session vocabulary can put on a card. */
const SESSION_WORDS = /\b(active|offline|online|playing)\b/i;

type Props = {
	id: string;
	name: string;
	status: string;
	session?: { hasActiveSession: boolean };
	isDefault?: boolean;
	playable?: boolean;
	savingDefault?: boolean;
	onselect?: (id: string) => void;
	onmakedefault?: (id: string) => void;
};

function props(p: Partial<Props>): Props {
	return {
		id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
		name: 'Bazian',
		status: 'active',
		isDefault: false,
		playable: true,
		...p,
	};
}

function render(p: Props): { text: string; html: string; target: HTMLElement; dispose: () => void } {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(RosterCard, { target, props: p });
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

afterEach(() => document.body.replaceChildren());

describe('RosterCard', () => {
	it('shows the Default badge alongside the active session badge for a default, connected character', () => {
		const { text } = render(
			props({ status: 'active', isDefault: true, session: { hasActiveSession: true } }),
		);
		expect(text).toContain('Default');
		expect(text).toContain('Active');
	});

	it('shows the Default badge alongside the offline session badge for a default, disconnected character', () => {
		const { text } = render(
			props({ status: 'active', isDefault: true, session: { hasActiveSession: false } }),
		);
		expect(text).toContain('Default');
		expect(text).toContain('Offline');
	});

	it('shows the session badge alone and the Make default control for a non-default active character', () => {
		const { text, target } = render(
			props({ status: 'active', isDefault: false, session: { hasActiveSession: true } }),
		);
		expect(text).toContain('Active');
		expect(text).not.toContain('Default badge');
		expect(target.querySelector('[data-testid="default-badge"]')).toBeNull();
		expect(target.querySelector('[data-testid="make-default"]')).not.toBeNull();
	});

	it('shows the Retired badge ONLY for a retired character, with no session word', () => {
		const { text, target } = render(props({ status: 'retired', playable: false }));
		expect(text).toContain('Retired');
		expect(text).not.toMatch(SESSION_WORDS);
		expect(target.querySelector('[data-testid="make-default"]')).toBeNull();
	});

	it('renders no session word for a retired character even when session data IS supplied', () => {
		// The suppression is in the template. If it ever moves into the join,
		// this spec is what notices.
		const { text } = render(
			props({ status: 'retired', playable: false, session: { hasActiveSession: true } }),
		);
		expect(text).toContain('Retired');
		expect(text).not.toMatch(SESSION_WORDS);
	});

	it('renders no session word and no bespoke prose for the unreachable idle lifecycle', () => {
		// `idle` has no transition into it in v0.13 (01-SPEC §4.2), so the branch
		// exists and says nothing a player was ever promised.
		const { text } = render(
			props({ status: 'idle', playable: false, session: { hasActiveSession: true } }),
		);
		expect(text).not.toMatch(SESSION_WORDS);
		expect(text).not.toMatch(/idle/i);
	});

	it('renders without throwing and without a session badge on an unrecognised lifecycle', () => {
		const { text } = render(
			props({ status: 'wat', playable: false, session: { hasActiveSession: true } }),
		);
		expect(text).toContain('Bazian');
		expect(text).not.toMatch(SESSION_WORDS);
	});

	it('makes the whole surface of a playable card clickable and reports the character id', () => {
		const onselect = vi.fn();
		const { target } = render(props({ playable: true, onselect }));
		const card = target.querySelector<HTMLElement>('[data-testid="roster-card"]');
		expect(card).not.toBeNull();
		expect(card?.getAttribute('role')).toBe('button');
		card?.click();
		expect(onselect).toHaveBeenCalledWith('01ARZ3NDEKTSV4RRFFQ69G5FAV');
	});

	it('gives a not-playable card the View profile affordance pointing at the character id', () => {
		const { target, text } = render(props({ status: 'retired', playable: false }));
		const link = target.querySelector<HTMLAnchorElement>('[data-testid="view-profile"]');
		expect(link?.getAttribute('href')).toBe('/c/01ARZ3NDEKTSV4RRFFQ69G5FAV');
		expect(text).toContain('View profile');
		expect(target.querySelector('[data-testid="roster-card"]')?.getAttribute('role')).toBeNull();
	});

	it('draws the portrait with the one shared tinted component, not a locally styled plate', () => {
		const { html } = render(props({}));
		expect(html).toMatch(/class="[^"]*\bportrait\b/);
		expect(html).toContain('aria-hidden="true"');
		expect(html).not.toContain('bg-primary');
	});

	it('keeps the Make default label unchanged in flight and marks it busy', () => {
		const { target } = render(props({ playable: true, isDefault: false, savingDefault: true }));
		const button = target.querySelector<HTMLButtonElement>('[data-testid="make-default"]');
		expect(button?.textContent?.trim()).toBe('Make default');
		expect(button?.getAttribute('aria-busy')).toBe('true');
		expect(button?.disabled).toBe(true);
	});

	it('gives EVERY owned card the owner-edit route into the authoring surface', () => {
		// The reachability property, asserted at both lifecycle states because a
		// retired character's twelve profile fields are every bit as editable as
		// an active one's. Before this control the only link to /characters/[id]
		// was the roster's one-shot repair notice, gated on a partial-write
		// failure — so a clean create left the surface unreachable.
		for (const p of [
			props({ status: 'active', playable: true }),
			props({ status: 'retired', playable: false }),
		]) {
			const { target, text, dispose } = render(p);
			const link = target.querySelector<HTMLAnchorElement>('[data-testid="edit-character"]');
			expect(link, `expected an edit link on a ${p.status} card`).not.toBeNull();
			expect(link?.getAttribute('href')).toBe('/characters/01ARZ3NDEKTSV4RRFFQ69G5FAV');
			// The accessible name IS the visible text — no aria-label override to
			// drift out of sync — and it names a verb the card's other controls do
			// not use.
			expect(link?.textContent?.trim()).toBe('Edit profile →');
			expect(text).toContain('Edit profile');
			dispose();
		}
	});

	it('does not also select the character when the edit link is followed on a playable card', () => {
		const onselect = vi.fn();
		const { target } = render(props({ playable: true, onselect }));
		const link = target.querySelector<HTMLAnchorElement>('[data-testid="edit-character"]');
		// jsdom does not navigate, so the click is observed purely for the
		// side-effect that must NOT happen: the card's whole-surface select.
		link?.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
		expect(onselect).not.toHaveBeenCalled();
	});

	it('leaves Enter on a control inside the card to that control, rather than selecting', () => {
		// The card's own arm calls preventDefault(), so a keydown bubbling up from
		// a focused child would suppress that child's activation and enter the
		// game instead. Mouse-reachable-only would make the edit route no route
		// at all for a keyboard player.
		const onselect = vi.fn();
		const { target } = render(props({ playable: true, isDefault: false, onselect }));
		for (const testid of ['edit-character', 'make-default']) {
			const control = target.querySelector<HTMLElement>(`[data-testid="${testid}"]`);
			expect(control, `expected a ${testid} control`).not.toBeNull();
			const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
			control?.dispatchEvent(event);
			expect(onselect, `Enter on ${testid} must not select`).not.toHaveBeenCalled();
			expect(event.defaultPrevented, `Enter on ${testid} must stay actionable`).toBe(false);
		}

		// The card itself still answers for its OWN focus.
		const card = target.querySelector<HTMLElement>('[data-testid="roster-card"]');
		card?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
		expect(onselect).toHaveBeenCalledWith('01ARZ3NDEKTSV4RRFFQ69G5FAV');
	});

	it('reports the character id when Make default is activated, without also selecting the character', () => {
		const onselect = vi.fn();
		const onmakedefault = vi.fn();
		const { target } = render(props({ playable: true, isDefault: false, onselect, onmakedefault }));
		target.querySelector<HTMLButtonElement>('[data-testid="make-default"]')?.click();
		expect(onmakedefault).toHaveBeenCalledWith('01ARZ3NDEKTSV4RRFFQ69G5FAV');
		expect(onselect).not.toHaveBeenCalled();
	});
});
