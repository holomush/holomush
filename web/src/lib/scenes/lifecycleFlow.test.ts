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

import { endSceneAction } from './lifecycleFlow';
import { ensureSession } from './altSessions.svelte';
import { endScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

describe('endSceneAction', () => {
	beforeEach(() => vi.clearAllMocks());

	it('ensures the alt session, ends the scene, and merges the returned scene', async () => {
		await endSceneAction({ sceneId: 'scene-1', characterId: 'char-1' });
		expect(ensureSession).toHaveBeenCalledWith('char-1');
		expect(endScene).toHaveBeenCalledWith('sess-1', { characterId: 'char-1', sceneId: 'scene-1' });
		expect(workspaceStore.applySceneInfo).toHaveBeenCalledWith({ id: 'scene-1', state: 'ended' });
	});
});
