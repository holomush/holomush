// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';

/**
 * WorkspaceScene is the client-side projection of a scene the current
 * character participates in (owns, is a member of, or is watching).
 * Seeded by ListMyScenes; live fields (unread) updated by stream events.
 */
export interface WorkspaceScene {
	sceneId: string;
	title: string;
	locationId: string;
	state: string;
	tags: string[];
	/** 'owner' | 'member' | 'observer' */
	role: string;
	/** Character ID the alt session is acting as for this scene. */
	asCharacterId: string;
	asCharacterName: string;
	/** Epoch-ms of most-recent IC activity; 0 when log is empty. */
	lastActivityMs: bigint;
	/** Total IC log entries (workspace activity panel). */
	entryCount: bigint;
	/** In-session unread badge count; cleared on select(). */
	unread: number;
	/**
	 * Scene participants (role=owner or member). Populated by select() via
	 * getScene(); empty until .8.25 backend bead lands roster population.
	 */
	participants?: { id: string; name: string }[];
	/**
	 * Scene observers (role=observer). Populated by select() via getScene();
	 * listed separately per INV-SCENE-61.
	 */
	observers?: { id: string; name: string }[];
}

/**
 * LogEntry is one rendered pose/say/ooc/system line in the workspace log.
 */
export interface LogEntry {
	/** Originating event ULID for dedup. */
	id: string;
	kind: 'pose' | 'say' | 'ooc' | 'system';
	actorId: string;
	actorName: string;
	text: string;
	/** Epoch-ms; derived from GameEvent.timestamp (seconds) * 1000. */
	timestampMs: number;
	contentWarning?: string;
}

/**
 * Parses JSONL export bytes (from ExportSceneLog or DownloadPublicSceneArchive)
 * into LogEntry[]. Each line is a PublishedSceneEntry shape:
 *   {"speaker": string, "kind": string, "content": string}
 * kind values from the plugin renderer: "pose", "say", "ooc", "emit" (→ system).
 * Synthesises a stable id from the line index; timestampMs is 0 (not present
 * in the export format — callers render in file order, not by timestamp).
 */
export function jsonlToLogEntries(bytes: Uint8Array | string): LogEntry[] {
	const text = typeof bytes === 'string' ? bytes : new TextDecoder().decode(bytes);
	const entries: LogEntry[] = [];
	let idx = 0;
	for (const line of text.split('\n')) {
		const trimmed = line.trim();
		if (!trimmed) continue;
		try {
			const obj = JSON.parse(trimmed) as { speaker?: string; kind?: string; content?: string };
			const rawKind = obj.kind ?? '';
			// Map plugin renderer kind values to LogEntry kinds.
			let kind: LogEntry['kind'];
			if (rawKind === 'pose') kind = 'pose';
			else if (rawKind === 'say') kind = 'say';
			else if (rawKind === 'ooc') kind = 'ooc';
			else kind = 'system'; // "emit" and any unknown kind → system narration
			entries.push({
				id: `export-${idx}`,
				kind,
				actorId: '',
				actorName: obj.speaker ?? '',
				text: obj.content ?? '',
				timestampMs: 0,
			});
		} catch {
			// Malformed line — skip silently; spec says do NOT throw
		}
		idx++;
	}
	return entries;
}

/**
 * Map from plugin-qualified event type to LogEntry kind.
 * Covers the four verbs emitted by core-scenes for IC events:
 *   core-scenes:scene_pose  → 'pose'
 *   core-scenes:scene_say   → 'say'
 *   core-scenes:scene_ooc   → 'ooc'
 *   core-scenes:scene_emit  → 'system' (GM/system narration)
 *
 * The payload shape from handleEmit's marshal is {actor_id, scene_id, text}.
 */
const KIND_MAP: Record<string, LogEntry['kind']> = {
	'core-scenes:scene_pose': 'pose',
	'core-scenes:scene_say': 'say',
	'core-scenes:scene_ooc': 'ooc',
	'core-scenes:scene_emit': 'system',
};

/**
 * Parses a GameEvent frame into a LogEntry for the workspace log.
 * Returns null for frames that are not scene IC events (e.g. movement,
 * presence, command_response) — callers should discard null returns.
 */
export function eventFrameToLogEntry(ev: GameEvent): LogEntry | null {
	const kind = KIND_MAP[ev.type];
	if (!kind) return null;

	// Payload is JSON-encoded {actor_id, scene_id, text} per handleEmit.
	let actorId = ev.actorId;
	let text = ev.text;

	// Prefer decoded metadata fields when available; fall back to top-level fields.
	if (ev.metadata) {
		const meta = ev.metadata as Record<string, unknown>;
		if (typeof meta['actor_id'] === 'string' && meta['actor_id']) {
			actorId = meta['actor_id'] as string;
		}
		if (typeof meta['text'] === 'string') {
			text = meta['text'] as string;
		}
	}

	return {
		id: ev.eventId,
		kind,
		actorId,
		actorName: ev.actor,
		text,
		timestampMs: Number(ev.timestamp) * 1000,
	};
}
