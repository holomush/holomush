// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import {
	WebService,
	type WebListScenesRequest,
	type WebListPublishedScenesRequest,
	type WebWatchSceneRequest,
	type WebExportSceneRequest,
} from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';

/** Singleton Connect client for the web client scenes layer. */
export const client = createClient(WebService, transport);

/**
 * Lists the scenes the given character participates in (owned, member,
 * observer). characterId identifies which alt to query; sessionId is the
 * per-alt comms_hub session from ensureSession().
 * Returns the raw CharacterSceneInfo array from the response.
 */
export async function listMyScenes(sessionId: string, characterId: string) {
	const res = await client.webListMyScenes({ sessionId, characterId });
	return res.scenes;
}

/**
 * Fetches full scene metadata for one scene on behalf of the given character.
 * Returns the SceneInfo (participants/observers populated once .8.25 lands).
 * sessionId is the per-alt comms_hub session from ensureSession().
 */
export async function getScene(sessionId: string, characterId: string, sceneId: string) {
	const res = await client.webGetScene({ sessionId, characterId, sceneId });
	return res.scene;
}

/**
 * Lists scenes visible to the authenticated player — the board view.
 * Pass characterId to narrow to scenes for a specific alt.
 */
export async function listScenes(
	sessionId: string,
	opts: Partial<Pick<WebListScenesRequest, 'characterId' | 'limit' | 'offset' | 'tags'>> = {},
) {
	const res = await client.webListScenes({
		sessionId,
		characterId: opts.characterId ?? '',
		limit: opts.limit ?? 0,
		offset: opts.offset ?? 0,
		tags: opts.tags ?? [],
	});
	return res.scenes;
}

/**
 * Adds the character as an observer on the given scene.
 * Returns the resulting ParticipantInfo so callers can update role state.
 */
export async function watchScene(
	sessionId: string,
	opts: Pick<WebWatchSceneRequest, 'characterId' | 'sceneId'>,
) {
	const res = await client.webWatchScene({ sessionId, ...opts });
	return res.participant;
}

/**
 * Requests a scene export (JSONL or Markdown) for participants.
 * Returns content bytes, MIME type, and filename from the response.
 */
export async function exportScene(
	sessionId: string,
	opts: Pick<WebExportSceneRequest, 'characterId' | 'sceneId' | 'format'>,
) {
	return client.webExportScene({ sessionId, ...opts });
}

/**
 * Sets or clears the server-side scene focus for a connection.
 * Routing: the server delivers SCENE_ACTIVITY control frames only for
 * scenes that are NOT currently focused. Pass sceneId='' to clear focus.
 *
 * MUST NOT be called before connectionId is captured from STREAM_OPENED.
 */
export async function setSceneFocus(
	sessionId: string,
	connectionId: string,
	sceneId: string,
): Promise<void> {
	await client.webSetSceneFocus({ sessionId, connectionId, sceneId });
}

/**
 * Sends a raw command text on behalf of the given session.
 * Used by the composer to issue scene pose/say/ooc/<verb> commands.
 */
export async function sendSceneCommand(
	sessionId: string,
	connectionId: string,
	cmd: string,
): Promise<void> {
	await client.sendCommand({ sessionId, connectionId, text: cmd });
}

/**
 * Lists published (archived) scenes visible to any authenticated player.
 * Returns PublicSceneArchive summaries ordered newest-first.
 */
export async function listPublishedScenes(
	sessionId: string,
	opts: Partial<Pick<WebListPublishedScenesRequest, 'limit' | 'offset' | 'tags'>> = {},
) {
	const res = await client.webListPublishedScenes({
		sessionId,
		limit: opts.limit ?? 0,
		offset: opts.offset ?? 0,
		tags: opts.tags ?? [],
	});
	return res.archives;
}

/**
 * Fetches full public archive metadata for a published scene.
 * publishedSceneId is the publication-attempt ID (PublicSceneArchive.id).
 */
export async function getPublicSceneArchive(sessionId: string, publishedSceneId: string) {
	const res = await client.webGetPublicSceneArchive({ sessionId, publishedSceneId });
	return res;
}

/**
 * Downloads a published scene archive as rendered content bytes.
 * format is 'jsonl' or 'markdown'. Returns content bytes, MIME type, and
 * (for markdown) the content. The response carries only content + mime_type;
 * callers supply the filename.
 */
export async function downloadPublicSceneArchive(
	sessionId: string,
	publishedSceneId: string,
	format: string,
) {
	return client.webDownloadPublicSceneArchive({ sessionId, publishedSceneId, format });
}

/**
 * Fetches a page of historical events for a scene IC stream.
 * notAfterMs should be set to attachMomentMs from REPLAY_COMPLETE to
 * avoid the connect-time replay/backfill race (holomush-iu8j).
 */
export async function queryStreamHistory(
	sessionId: string,
	stream: string,
	opts: { count?: number; notAfterMs?: bigint; notBeforeMs?: bigint } = {},
) {
	const res = await client.webQueryStreamHistory({
		sessionId,
		stream,
		count: opts.count ?? 0,
		notAfterMs: opts.notAfterMs ?? 0n,
		notBeforeMs: opts.notBeforeMs ?? 0n,
		cursor: new Uint8Array(),
	});
	return res;
}
