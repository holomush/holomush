// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount, unmount, flushSync, tick } from 'svelte';
import SceneSettingsForm from './SceneSettingsForm.svelte';

vi.mock('$lib/scenes/settingsFlow', () => ({
	loadSceneSettings: vi.fn(async () => ({
		title: 'Tavern', description: 'A bar', visibility: 'open',
		poseOrderMode: 'free', tags: ['social'], contentWarnings: [],
	})),
	settingsMask: vi.fn(() => []),
	saveSceneSettings: vi.fn(async () => true),
}));

describe('SceneSettingsForm', () => {
	let host: HTMLElement;
	beforeEach(() => {
		vi.clearAllMocks();
		host = document.createElement('div');
		document.body.appendChild(host);
	});

	it('loads the baseline and pre-populates the title once mounted', async () => {
		const comp = mount(SceneSettingsForm, {
			target: host,
			props: { sceneId: 'scene-1', characterId: 'char-1', onDone: () => {} },
		});
		flushSync();
		await tick();
		await tick();
		const title = host.querySelector<HTMLInputElement>('input[name="settings-title"]');
		expect(title?.value).toBe('Tavern');
		const save = host.querySelector<HTMLButtonElement>('button[type="submit"]');
		// No diff yet → Save disabled.
		expect(save?.disabled).toBe(true);
		unmount(comp);
	});
});
