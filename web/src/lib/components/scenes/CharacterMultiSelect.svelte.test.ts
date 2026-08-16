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

	it('loads the viewer-scoped directory on mount, passing no acting alt', async () => {
		const onChange = vi.fn();
		const target = document.createElement('div');
		document.body.appendChild(target);
		const comp = mount(CharacterMultiSelect, {
			target,
			props: { selected: [], onChange },
		});
		flushSync(); // force $effect to run so the on-mount fetch fires deterministically
		const { listAllCharacters } = await import('$lib/scenes/directoryClient');
		// The listing is scoped by the session cookie, never by a client-supplied
		// character id — asserting the empty argument list is what pins that.
		expect(listAllCharacters).toHaveBeenCalledWith();
		unmount(comp);
		target.remove();
	});
});
