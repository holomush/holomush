// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import CharacterMultiSelect from './CharacterMultiSelect.svelte';

vi.mock('$lib/scenes/directoryClient', () => ({
	listAllCharacters: vi.fn(async () => [
		{ id: 'c1', name: 'Alice' },
		{ id: 'c2', name: 'Bob' },
	]),
}));

describe('CharacterMultiSelect', () => {
	beforeEach(() => vi.clearAllMocks());

	it('loads the directory for the acting alt on mount', async () => {
		const onChange = vi.fn();
		const target = document.createElement('div');
		document.body.appendChild(target);
		const comp = mount(CharacterMultiSelect, {
			target,
			props: { characterId: 'char-me', selected: [], onChange },
		});
		flushSync(); // force $effect to run so the on-mount fetch fires deterministically
		const { listAllCharacters } = await import('$lib/scenes/directoryClient');
		expect(listAllCharacters).toHaveBeenCalledWith('char-me');
		unmount(comp);
		target.remove();
	});
});
