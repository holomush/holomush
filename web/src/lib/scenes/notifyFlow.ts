// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ensureSession } from './altSessions.svelte';
import { muteScene, setSceneNotifyPref } from './client';
import { workspaceStore } from './workspaceStore.svelte';

type Base = { sceneId: string; characterId: string };

/**
 * Toggles the character's per-scene notification mute via the typed WebMuteScene
 * RPC (never the command path — gateway-boundary), then reflects the new state
 * locally so the toggle updates immediately. Mirrors membershipFlow's
 * toggle-on-an-existing-resource shape (RESEARCH Pattern 3), not createFlow.
 *
 * On RPC failure the local store is left unchanged and the error propagates so
 * the UI can surface it; the persisted state is re-read authoritatively on the
 * next workspace refresh (round-3 Concern 1 read-back).
 */
export async function toggleSceneMute({
	sceneId,
	characterId,
	muted,
}: Base & { muted: boolean }): Promise<void> {
	const sessionId = await ensureSession(characterId);
	await muteScene(sessionId, { characterId, sceneId, muted });
	workspaceStore.setMuted(sceneId, characterId, muted);
}

/**
 * Writes the character's global scene-notify preference via the typed
 * WebSetSceneNotifyPref RPC (character-self scope, no scene), then reflects the
 * new state locally. Same failure semantics as toggleSceneMute.
 */
export async function setGlobalNotify({
	characterId,
	enabled,
}: {
	characterId: string;
	enabled: boolean;
}): Promise<void> {
	const sessionId = await ensureSession(characterId);
	await setSceneNotifyPref(sessionId, { characterId, enabled });
	workspaceStore.setGlobalNotifyEnabled(enabled);
}
