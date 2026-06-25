// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * workspaceStore is the Svelte 5 runes state container for the scenes
 * workspace. It holds the scene list, per-scene log entries, unread badge
 * counts, and the currently selected sceneId.
 *
 * State mutations:
 *   refresh(characters) – fan-out ListMyScenes across all owned alts →
 *                         seed myScenes + badge/activity. Each WorkspaceScene
 *                         is tagged with the asCharacterId/asCharacterName of
 *                         the alt that found it. One-alt failure is tolerated
 *                         (Promise.allSettled); partial results are merged.
 *   select(id)          – ensure alt session + stream, wait for connectionId,
 *                         call setSceneFocus, clear unread, backfill gap,
 *                         then best-effort enrich roster from getScene.
 *   ingestEvent()       – append LogEntry to the focused scene's log
 *   bumpUnread()        – increment badge (skip when sceneId === selectedSceneId)
 */

import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';
import type { SceneInfo } from '$lib/connect/holomush/scene/v1/scene_pb';
import { eventFrameToLogEntry, type LogEntry, type WorkspaceScene } from './types';
import {
	listMyScenes,
	getScene,
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

// ── actions ──────────────────────────────────────────────────────────────────

/**
 * Refreshes the workspace by fanning out ListMyScenes across all owned alts.
 *
 * For each character in `characters`:
 *   1. ensureSession(characterId) → per-alt comms_hub sessionId
 *   2. listMyScenes(sessionId, characterId) → CharacterSceneInfo[]
 *   3. Map each result to WorkspaceScene, tagging asCharacterId/asCharacterName
 *
 * All per-alt fetches run concurrently via Promise.allSettled — one failing
 * alt does not abort the others. Failed alts are logged and skipped.
 *
 * If the same sceneId appears for two alts, both rows are preserved (a player
 * participating via two alts → two workspace rows, one per alt).
 */
async function refresh(
	characters: { characterId: string; name?: string; characterName?: string }[],
): Promise<void> {
	const results = await Promise.allSettled(
		characters.map(async (char) => {
			const characterId = char.characterId;
			// Support both `name` (authStore.CharacterSummary) and `characterName`
			// (layout data CharacterSummary from webCheckSession).
			const characterName = char.characterName ?? char.name ?? characterId;
			const sessionId = await ensureSession(characterId);
			const scenes = await listMyScenes(sessionId, characterId);
			return { characterId, characterName, scenes };
		}),
	);

	const next: WorkspaceScene[] = [];
	for (const result of results) {
		if (result.status === 'rejected') {
			console.warn('[workspaceStore] refresh: alt fetch failed', result.reason);
			continue;
		}
		const { characterId, characterName, scenes } = result.value;
		for (const csi of scenes) {
			const si = csi.scene;
			const sceneId = si?.id ?? '';
			next.push({
				sceneId,
				title: si?.title ?? '',
				locationId: si?.locationId ?? '',
				state: si?.state ?? '',
				tags: si?.tags ?? [],
				role: csi.role,
				ownerId: si?.ownerId ?? '',
				asCharacterId: characterId,
				asCharacterName: characterName,
				lastActivityMs: csi.lastActivityMs,
				entryCount: csi.entryCount,
				unread: unreadBySceneId[sceneId] ?? 0,
			});
		}
	}

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
 *   4. Best-effort roster enrichment via getScene populates participants/observers
 *      on the WorkspaceScene (ready for when .8.25 backend bead lands).
 *
 * Uses the scene's asCharacterId (populated by refresh fan-out) for all
 * per-alt session operations. The playerSessionId parameter is no longer used
 * for backfill — per-alt sessionId from ensureSession is used throughout.
 */
async function select(
	sceneId: string,
	/** Unused legacy parameter kept for call-site compatibility. */
	_playerSessionId: string,
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
	//    Uses per-alt sessionId (not playerId). notAfterMs = attachMomentMs
	//    to avoid the connect-time race (iu8j).
	const attachMomentMs = getAttachMomentMs(characterId);
	const icStream = `scene.${sceneId}.ic`;
	try {
		const history = await queryStreamHistory(altSessionId, icStream, {
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

	// 6. Best-effort roster enrichment from getScene. Populates
	//    participants/observers on the WorkspaceScene for SceneContextRail.
	//    Empty roster is fine until .8.25 lands backend population.
	try {
		const si = await getScene(altSessionId, characterId, sceneId);
		if (si) {
			const participants = si.participants.map((p) => ({ id: p.characterId, name: p.characterName }));
			const observers = si.observers.map((p) => ({ id: p.characterId, name: p.characterName }));
			// Enrich the matching WorkspaceScene in myScenes.
			myScenes = myScenes.map((s) =>
				s.sceneId === sceneId && s.asCharacterId === characterId
					? { ...s, participants, observers, ownerId: si.ownerId }
					: s,
			);
			// Keep watching in sync.
			watching = myScenes.filter((s) => s.role === 'observer');
		}
	} catch {
		// Non-fatal: roster is best-effort until .8.25.
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
 * Merges the post-mutation SceneInfo into the cached scene(s) matched by scene.id.
 * Updates both myScenes and watching in-place (Svelte 5 proxied $state arrays).
 * Fields mapped: state, title, tags, locationId, participants, observers, lastActivityMs.
 * ownerId is intentionally NOT re-applied: ownership is immutable across lifecycle
 * transitions, so the value set by refresh()/select() is preserved.
 * Fields not mapped (no WorkspaceScene counterpart): description, poseOrderMode,
 * contentWarnings, visibility, createdAt, endedAt.
 */
function applySceneInfo(scene: SceneInfo): void {
	const apply = (list: WorkspaceScene[]) => {
		const s = list.find((x) => x.sceneId === scene.id);
		if (!s) return;
		s.state = scene.state;
		s.title = scene.title;
		s.tags = scene.tags;
		s.locationId = scene.locationId;
		if (scene.participants.length > 0) {
			s.participants = scene.participants.map((p) => ({ id: p.characterId, name: p.characterName }));
		}
		if (scene.observers.length > 0) {
			s.observers = scene.observers.map((p) => ({ id: p.characterId, name: p.characterName }));
		}
		if (scene.lastActivityMs) {
			s.lastActivityMs = scene.lastActivityMs;
		}
	};
	apply(myScenes);
	apply(watching);
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
	applySceneInfo,
};

// Named exports for direct import in tests.
export { refresh, select, ingestEvent, bumpUnread, applySceneInfo };
