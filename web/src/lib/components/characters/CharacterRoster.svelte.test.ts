// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/*
 * THE SECTION RULES, ASSERTED ON MARKUP RATHER THAN ON STYLE.
 *
 * Two of the rules below are only meaningfully testable as markup facts:
 *
 *   1. The `Not playable` section is OMITTED ENTIRELY at zero — a heading with
 *      no body is a count of one, and a class applied to an empty section is
 *      still a heading a screen reader reads out.
 *   2. Collapsing REMOVES the grid, rather than hiding it. A `display: none`
 *      grid is still in the DOM and, depending on how it is hidden, still in
 *      the accessibility tree; the chip would then claim `aria-expanded=false`
 *      over content that is still announced.
 *
 * So the specs read `innerHTML` and `querySelector`, never a class list.
 */

import { afterEach, describe, expect, it } from 'vitest';
import { mount, tick, unmount } from 'svelte';
import CharacterRoster from './CharacterRoster.svelte';

type Row = {
	id: string;
	name: string;
	status: string;
	session?: { hasActiveSession: boolean; sessionStatus: string };
};

function row(p: Partial<Row>): Row {
	return { id: 'c1', name: 'Bazian', status: 'active', ...p };
}

function render(characters: Row[], defaultCharacterId = '') {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(CharacterRoster, { target, props: { characters, defaultCharacterId } });
	return {
		target,
		get text() {
			return (target.textContent ?? '').replace(/\s+/g, ' ').trim();
		},
		get html() {
			return target.innerHTML;
		},
		dispose: () => {
			unmount(component);
			target.remove();
		},
	};
}

afterEach(() => document.body.replaceChildren());

describe('CharacterRoster', () => {
	it('renders the authored empty copy and the create card, and no Not playable section, at zero characters', () => {
		const r = render([]);
		expect(r.text).toContain('No characters yet');
		expect(r.text).toContain('Create one to step into the world.');
		expect(r.target.querySelector('[data-testid="create-character"]')).not.toBeNull();
		expect(r.html).not.toContain('Not playable');
	});

	it('says in words that nothing is playable when every character is not playable', () => {
		const r = render([row({ id: 'c1', name: 'Bazian', status: 'retired' })]);
		expect(r.text).toContain('Nothing playable right now.');
		expect(r.text).toContain('Create a character to get back in.');
		expect(r.target.querySelector('[data-testid="create-character"]')).not.toBeNull();
		// The not-playable card still renders, in its own section.
		expect(r.target.querySelector('[data-testid="view-profile"]')).not.toBeNull();
	});

	it('puts the Playable section with the create card first and the Not playable section second', () => {
		const r = render([row({ id: 'c1', name: 'Bazian' }), row({ id: 'c2', name: 'Wren', status: 'retired' })]);
		const playableAt = r.html.indexOf('Playable');
		const notPlayableAt = r.html.indexOf('Not playable');
		expect(playableAt).toBeGreaterThanOrEqual(0);
		expect(notPlayableAt).toBeGreaterThan(playableAt);
		// The create card belongs to the top grid, ahead of the second section.
		expect(r.html.indexOf('data-testid="create-character"')).toBeLessThan(notPlayableAt);
	});

	it('omits the Not playable section entirely — heading and chip both — when nothing is not playable', () => {
		const r = render([row({ id: 'c1' }), row({ id: 'c2', name: 'Wren' })]);
		expect(r.html).not.toContain('Not playable');
		expect(r.target.querySelector('[data-testid="not-playable-toggle"]')).toBeNull();
	});

	it('renders the Not playable section EXPANDED on first paint', () => {
		const r = render([row({ id: 'c1' }), row({ id: 'c2', name: 'Wren', status: 'retired' })]);
		const chip = r.target.querySelector<HTMLButtonElement>('[data-testid="not-playable-toggle"]');
		expect(chip?.getAttribute('aria-expanded')).toBe('true');
		expect(r.target.querySelector('[data-testid="not-playable-grid"]')).not.toBeNull();
	});

	it('removes the not-playable grid from the markup when the chip is activated', async () => {
		const r = render([row({ id: 'c1' }), row({ id: 'c2', name: 'Wren', status: 'retired' })]);
		const chip = r.target.querySelector<HTMLButtonElement>('[data-testid="not-playable-toggle"]');
		chip?.click();
		await tick();
		expect(r.target.querySelector('[data-testid="not-playable-grid"]')).toBeNull();
		expect(
			r.target.querySelector('[data-testid="not-playable-toggle"]')?.getAttribute('aria-expanded'),
		).toBe('false');
	});

	it('points the chip aria-controls at the not-playable grid it governs', () => {
		const r = render([row({ id: 'c1' }), row({ id: 'c2', name: 'Wren', status: 'retired' })]);
		const chip = r.target.querySelector<HTMLButtonElement>('[data-testid="not-playable-toggle"]');
		const grid = r.target.querySelector<HTMLElement>('[data-testid="not-playable-grid"]');
		expect(grid?.id).toBeTruthy();
		expect(chip?.getAttribute('aria-controls')).toBe(grid?.id);
	});

	it('reads the chip label in the singular for one not-playable character', () => {
		const r = render([row({ id: 'c1' }), row({ id: 'c2', name: 'Wren', status: 'retired' })]);
		const label = r.target.querySelector('[data-testid="not-playable-toggle"]')?.textContent?.trim();
		expect(label).toBe('Hide 1 character');
	});

	it('reads the chip label in the plural for two or more not-playable characters', () => {
		const r = render([
			row({ id: 'c1' }),
			row({ id: 'c2', name: 'Wren', status: 'retired' }),
			row({ id: 'c3', name: 'Mera', status: 'idle' }),
		]);
		const label = r.target.querySelector('[data-testid="not-playable-toggle"]')?.textContent?.trim();
		expect(label).toBe('Hide 2 characters');
	});

	it('renders one playable character plus the create card with no section-chrome change', () => {
		const r = render([row({ id: 'c1', name: 'Bazian' })]);
		expect(r.target.querySelectorAll('[data-testid="roster-card"]').length).toBe(1);
		expect(r.target.querySelector('[data-testid="create-character"]')).not.toBeNull();
		expect(r.text).toContain('Playable');
		expect(r.html).not.toContain('Not playable');
		expect(r.text).not.toContain('Nothing playable right now.');
	});

	it('marks exactly the default character, and only when it is playable', () => {
		const r = render([row({ id: 'c1', name: 'Bazian' }), row({ id: 'c2', name: 'Wren' })], 'c2');
		const badges = r.target.querySelectorAll('[data-testid="default-badge"]');
		expect(badges.length).toBe(1);
		expect(badges[0].closest('[data-testid="roster-card"]')?.textContent).toContain('Wren');
	});

	it('links the create card to the creation route and never claims a name is permanent', () => {
		const r = render([]);
		const link = r.target.querySelector<HTMLAnchorElement>('[data-testid="create-character"]');
		expect(link?.getAttribute('href')).toBe('/characters/new');
		expect(r.text).toContain('Create a character');
		expect(r.text).toContain('Names are reserved once taken.');
		expect(r.text).not.toMatch(/permanent/i);
	});
});
