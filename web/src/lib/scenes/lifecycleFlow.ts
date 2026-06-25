// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ensureSession } from './altSessions.svelte';
import { endScene, pauseScene, resumeScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

type LifecycleArgs = { sceneId: string; characterId: string };

/**
 * Ensures the alt session, ends the scene via the typed RPC, and merges the
 * returned post-transition SceneInfo into the workspace store.
 */
export async function endSceneAction({ sceneId, characterId }: LifecycleArgs): Promise<void> {
	const sessionId = await ensureSession(characterId);
	const scene = await endScene(sessionId, { characterId, sceneId });
	if (scene) workspaceStore.applySceneInfo(scene);
}

/**
 * Ensures the alt session, pauses the scene via the typed RPC, and merges the
 * returned post-transition SceneInfo into the workspace store.
 */
export async function pauseSceneAction({ sceneId, characterId }: LifecycleArgs): Promise<void> {
	const sessionId = await ensureSession(characterId);
	const scene = await pauseScene(sessionId, { characterId, sceneId });
	if (scene) workspaceStore.applySceneInfo(scene);
}

/**
 * Ensures the alt session, resumes the scene via the typed RPC, and merges the
 * returned post-transition SceneInfo into the workspace store.
 */
export async function resumeSceneAction({ sceneId, characterId }: LifecycleArgs): Promise<void> {
	const sessionId = await ensureSession(characterId);
	const scene = await resumeScene(sessionId, { characterId, sceneId });
	if (scene) workspaceStore.applySceneInfo(scene);
}
