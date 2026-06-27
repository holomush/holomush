// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'sess-1') }));
vi.mock('./client', () => ({
	inviteToScene: vi.fn(async () => {}),
	kickFromScene: vi.fn(async () => {}),
	transferOwnership: vi.fn(async () => {}),
	leaveScene: vi.fn(async () => {}),
}));
vi.mock('./workspaceStore.svelte', () => ({ workspaceStore: { select: vi.fn() } }));

import { inviteCharacters, kickAction, transferAction, leaveAction } from './membershipFlow';
import { inviteToScene, kickFromScene, transferOwnership, leaveScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

describe('membershipFlow', () => {
	beforeEach(() => vi.clearAllMocks());

	it('invites each selected character then refetches the roster', async () => {
		await inviteCharacters({ sceneId: 'scene-1', characterId: 'char-1', targetIds: ['e1', 'e2'] });
		expect(inviteToScene).toHaveBeenCalledTimes(2);
		expect(inviteToScene).toHaveBeenNthCalledWith(1, 'sess-1', {
			characterId: 'char-1',
			sceneId: 'scene-1',
			targetCharacterId: 'e1',
		});
		expect(inviteToScene).toHaveBeenNthCalledWith(2, 'sess-1', {
			characterId: 'char-1',
			sceneId: 'scene-1',
			targetCharacterId: 'e2',
		});
		expect(workspaceStore.select).toHaveBeenCalledWith('scene-1', '', 'char-1');
	});

	it('aborts on the first invite failure and skips the refetch', async () => {
		vi.mocked(inviteToScene).mockRejectedValueOnce(new Error('invite failed'));
		await expect(
			inviteCharacters({ sceneId: 'scene-1', characterId: 'char-1', targetIds: ['e1', 'e2'] }),
		).rejects.toThrow('invite failed');
		// First error aborts: the second invite is never sent and the roster is not refetched.
		expect(inviteToScene).toHaveBeenCalledTimes(1);
		expect(workspaceStore.select).not.toHaveBeenCalled();
	});

	it('kicks then refetches', async () => {
		await kickAction({ sceneId: 'scene-1', characterId: 'char-1', targetCharacterId: 'e1' });
		expect(kickFromScene).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1',
			sceneId: 'scene-1',
			targetCharacterId: 'e1',
		});
		expect(workspaceStore.select).toHaveBeenCalled();
	});

	it('transfers ownership then refetches', async () => {
		await transferAction({
			sceneId: 'scene-1',
			characterId: 'char-1',
			newOwnerCharacterId: 'e1',
		});
		expect(transferOwnership).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1',
			sceneId: 'scene-1',
			newOwnerCharacterId: 'e1',
		});
		expect(workspaceStore.select).toHaveBeenCalled();
	});

	it('leaves then refetches', async () => {
		await leaveAction({ sceneId: 'scene-1', characterId: 'char-1' });
		expect(leaveScene).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1',
			sceneId: 'scene-1',
		});
		expect(workspaceStore.select).toHaveBeenCalled();
	});
});
