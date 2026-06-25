// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'sess-1') }));
vi.mock('./client', () => ({
	endScene: vi.fn(async () => ({ id: 'scene-1', state: 'ended' })),
	pauseScene: vi.fn(async () => ({ id: 'scene-1', state: 'paused' })),
	resumeScene: vi.fn(async () => ({ id: 'scene-1', state: 'active' })),
}));
vi.mock('./workspaceStore.svelte', () => ({
	workspaceStore: { applySceneInfo: vi.fn() },
}));

import { endSceneAction, pauseSceneAction, resumeSceneAction } from './lifecycleFlow';
import { ensureSession } from './altSessions.svelte';
import { endScene, pauseScene, resumeScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

describe('endSceneAction', () => {
	beforeEach(() => vi.clearAllMocks());

	it('ensures the alt session, ends the scene, and merges the returned scene', async () => {
		await endSceneAction({ sceneId: 'scene-1', characterId: 'char-1' });
		expect(ensureSession).toHaveBeenCalledWith('char-1');
		expect(endScene).toHaveBeenCalledWith('sess-1', { characterId: 'char-1', sceneId: 'scene-1' });
		expect(workspaceStore.applySceneInfo).toHaveBeenCalledWith({ id: 'scene-1', state: 'ended' });
	});

	it('propagates rejection and does not call applySceneInfo', async () => {
		vi.mocked(endScene).mockRejectedValueOnce(new Error('denied'));
		await expect(endSceneAction({ sceneId: 'scene-1', characterId: 'char-1' })).rejects.toThrow(
			'denied',
		);
		expect(workspaceStore.applySceneInfo).not.toHaveBeenCalled();
	});
});

describe('pauseSceneAction', () => {
	beforeEach(() => vi.clearAllMocks());

	it('ensures the alt session, pauses the scene, and merges the returned scene', async () => {
		await pauseSceneAction({ sceneId: 'scene-1', characterId: 'char-1' });
		expect(ensureSession).toHaveBeenCalledWith('char-1');
		expect(pauseScene).toHaveBeenCalledWith('sess-1', { characterId: 'char-1', sceneId: 'scene-1' });
		expect(workspaceStore.applySceneInfo).toHaveBeenCalledWith({ id: 'scene-1', state: 'paused' });
	});
});

describe('resumeSceneAction', () => {
	beforeEach(() => vi.clearAllMocks());

	it('ensures the alt session, resumes the scene, and merges the returned scene', async () => {
		await resumeSceneAction({ sceneId: 'scene-1', characterId: 'char-1' });
		expect(ensureSession).toHaveBeenCalledWith('char-1');
		expect(resumeScene).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1',
			sceneId: 'scene-1',
		});
		expect(workspaceStore.applySceneInfo).toHaveBeenCalledWith({ id: 'scene-1', state: 'active' });
	});
});
