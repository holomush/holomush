// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'sess-1') }));
vi.mock('./client', () => ({
	getScene: vi.fn(async () => ({
		title: 'Tavern', description: 'desc', visibility: 'open',
		poseOrderMode: 'free', tags: ['social'], contentWarnings: [],
	})),
	updateScene: vi.fn(async () => ({
		id: 'scene-1', title: 'Tavern', description: 'desc', visibility: 'private',
		poseOrderMode: 'free', tags: ['social'], contentWarnings: [],
	})),
}));
vi.mock('./workspaceStore.svelte', () => ({ workspaceStore: { applySceneInfo: vi.fn() } }));

import { loadSceneSettings, settingsMask, saveSceneSettings } from './settingsFlow';
import { getScene, updateScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

const base = {
	title: 'Tavern', description: 'desc', visibility: 'open',
	poseOrderMode: 'free', tags: ['social'], contentWarnings: [] as string[],
};

describe('settingsMask', () => {
	it('returns an empty mask when nothing changed', () => {
		expect(settingsMask(base, { ...base })).toEqual([]);
	});
	it('includes only changed snake_case paths', () => {
		expect(settingsMask(base, { ...base, visibility: 'private', tags: ['social', 'plot'] }))
			.toEqual(['visibility', 'tags']);
	});
	it('treats pose-order mode as pose_order_mode', () => {
		expect(settingsMask(base, { ...base, poseOrderMode: 'strict' })).toEqual(['pose_order_mode']);
	});
});

describe('loadSceneSettings', () => {
	beforeEach(() => vi.clearAllMocks());
	it('ensures the alt session and maps SceneInfo to the baseline', async () => {
		const got = await loadSceneSettings('char-1', 'scene-1');
		expect(getScene).toHaveBeenCalledWith('sess-1', 'char-1', 'scene-1');
		expect(got.visibility).toBe('open');
		expect(got.tags).toEqual(['social']);
	});
});

describe('saveSceneSettings', () => {
	beforeEach(() => vi.clearAllMocks());
	it('no-ops without an RPC when the diff is empty', async () => {
		const wrote = await saveSceneSettings({ characterId: 'char-1', sceneId: 'scene-1', orig: base, next: { ...base } });
		expect(wrote).toBe(false);
		expect(updateScene).not.toHaveBeenCalled();
	});
	it('sends the changed fields + mask and merges the response', async () => {
		const wrote = await saveSceneSettings({
			characterId: 'char-1', sceneId: 'scene-1', orig: base, next: { ...base, visibility: 'private' },
		});
		expect(wrote).toBe(true);
		expect(updateScene).toHaveBeenCalledWith('sess-1', expect.objectContaining({
			characterId: 'char-1', sceneId: 'scene-1', visibility: 'private',
			updateMask: { paths: ['visibility'] },
		}));
		expect(workspaceStore.applySceneInfo).toHaveBeenCalledWith(expect.objectContaining({
			id: 'scene-1', visibility: 'private', title: 'Tavern',
		}));
	});
});
