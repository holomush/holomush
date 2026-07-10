// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'sess-1') }));
vi.mock('./client', () => ({
	muteScene: vi.fn(async () => {}),
	setSceneNotifyPref: vi.fn(async () => {}),
}));
vi.mock('./workspaceStore.svelte', () => ({
	workspaceStore: { setMuted: vi.fn(), setGlobalNotifyEnabled: vi.fn() },
}));

import { toggleSceneMute, setGlobalNotify } from './notifyFlow';
import { muteScene, setSceneNotifyPref } from './client';
import { workspaceStore } from './workspaceStore.svelte';

describe('notifyFlow', () => {
	beforeEach(() => vi.clearAllMocks());

	it('mutes a scene via the typed WebMuteScene request then reflects it locally', async () => {
		await toggleSceneMute({ sceneId: 'scene-1', characterId: 'char-1', muted: true });
		expect(muteScene).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1',
			sceneId: 'scene-1',
			muted: true,
		});
		expect(workspaceStore.setMuted).toHaveBeenCalledWith('scene-1', 'char-1', true);
	});

	it('round-trips a mute toggle (unmute) with muted=false', async () => {
		await toggleSceneMute({ sceneId: 'scene-1', characterId: 'char-1', muted: false });
		expect(muteScene).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1',
			sceneId: 'scene-1',
			muted: false,
		});
		expect(workspaceStore.setMuted).toHaveBeenCalledWith('scene-1', 'char-1', false);
	});

	it('surfaces a denial as the flow error and does not touch local state', async () => {
		vi.mocked(muteScene).mockRejectedValueOnce(new Error('not a participant'));
		await expect(
			toggleSceneMute({ sceneId: 'scene-1', characterId: 'char-1', muted: true }),
		).rejects.toThrow('not a participant');
		expect(workspaceStore.setMuted).not.toHaveBeenCalled();
	});

	it('sets the global notify preference via the typed RPC then reflects it locally', async () => {
		await setGlobalNotify({ characterId: 'char-1', enabled: false });
		expect(setSceneNotifyPref).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1',
			enabled: false,
		});
		expect(workspaceStore.setGlobalNotifyEnabled).toHaveBeenCalledWith(false);
	});

	it('surfaces a notify-pref RPC failure and leaves local state untouched', async () => {
		vi.mocked(setSceneNotifyPref).mockRejectedValueOnce(new Error('boom'));
		await expect(setGlobalNotify({ characterId: 'char-1', enabled: true })).rejects.toThrow('boom');
		expect(workspaceStore.setGlobalNotifyEnabled).not.toHaveBeenCalled();
	});
});
