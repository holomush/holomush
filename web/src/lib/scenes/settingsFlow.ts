// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ensureSession } from './altSessions.svelte';
import { getScene, updateScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

/** The owner-editable subset of a scene's settings (mirrors the form fields). */
export type SceneSettings = {
	title: string;
	description: string;
	visibility: string;
	poseOrderMode: string;
	tags: string[];
	contentWarnings: string[];
};

/** Loads the current settings baseline for the sheet from the authoritative read RPC. */
export async function loadSceneSettings(characterId: string, sceneId: string): Promise<SceneSettings> {
	const sessionId = await ensureSession(characterId);
	const s = await getScene(sessionId, characterId, sceneId);
	return {
		title: s?.title ?? '',
		description: s?.description ?? '',
		visibility: s?.visibility ?? '',
		poseOrderMode: s?.poseOrderMode ?? '',
		tags: s?.tags ?? [],
		contentWarnings: s?.contentWarnings ?? [],
	};
}

function sameList(a: string[], b: string[]): boolean {
	return a.length === b.length && a.every((v, i) => v === b[i]);
}

/** Computes the changed-fields update_mask (snake_case proto paths). */
export function settingsMask(orig: SceneSettings, next: SceneSettings): string[] {
	const paths: string[] = [];
	if (next.title !== orig.title) paths.push('title');
	if (next.description !== orig.description) paths.push('description');
	if (next.visibility !== orig.visibility) paths.push('visibility');
	if (next.poseOrderMode !== orig.poseOrderMode) paths.push('pose_order_mode');
	if (!sameList(next.tags, orig.tags)) paths.push('tags');
	if (!sameList(next.contentWarnings, orig.contentWarnings)) paths.push('content_warnings');
	return paths;
}

/**
 * Saves changed settings via the typed RPC. Returns false (no RPC) when nothing
 * changed; otherwise issues UpdateScene with the diff mask and merges the
 * returned SceneInfo into the workspace cache.
 */
export async function saveSceneSettings(args: {
	characterId: string;
	sceneId: string;
	orig: SceneSettings;
	next: SceneSettings;
}): Promise<boolean> {
	const paths = settingsMask(args.orig, args.next);
	if (paths.length === 0) return false;
	const sessionId = await ensureSession(args.characterId);
	const scene = await updateScene(sessionId, {
		characterId: args.characterId,
		sceneId: args.sceneId,
		title: args.next.title,
		description: args.next.description,
		visibility: args.next.visibility,
		poseOrderMode: args.next.poseOrderMode,
		tags: args.next.tags,
		contentWarnings: args.next.contentWarnings,
		updateMask: { paths },
	});
	if (scene) workspaceStore.applySceneInfo(scene);
	return true;
}
