// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'sess-1') }));
vi.mock('./client', () => ({
	startScenePublish: vi.fn(async () => ({ id: 'att-1' })),
	castPublishSceneVote: vi.fn(async () => ({})),
	withdrawScenePublish: vi.fn(async () => ({})),
}));
// hoisted to avoid TDZ with vi.mock hoisting (see workspaceStore.test.ts)
const mockStore = vi.hoisted(() => ({
	activeAttemptId: 'att-1',
	castInFlight: false,
	_markVotePending: vi.fn(),
	_ackVote: vi.fn(),
	_clearVote: vi.fn(),
}));
vi.mock('./publishStore.svelte', () => ({ publishStore: mockStore }));

import { startPublishAction, castVoteAction, withdrawAction } from './publishFlow';
import { ensureSession } from './altSessions.svelte';
import { startScenePublish, castPublishSceneVote, withdrawScenePublish } from './client';

beforeEach(() => {
	vi.clearAllMocks();
	mockStore.activeAttemptId = 'att-1';
	mockStore.castInFlight = false;
});

describe('startPublishAction', () => {
	it('ensures the alt session and starts the publish vote', async () => {
		await startPublishAction({ sceneId: 'scene-1', characterId: 'char-1' });
		expect(ensureSession).toHaveBeenCalledWith('char-1');
		expect(startScenePublish).toHaveBeenCalledWith('sess-1', { characterId: 'char-1', sceneId: 'scene-1' });
	});
});

describe('castVoteAction', () => {
	it('marks pending, casts the vote, then acks on success', async () => {
		await castVoteAction({ characterId: 'char-1', vote: true });
		expect(mockStore._markVotePending).toHaveBeenCalledWith(true);
		expect(castPublishSceneVote).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1', publishedSceneId: 'att-1', vote: true,
		});
		expect(mockStore._ackVote).toHaveBeenCalledTimes(1);
		expect(mockStore._clearVote).not.toHaveBeenCalled();
	});

	it('reverts (clearVote) and rethrows when the RPC rejects; does not ack', async () => {
		vi.mocked(castPublishSceneVote).mockRejectedValueOnce(new Error('failed_precondition'));
		await expect(castVoteAction({ characterId: 'char-1', vote: false })).rejects.toThrow('failed_precondition');
		expect(mockStore._clearVote).toHaveBeenCalledTimes(1);
		expect(mockStore._ackVote).not.toHaveBeenCalled();
	});

	it('is a silent no-op when there is no active attempt', async () => {
		mockStore.activeAttemptId = '';
		await castVoteAction({ characterId: 'char-1', vote: true });
		expect(ensureSession).not.toHaveBeenCalled();
		expect(castPublishSceneVote).not.toHaveBeenCalled();
		expect(mockStore._markVotePending).not.toHaveBeenCalled();
	});

	it('is a silent no-op when a cast is already in flight (serialize)', async () => {
		mockStore.castInFlight = true;
		await castVoteAction({ characterId: 'char-1', vote: true });
		expect(castPublishSceneVote).not.toHaveBeenCalled();
		expect(mockStore._markVotePending).not.toHaveBeenCalled();
	});
});

describe('withdrawAction', () => {
	it('ensures the alt session and withdraws the active attempt', async () => {
		await withdrawAction({ characterId: 'char-1' });
		expect(withdrawScenePublish).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1', publishedSceneId: 'att-1',
		});
	});

	it('is a silent no-op when there is no active attempt', async () => {
		mockStore.activeAttemptId = '';
		await withdrawAction({ characterId: 'char-1' });
		expect(ensureSession).not.toHaveBeenCalled();
		expect(withdrawScenePublish).not.toHaveBeenCalled();
	});
});
