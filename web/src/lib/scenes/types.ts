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
