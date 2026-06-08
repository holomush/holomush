// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * workspaceStore is the Svelte 5 runes state container for the scenes
 * workspace. It holds the scene list, per-scene log entries, unread badge
 * counts, and the currently selected sceneId.
 *
 * State mutations:
 *   refresh()      – snapshot ListMyScenes → seed myScenes + badge/activity
 *   select(id)     – ensure alt session + stream, wait for connectionId,
 *                    call setSceneFocus, clear unread, backfill gap
 *   ingestEvent()  – append LogEntry to the focused scene's log
 *   bumpUnread()   – increment badge (skip when sceneId === selectedSceneId)
 */

import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';
import { eventFrameToLogEntry, type LogEntry, type WorkspaceScene } from './types';
import {
	listMyScenes,
	setSceneFocus,
	queryStreamHistory,
} from './client';
import {
	ensureSession,
	awaitConnectionId,
	getAttachMomentMs,
} from './altSessions.svelte';

// ── runes state ──────────────────────────────────────────────────────────────

/** All scenes the character participates in (seeded by refresh()). */
let myScenes = $state<WorkspaceScene[]>([]);

/** Scenes the character is actively watching (observer role). */
let watching = $state<WorkspaceScene[]>([]);

/** Currently focused scene ID; null when none selected. */
let selectedSceneId = $state<string | null>(null);

/** Per-scene log entries keyed by sceneId. */
let logsBySceneId = $state<Record<string, LogEntry[]>>({});

/** Per-scene unread badge counts keyed by sceneId. */
let unreadBySceneId = $state<Record<string, number>>({});

// ── derived: character state for active alt sessions ─────────────────────────

/** characterId → sessionId cache populated by refresh() via ListMyScenes. */
const characterIdBySceneId = new Map<string, string>();

// ── actions ──────────────────────────────────────────────────────────────────

/**
 * Snapshots ListMyScenes and seeds myScenes/watching with badge and activity
 * data. Accepts the player-session id used for the request.
 * Called on workspace mount and after watch/join actions.
 */
async function refresh(sessionId: string): Promise<void> {
	const scenes = await listMyScenes(sessionId);

	const next: WorkspaceScene[] = scenes.map((csi) => {
		const si = csi.scene;
		const sceneId = si?.id ?? '';
		return {
			sceneId,
			title: si?.title ?? '',
			locationId: si?.locationId ?? '',
			state: si?.state ?? '',
			tags: si?.tags ?? [],
			role: csi.role,
			asCharacterId: '',
			asCharacterName: '',
			lastActivityMs: csi.lastActivityMs,
			entryCount: csi.entryCount,
			unread: unreadBySceneId[sceneId] ?? 0,
		};
	});

	myScenes = next;
	watching = next.filter((s) => s.role === 'observer');
}

/**
 * Selects a scene for display.
 *
 * Ordering invariant: setSceneFocus MUST NOT be called before connectionId
 * arrives from STREAM_OPENED. This is enforced by awaiting awaitConnectionId()
 * before calling setSceneFocus. The alt session opens concurrently but focus
 * is only set once the connectionId promise resolves.
 *
 * After setting focus:
 *   1. selectedSceneId is updated.
 *   2. unreadBySceneId[sceneId] is cleared.
 *   3. Backfill runs via webQueryStreamHistory on the scene .ic subject if
 *      the replay tail left a gap (notAfterMs = attachMomentMs from
 *      REPLAY_COMPLETE; 0 means no upper bound per spec).
 */
