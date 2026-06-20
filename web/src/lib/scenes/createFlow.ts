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
	await workspaceStore.refresh(opts.characters);
	if (sceneId) {
		await workspaceStore.select(sceneId, '', opts.characterId);
	}
	return sceneId;
}
