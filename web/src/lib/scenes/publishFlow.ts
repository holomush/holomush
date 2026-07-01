// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ensureSession } from './altSessions.svelte';
import { startScenePublish, castPublishSceneVote, withdrawScenePublish } from './client';
import { publishStore } from './publishStore.svelte';

type StartArgs = { sceneId: string; characterId: string };
type VoteArgs = { characterId: string; vote: boolean };
type WithdrawArgs = { characterId: string };

/**
 * Starts a publication vote (structural write → typed RPC). No store mutation:
 * the scene_publish_started event drives reloadPointer → the panel appears.
 * Takes the uniform { sceneId, characterId } shape so the rail's runLifecycle
 * wrapper can call it.
 */
export async function startPublishAction({ sceneId, characterId }: StartArgs): Promise<void> {
	const sessionId = await ensureSession(characterId);
	await startScenePublish(sessionId, { characterId, sceneId });
}

/**
 * Casts or changes the caller's Yes(true)/No(false) vote on the active attempt.
 * Optimistically marks the button dark (pending) before the RPC; reverts to the
 * previous confirmed vote on failure; on success acks → promotes it to confirmed
 * (brighten, publishStore §5). No-op if no attempt is active or a cast is already
 * in flight (serialize). The lock (_markVotePending → castInFlight) is raised
 * SYNCHRONOUSLY before any await, so a second click during session setup bails.
 */
export async function castVoteAction({ characterId, vote }: VoteArgs): Promise<void> {
	const publishedSceneId = publishStore.activeAttemptId;
	if (!publishedSceneId) return;
	if (publishStore.castInFlight) return;
	publishStore._markVotePending(vote); // raise the lock before any await
	try {
		const sessionId = await ensureSession(characterId);
		await castPublishSceneVote(sessionId, { characterId, publishedSceneId, vote });
	} catch (e) {
		publishStore._clearVote();
		throw e;
	}
	publishStore._ackVote();
}

/**
 * Withdraws (cancels) the active attempt the caller owns. No store mutation:
 * the scene_publish_withdrawn event drives reloadPointer → the panel clears.
 * Silent no-op if no attempt is active.
 */
export async function withdrawAction({ characterId }: WithdrawArgs): Promise<void> {
	const publishedSceneId = publishStore.activeAttemptId;
	if (!publishedSceneId) return;
	const sessionId = await ensureSession(characterId);
	await withdrawScenePublish(sessionId, { characterId, publishedSceneId });
}