async function select(
	sceneId: string,
	/** The player's primary session ID (used for setSceneFocus and backfill). */
	playerSessionId: string,
	/** The character ID whose alt session should be used. */
	characterId: string,
): Promise<void> {
	// 1. Ensure the alt session exists and the stream is open.
	const altSessionId = await ensureSession(characterId);

	// 2. Wait for connectionId from STREAM_OPENED (invariant: must arrive before
	//    setSceneFocus). Uses the alt session's connectionIdPromise.
	const connectionId = await awaitConnectionId(characterId);

	// 3. Update selected scene and clear unread.
	selectedSceneId = sceneId;
	unreadBySceneId = { ...unreadBySceneId, [sceneId]: 0 };

	// 4. Notify server of the new focus so SCENE_ACTIVITY suppression fires.
	await setSceneFocus(altSessionId, connectionId, sceneId);

	// 5. Backfill: query IC stream history to fill any gap.
	//    notAfterMs = attachMomentMs to avoid the connect-time race (iu8j).
	const attachMomentMs = getAttachMomentMs(characterId);
	const icStream = `scene.${sceneId}.ic`;
	try {
		const history = await queryStreamHistory(playerSessionId, icStream, {
			notAfterMs: attachMomentMs,
		});
		const entries = history.events
			.map((ev) => eventFrameToLogEntry(ev))
			.filter((e): e is LogEntry => e !== null);

		if (entries.length > 0) {
			// Prepend historical entries (they are oldest-first from the server).
			const existing = logsBySceneId[sceneId] ?? [];
			const seenIds = new Set(existing.map((e) => e.id));
			const deduped = entries.filter((e) => !seenIds.has(e.id));
			logsBySceneId = {
				...logsBySceneId,
				[sceneId]: [...deduped, ...existing],
			};
		}
	} catch {
		// Non-fatal: backfill is best-effort.
	}
}

/**
 * Appends a LogEntry to the correct scene's log if the frame is a scene IC
 * event. Routes by the scene_id parsed from the event payload by
 * eventFrameToLogEntry, so an event for scene B lands in scene B's log
 * regardless of which alt session delivered it (multi-alt correctness).
 * Frames that are not scene events (movement, presence, etc.) are ignored.
 * Frames whose parsed scene_id is absent are dropped with a debug log.
 *
 * The sessionId parameter is kept for symmetry with the stream loop call site
 * but routing is now done via the parsed entry, not the session.
 */
function ingestEvent(_sessionId: string, ev: GameEvent): void {
	const entry = eventFrameToLogEntry(ev);
	if (!entry) return;

	// Route by scene_id parsed from the event metadata by eventFrameToLogEntry.
	// Prefer metadata over top-level fields (consistent with eventFrameToLogEntry).
	const meta = ev.metadata as Record<string, unknown> | undefined;
	const sceneId =
		typeof meta?.['scene_id'] === 'string' && meta['scene_id']
			? (meta['scene_id'] as string)
			: null;

	if (!sceneId) {
		console.debug('[workspaceStore] ingestEvent: dropping scene frame with no scene_id', {
			type: ev.type,
			eventId: ev.eventId,
		});
		return;
	}

	const existing = logsBySceneId[sceneId] ?? [];
	// Dedup by eventId.
	if (entry.id && existing.some((e) => e.id === entry.id)) return;

	logsBySceneId = {
		...logsBySceneId,
		[sceneId]: [...existing, entry],
	};
}

/**
 * Increments the unread badge for a scene.
 * Spec D7 dedup rule: skip when sceneId === selectedSceneId (the scene is
 * currently in focus so events are already visible to the user).
 */
function bumpUnread(sceneId: string): void {
	if (sceneId === selectedSceneId) return;
	unreadBySceneId = {
		...unreadBySceneId,
		[sceneId]: (unreadBySceneId[sceneId] ?? 0) + 1,
	};
}

/**
 * Exported store object following the Svelte 5 runes pattern.
 * Consumers import and destructure: `const { myScenes, select, ... } = workspaceStore`.
 */
export const workspaceStore = {
	get myScenes() {
		return myScenes;
	},
	get watching() {
		return watching;
	},
	get selectedSceneId() {
		return selectedSceneId;
	},
	get logsBySceneId() {
		return logsBySceneId;
	},
	get unreadBySceneId() {
		return unreadBySceneId;
	},
	refresh,
	select,
	ingestEvent,
	bumpUnread,
	/** Internal: characterIdBySceneId map (for alt session routing). */
	_characterIdBySceneId: characterIdBySceneId,
};

// Named exports for direct import in tests.
export { refresh, select, ingestEvent, bumpUnread };
