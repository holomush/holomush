// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ensureSession } from './altSessions.svelte';
import { inviteToScene, kickFromScene, transferOwnership, leaveScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

type Base = { sceneId: string; characterId: string };

async function refetch(sceneId: string, characterId: string): Promise<void> {
	await workspaceStore.select(sceneId, '', characterId);
}

/**
 * Invites every selected character (sequential), then refetches the roster.
 * Sequential keeps partial-failure semantics simple: the first error aborts and
 * surfaces; already-sent invites stand.
 */
export async function inviteCharacters({
	sceneId,
	characterId,
	targetIds,
}: Base & { targetIds: string[] }): Promise<void> {
	const sessionId = await ensureSession(characterId);
	let invitedAny = false;
	for (const targetCharacterId of targetIds) {
		try {
			await inviteToScene(sessionId, { characterId, sceneId, targetCharacterId });
			invitedAny = true;
		} catch (error) {
			// Partial successes stand (see docstring): refresh the roster so the
			// UI reflects invites that already took effect before re-surfacing.
			// A refetch failure here must not mask the original invite error.
			if (invitedAny) {
				try {
					await refetch(sceneId, characterId);
				} catch {
					// best-effort refresh; the original invite failure is authoritative
				}
			}
			throw error;
		}
	}
	await refetch(sceneId, characterId);
}

/**
 * Kicks the target character from the scene, then refetches the roster.
 */
export async function kickAction({
	sceneId,
	characterId,
	targetCharacterId,
}: Base & { targetCharacterId: string }): Promise<void> {
	const sessionId = await ensureSession(characterId);
	await kickFromScene(sessionId, { characterId, sceneId, targetCharacterId });
	await refetch(sceneId, characterId);
}

/**
 * Transfers scene ownership, then refetches the roster to surface the new owner.
 */
export async function transferAction({
	sceneId,
	characterId,
	newOwnerCharacterId,
}: Base & { newOwnerCharacterId: string }): Promise<void> {
	const sessionId = await ensureSession(characterId);
	await transferOwnership(sessionId, { characterId, sceneId, newOwnerCharacterId });
	await refetch(sceneId, characterId);
}

/**
 * Leaves the scene on behalf of characterId, then refetches to update local state.
 */
export async function leaveAction({ sceneId, characterId }: Base): Promise<void> {
	const sessionId = await ensureSession(characterId);
	await leaveScene(sessionId, { characterId, sceneId });
	await refetch(sceneId, characterId);
}
