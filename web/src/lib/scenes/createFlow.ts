// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ensureSession } from './altSessions.svelte';
import { createScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

/**
 * Orchestrates web scene creation: ensure the acting alt's session, create the
 * scene via the typed RPC, refresh My Scenes so the new (owner) scene appears,
 * then focus it. Returns the new scene id ('' when the server returned none).
 * The create RPC is authoritative; refresh/select are best-effort UI updates.
 */
export async function submitCreateScene(opts: {
	characterId: string;
	title: string;
	description: string;
	characters: { characterId: string; name?: string; characterName?: string }[];
}): Promise<string> {
	const sessionId = await ensureSession(opts.characterId);
	const scene = await createScene(sessionId, {
		characterId: opts.characterId,
		title: opts.title,
		description: opts.description,
	});
	const sceneId = scene?.id ?? '';
	// refresh/select are best-effort UI updates: the scene is already created and
	// authoritative, so a post-create failure here MUST NOT propagate as a create
	// failure — that would show "Create failed" to the user and risk a duplicate
	// scene on retry. Swallow and warn; the workspace reconciles on next interaction.
	try {
		await workspaceStore.refresh(opts.characters);
		if (sceneId) {
			await workspaceStore.select(sceneId, '', opts.characterId);
		}
	} catch (e) {
		console.warn('[submitCreateScene] post-create refresh/select failed', e);
	}
	return sceneId;
}
