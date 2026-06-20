// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount } from 'svelte';
import CreateSceneForm from './CreateSceneForm.svelte';

vi.mock('$lib/scenes/createFlow', () => ({ submitCreateScene: vi.fn(async () => 'scene-new') }));

afterEach(() => document.body.replaceChildren());

function render(characters: { characterId: string; name?: string }[]) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const onDone = vi.fn();
	const component = mount(CreateSceneForm, { target, props: { characters, onDone } });
	return { target, onDone, component };
}

describe('CreateSceneForm', () => {
	it('disables Create until a title is entered', () => {
		const { target } = render([{ characterId: 'c1', name: 'Alice' }]);
		const create = target.querySelector<HTMLButtonElement>('button[aria-label="Create scene"]')!;
		expect(create.disabled).toBe(true);
	});

	it('hides the character selector with a single alt', () => {
		const { target } = render([{ characterId: 'c1', name: 'Alice' }]);
		expect(target.querySelector('select[aria-label="Create scene as"]')).toBeNull();
	});

	it('shows the character selector with multiple alts', () => {
		const { target } = render([
			{ characterId: 'c1', name: 'Alice' },
			{ characterId: 'c2', name: 'Bob' },
		]);
		expect(target.querySelector('select[aria-label="Create scene as"]')).not.toBeNull();
	});
});
