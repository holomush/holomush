// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * workspaceStore is the Svelte 5 runes state container for the scenes
 * workspace. It holds the scene list, per-scene log entries, unread badge
 * counts, the currently selected sceneId, and per-scene focus-ready state.
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
 *                         focusReadyBySceneId[id] flips false→true around the
 *                         setSceneFocus call (see isFocusReady()).
 *   ingestEvent()       – append LogEntry to the focused scene's log
 *   bumpUnread()        – increment badge (skip when sceneId === selectedSceneId)
 *   isFocusReady(id)    – true once select(id)'s setSceneFocus call resolved
 */

import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';
import type { SceneInfo } from '$lib/connect/holomush/scene/v1/scene_pb';
import { eventFrameToLogEntry, type LogEntry, type WorkspaceScene } from './types';
import { publishStore } from './publishStore.svelte';
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

// focusReadyBySceneId[sceneId] === true once select() has awaited setSceneFocus.
// The SceneComposer gates sends on this so raw `pose` never races ahead of the
// server-side focus write (holomush-g1qcw).
let focusReadyBySceneId = $state<Record<string, boolean>>({});

// The character's persisted global scene-notify preference, captured from the
// ListMyScenes read-back so the prefs UI renders the global-notify toggle on
// reload (round-3 Concern 1). Defaults true (notifications on). Multi-alt: the
// last-refreshed alt's value wins — the global preference is per-character and
// the prefs UI reads it for the acting alt.
let globalNotifyEnabled = $state(true);

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
			const { scenes, globalNotifyEnabled: altGlobalNotify } = await listMyScenes(
				sessionId,
				characterId,
			);
			return { characterId, characterName, scenes, altGlobalNotify };
		}),
	);

	const next: WorkspaceScene[] = [];
	// Capture the persisted global-notify preference from the last successful
	// alt fetch (round-3 Concern 1 read-back). The preference is per-character;
	// the prefs UI reads it for the acting alt.
	let nextGlobalNotify = globalNotifyEnabled;
	for (const result of results) {
		if (result.status === 'rejected') {
			console.warn('[workspaceStore] refresh: alt fetch failed', result.reason);
			continue;
		}
		const { characterId, characterName, scenes, altGlobalNotify } = result.value;
		nextGlobalNotify = altGlobalNotify;
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
				muted: csi.muted ?? false,
			});
		}
	}

	myScenes = next;
	watching = next.filter((s) => s.role === 'observer');
	globalNotifyEnabled = nextGlobalNotify;
}

/**
 * Optimistically updates the local per-scene mute flag after a successful
 * MuteScene RPC, so the toggle reflects immediately. The persisted state is
 * authoritative on the next refresh() (round-3 Concern 1 read-back).
 */
function setMuted(sceneId: string, characterId: string, muted: boolean): void {
	myScenes = myScenes.map((s) =>
		s.sceneId === sceneId && s.asCharacterId === characterId ? { ...s, muted } : s,
	);
	watching = myScenes.filter((s) => s.role === 'observer');
}

/**
 * Optimistically updates the local global-notify preference after a successful
 * SetSceneNotifyPref RPC. Authoritative value is re-read on the next refresh().
 */
function setGlobalNotifyEnabled(enabled: boolean): void {
	globalNotifyEnabled = enabled;
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

	// 3. Update selected scene and clear unread. Focus is not yet confirmed by
	//    the server, so the composer's focus-ready gate starts closed.
	selectedSceneId = sceneId;
	unreadBySceneId = { ...unreadBySceneId, [sceneId]: 0 };
	focusReadyBySceneId = { ...focusReadyBySceneId, [sceneId]: false };

	// 3b. Cold-start the publish-vote store for this scene so ScenePublishPanel
	//     renders any in-progress vote AND publishStore.onEvent has a sceneId to
	//     match incoming scene_publish_* events against (its cross-scene filter
	//     compares ev.metadata.scene_id to the store's sceneId, set only here via
	//     loadColdStart). Best-effort and fire-and-forget — never blocks scene
	//     selection. (holomush-5rh.24.41.10)
	void publishStore.loadColdStart(characterId, sceneId).catch(() => {});

	// 4. Notify server of the new focus so SCENE_ACTIVITY suppression fires.
	await setSceneFocus(altSessionId, connectionId, sceneId);
	focusReadyBySceneId = { ...focusReadyBySceneId, [sceneId]: true };

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
	// Publish lifecycle/vote events are NOT IC log entries — fan them to the
	// publish store, then fall through (eventFrameToLogEntry returns null for
	// them, so the log path below is a no-op). scene_id rides ev.metadata,
	// stamped by translate.go's sceneIDFromSubject for all scene IC events.
	if (ev.type.startsWith('core-scenes:scene_publish_')) {
		publishStore.onEvent(ev as unknown as { type: string; metadata?: Record<string, unknown> });
	}

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
 * Reports whether the server has confirmed scene focus for sceneId — i.e.
 * select()'s setSceneFocus call for this scene has resolved. False while
 * unset (scene never selected) or while the focus write is still in flight.
 * SceneComposer gates Pose/Say/OOC sends on this to close the sub-100ms race
 * where a raw `pose` could reach the server before the scene-focus write
 * (holomush-g1qcw).
 */
function isFocusReady(sceneId: string): boolean {
	return focusReadyBySceneId[sceneId] === true;
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
		for (const s of list) {
			if (s.sceneId !== scene.id) continue;
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
	get globalNotifyEnabled() {
		return globalNotifyEnabled;
	},
	refresh,
	select,
	ingestEvent,
	bumpUnread,
	applySceneInfo,
	isFocusReady,
	setMuted,
	setGlobalNotifyEnabled,
};

// Named exports for direct import in tests.
export {
	refresh,
	select,
	ingestEvent,
	bumpUnread,
	applySceneInfo,
	isFocusReady,
	setMuted,
	setGlobalNotifyEnabled,
};
