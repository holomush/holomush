// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * Unit tests for the scenes workspace data layer.
 *
 * Pure/testable logic covered here:
 *   1. eventFrameToLogEntry — verb→kind mapping, null for non-scene frames
 *   2. bumpUnread dedup — skip when sceneId === selectedSceneId (spec D7)
 *   3. ingestEvent — appends to correct scene, ignores non-scene frames
 *   4. select() connectionId ordering — setSceneFocus MUST NOT be called
 *      before connectionId arrives from STREAM_OPENED
 *   5. refresh() fan-out — seeds myScenes with asCharacterId/asCharacterName
 *      tagged per-alt across 2 characters; one failing alt doesn't abort
 *   6. select() roster enrichment — participants/observers populated from getScene
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { eventFrameToLogEntry } from './types';
import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';

// ── helpers ───────────────────────────────────────────────────────────────────

function makeGameEvent(
	type: string,
	overrides: Partial<GameEvent> = {},
): GameEvent {
	return {
		type,
		category: 'communication',
		format: 'action',
		displayTarget: 0,
		timestamp: 1_000_000n, // epoch seconds
		actor: 'Opal Radon',
		actorId: '01HACTOR0000000000000AAAA',
		text: 'waves at the room.',
		metadata: { actor_id: '01HACTOR0000000000000AAAA', scene_id: '01HSCENE0000000000000BBBB', text: 'waves at the room.' },
		eventId: '01HEVENT0000000000000CCCC',
		cursor: new Uint8Array(),
		...overrides,
	} as GameEvent;
}

// ── 1. eventFrameToLogEntry — verb→kind mapping ───────────────────────────────

describe('eventFrameToLogEntry', () => {
	it('maps core-scenes:scene_pose to kind pose', () => {
		const ev = makeGameEvent('core-scenes:scene_pose');
		const entry = eventFrameToLogEntry(ev);
		expect(entry).not.toBeNull();
		expect(entry?.kind).toBe('pose');
	});

	it('maps core-scenes:scene_say to kind say', () => {
		const ev = makeGameEvent('core-scenes:scene_say');
		const entry = eventFrameToLogEntry(ev);
		expect(entry?.kind).toBe('say');
	});

	it('maps core-scenes:scene_ooc to kind ooc', () => {
		const ev = makeGameEvent('core-scenes:scene_ooc');
		const entry = eventFrameToLogEntry(ev);
		expect(entry?.kind).toBe('ooc');
	});

	it('maps core-scenes:scene_emit to kind system', () => {
		const ev = makeGameEvent('core-scenes:scene_emit');
		const entry = eventFrameToLogEntry(ev);
		expect(entry?.kind).toBe('system');
	});

	it('returns null for non-scene frames (movement)', () => {
		const ev = makeGameEvent('arrive');
		expect(eventFrameToLogEntry(ev)).toBeNull();
	});

	it('returns null for non-scene frames (command_response)', () => {
		const ev = makeGameEvent('command_response');
		expect(eventFrameToLogEntry(ev)).toBeNull();
	});

	it('returns null for empty type', () => {
		const ev = makeGameEvent('');
		expect(eventFrameToLogEntry(ev)).toBeNull();
	});

	it('sets timestampMs from epoch seconds * 1000', () => {
		const ev = makeGameEvent('core-scenes:scene_say', { timestamp: 1_717_000_000n });
		const entry = eventFrameToLogEntry(ev);
		expect(entry?.timestampMs).toBe(1_717_000_000_000);
	});

	it('carries actorId from metadata actor_id when present', () => {
		const ev = makeGameEvent('core-scenes:scene_pose', {
			metadata: { actor_id: 'META_ACTOR_ID', scene_id: '01HSCENE', text: 'nods.' },
		});
		const entry = eventFrameToLogEntry(ev);
		expect(entry?.actorId).toBe('META_ACTOR_ID');
	});

	it('falls back to ev.actorId when metadata has no actor_id', () => {
		const ev = makeGameEvent('core-scenes:scene_pose', {
			actorId: 'FALLBACK_ACTOR',
			metadata: { scene_id: '01HSCENE', text: 'nods.' },
		});
		const entry = eventFrameToLogEntry(ev);
		expect(entry?.actorId).toBe('FALLBACK_ACTOR');
	});

	it('uses ev.eventId as the log entry id', () => {
		const ev = makeGameEvent('core-scenes:scene_pose', {
			eventId: 'UNIQUE_EVENT_ID',
		});
		const entry = eventFrameToLogEntry(ev);
		expect(entry?.id).toBe('UNIQUE_EVENT_ID');
	});
});

// ── 2 & 3. bumpUnread dedup + ingestEvent via store ──────────────────────────
//
// workspaceStore.svelte.ts uses $state at module level which requires the
// Svelte compiler. We verify the bumpUnread dedup rule and ingestEvent
// by directly calling the exported action functions from the module
// under vi.mock so the $state calls are replaced with plain refs.

// Mock the altSessions and client modules so we don't hit real network.
vi.mock('./altSessions.svelte', () => ({
	ensureSession: vi.fn().mockResolvedValue('MOCK_SESSION_ID'),
	awaitConnectionId: vi.fn().mockResolvedValue('MOCK_CONN_ID'),
	getAttachMomentMs: vi.fn().mockReturnValue(0n),
	closeSession: vi.fn(),
	closeAllSessions: vi.fn(),
}));

vi.mock('./client', () => ({
	client: {},
	listMyScenes: vi.fn().mockResolvedValue([]),
	getScene: vi.fn().mockResolvedValue(undefined),
	listScenes: vi.fn().mockResolvedValue([]),
	watchScene: vi.fn().mockResolvedValue(undefined),
	exportScene: vi.fn().mockResolvedValue(undefined),
	setSceneFocus: vi.fn().mockResolvedValue(undefined),
	sendSceneCommand: vi.fn().mockResolvedValue(undefined),
	queryStreamHistory: vi.fn().mockResolvedValue({ events: [], hasMore: false }),
}));

describe('bumpUnread dedup (spec D7)', () => {
	it('skips bump when sceneId matches selectedSceneId', async () => {
		// Import dynamically so mocks are applied.
		const { workspaceStore } = await import('./workspaceStore.svelte');
		const { setSceneFocus } = await import('./client');

		// Select the scene first (which sets selectedSceneId).
		await workspaceStore.select('SCENE_A', 'PLAYER_SESSION', 'CHAR_1');

		const unreadBefore = workspaceStore.unreadBySceneId['SCENE_A'] ?? 0;
		workspaceStore.bumpUnread('SCENE_A');
		const unreadAfter = workspaceStore.unreadBySceneId['SCENE_A'] ?? 0;

		// Should remain 0 — focused scene must not accumulate unread.
		expect(unreadAfter).toBe(unreadBefore);
		// setSceneFocus must have been called with the connectionId from awaitConnectionId.
		expect(setSceneFocus).toHaveBeenCalledWith('MOCK_SESSION_ID', 'MOCK_CONN_ID', 'SCENE_A');
	});

	it('increments unread for a non-focused scene', async () => {
		const { workspaceStore } = await import('./workspaceStore.svelte');

		// Select a different scene so SCENE_B is unfocused.
		await workspaceStore.select('SCENE_X', 'PLAYER_SESSION', 'CHAR_2');

		const unreadBefore = workspaceStore.unreadBySceneId['SCENE_B'] ?? 0;
		workspaceStore.bumpUnread('SCENE_B');
		expect((workspaceStore.unreadBySceneId['SCENE_B'] ?? 0)).toBe(unreadBefore + 1);
	});
});

describe('connectionId ordering invariant', () => {
	it('calls setSceneFocus only after awaitConnectionId resolves', async () => {
		const altSessions = await import('./altSessions.svelte');
		const clientMod = await import('./client');
		const { workspaceStore } = await import('./workspaceStore.svelte');

		const callOrder: string[] = [];
		vi.mocked(altSessions.awaitConnectionId).mockImplementation(async () => {
			callOrder.push('awaitConnectionId');
			return 'CONN_ID';
		});
		vi.mocked(clientMod.setSceneFocus).mockImplementation(async () => {
			callOrder.push('setSceneFocus');
		});

		await workspaceStore.select('SCENE_ORDER', 'PLAYER_SESSION', 'CHAR_3');

		const awaitIdx = callOrder.indexOf('awaitConnectionId');
		const focusIdx = callOrder.indexOf('setSceneFocus');
		expect(awaitIdx).toBeGreaterThanOrEqual(0);
		expect(focusIdx).toBeGreaterThanOrEqual(0);
		// awaitConnectionId must complete before setSceneFocus is invoked.
		expect(awaitIdx).toBeLessThan(focusIdx);
	});
});

describe('ingestEvent', () => {
	it('appends a LogEntry for a scene_pose frame', async () => {
		const { workspaceStore } = await import('./workspaceStore.svelte');

		const ev = makeGameEvent('core-scenes:scene_pose', {
			eventId: 'INGEST_EV_001',
			metadata: { actor_id: 'ACT1', scene_id: 'SCENE_INGEST', text: 'nods.' },
		});

		const before = workspaceStore.logsBySceneId['SCENE_INGEST']?.length ?? 0;
		workspaceStore.ingestEvent('SESSION_1', ev);
		const after = workspaceStore.logsBySceneId['SCENE_INGEST']?.length ?? 0;

		expect(after).toBe(before + 1);
		const last = workspaceStore.logsBySceneId['SCENE_INGEST']?.at(-1);
		expect(last?.id).toBe('INGEST_EV_001');
		expect(last?.kind).toBe('pose');
	});

	it('ignores non-scene frames without mutating log', async () => {
		const { workspaceStore } = await import('./workspaceStore.svelte');

		const ev = makeGameEvent('arrive', {
			eventId: 'MOVEMENT_EV_001',
		});

		const before = Object.keys(workspaceStore.logsBySceneId).length;
		workspaceStore.ingestEvent('SESSION_1', ev);
		const after = Object.keys(workspaceStore.logsBySceneId).length;

		// No new scene key should have been added.
		expect(after).toBe(before);
	});
});

describe('ingestEvent cross-scene routing (medium fix)', () => {
	it('routes event for scene B to scene B log regardless of sessionId param', async () => {
		const { workspaceStore } = await import('./workspaceStore.svelte');

		const ev = makeGameEvent('core-scenes:scene_pose', {
			eventId: 'EV_CROSS_SCENE_001',
			metadata: { actor_id: 'ACT_B', scene_id: 'SCENE_CROSS_B', text: 'gestures.' },
		});

		const before = workspaceStore.logsBySceneId['SCENE_CROSS_B']?.length ?? 0;
		workspaceStore.ingestEvent('ANY_ALT_SESSION_ID', ev);
		const after = workspaceStore.logsBySceneId['SCENE_CROSS_B']?.length ?? 0;

		// Event lands in the correct scene log by parsed scene_id.
		expect(after).toBe(before + 1);
		expect(workspaceStore.logsBySceneId['SCENE_CROSS_B']?.at(-1)?.id).toBe('EV_CROSS_SCENE_001');
	});

	it('drops a scene frame with no scene_id in metadata without mutating any log', async () => {
		const { workspaceStore } = await import('./workspaceStore.svelte');

		const ev = makeGameEvent('core-scenes:scene_pose', {
			eventId: 'EV_NO_SCENE_ID',
			metadata: { actor_id: 'ACT', text: 'no scene' }, // no scene_id key
		});

		const keysBefore = Object.keys(workspaceStore.logsBySceneId).length;
		expect(() => workspaceStore.ingestEvent('ANY_SESSION', ev)).not.toThrow();
		expect(Object.keys(workspaceStore.logsBySceneId).length).toBe(keysBefore);
	});

	it('routes event for scene B to scene B log even when scene A is selected', async () => {
		const { workspaceStore } = await import('./workspaceStore.svelte');

		// Select scene A.
		await workspaceStore.select('SCENE_CROSS_A', 'PLAYER_SESSION', 'CHAR_CROSS_1');

		const evB = makeGameEvent('core-scenes:scene_pose', {
			eventId: 'EV_B_WHILE_A_SELECTED',
			metadata: { actor_id: 'ACT_B', scene_id: 'SCENE_CROSS_BX', text: 'waves.' },
		});

		workspaceStore.ingestEvent('ALT_B_SESSION', evB);

		// Must be in scene BX, NOT in scene A.
		expect(
			workspaceStore.logsBySceneId['SCENE_CROSS_BX']?.some((e) => e.id === 'EV_B_WHILE_A_SELECTED'),
		).toBe(true);
		expect(
			workspaceStore.logsBySceneId['SCENE_CROSS_A']?.some((e) => e.id === 'EV_B_WHILE_A_SELECTED'),
		).toBeFalsy();
	});
});

// ── 5. refresh() fan-out ──────────────────────────────────────────────────────

describe('refresh fan-out', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	function makeCharacterSceneInfo(sceneId: string, role = 'member') {
		return {
			$typeName: 'holomush.scene.v1.CharacterSceneInfo',
			role,
			lastActivityMs: 1_717_000_000_000n,
			entryCount: 5n,
			scene: {
				$typeName: 'holomush.scene.v1.SceneInfo',
				id: sceneId,
				title: `Scene ${sceneId}`,
				locationId: 'LOC1',
				state: 'active',
				tags: [],
				description: '',
				poseOrderMode: 'free',
				contentWarnings: [],
				visibility: 'open',
				ownerId: 'CHAR_OWNER',
				participants: [],
				observers: [],
			},
		} as never;
	}

	it('tags each WorkspaceScene with asCharacterId and asCharacterName from the queried alt', async () => {
		const clientMod = await import('./client');
		const altSessions = await import('./altSessions.svelte');
		const { workspaceStore } = await import('./workspaceStore.svelte');

		// Two characters with distinct sessions.
		vi.mocked(altSessions.ensureSession)
			.mockResolvedValueOnce('SESSION_CHAR_A')
			.mockResolvedValueOnce('SESSION_CHAR_B');

		// Each alt returns one scene.
		vi.mocked(clientMod.listMyScenes)
			.mockResolvedValueOnce([makeCharacterSceneInfo('SCENE_FROM_A')])
			.mockResolvedValueOnce([makeCharacterSceneInfo('SCENE_FROM_B')]);

		await workspaceStore.refresh([
			{ characterId: 'CHAR_A', characterName: 'Alice' },
			{ characterId: 'CHAR_B', characterName: 'Bob' },
		]);

		expect(workspaceStore.myScenes).toHaveLength(2);

		const sceneA = workspaceStore.myScenes.find((s) => s.sceneId === 'SCENE_FROM_A');
		expect(sceneA?.asCharacterId).toBe('CHAR_A');
		expect(sceneA?.asCharacterName).toBe('Alice');

		const sceneB = workspaceStore.myScenes.find((s) => s.sceneId === 'SCENE_FROM_B');
		expect(sceneB?.asCharacterId).toBe('CHAR_B');
		expect(sceneB?.asCharacterName).toBe('Bob');
	});

	it('passes the per-alt sessionId (from ensureSession) to listMyScenes, not playerId', async () => {
		const clientMod = await import('./client');
		const altSessions = await import('./altSessions.svelte');
		const { workspaceStore } = await import('./workspaceStore.svelte');

		vi.mocked(altSessions.ensureSession).mockResolvedValue('ALT_SESSION_XYZ');
		vi.mocked(clientMod.listMyScenes).mockResolvedValue([]);

		await workspaceStore.refresh([{ characterId: 'CHAR_Z', characterName: 'Zara' }]);

		// listMyScenes must receive the per-alt sessionId, not an empty string or playerId.
		expect(clientMod.listMyScenes).toHaveBeenCalledWith('ALT_SESSION_XYZ', 'CHAR_Z');
	});

	it('tolerates one failing alt and still populates scenes from the healthy alt', async () => {
		const clientMod = await import('./client');
		const altSessions = await import('./altSessions.svelte');
		const { workspaceStore } = await import('./workspaceStore.svelte');

		vi.mocked(altSessions.ensureSession)
			.mockRejectedValueOnce(new Error('auth expired for CHAR_BAD'))
			.mockResolvedValueOnce('SESSION_CHAR_GOOD');

		vi.mocked(clientMod.listMyScenes).mockResolvedValueOnce([
			makeCharacterSceneInfo('SCENE_GOOD'),
		]);

		await workspaceStore.refresh([
			{ characterId: 'CHAR_BAD', characterName: 'BadAlt' },
			{ characterId: 'CHAR_GOOD', characterName: 'GoodAlt' },
		]);

		// Only the healthy alt's scene appears; failed alt is silently skipped.
		expect(workspaceStore.myScenes).toHaveLength(1);
		expect(workspaceStore.myScenes[0]?.sceneId).toBe('SCENE_GOOD');
		expect(workspaceStore.myScenes[0]?.asCharacterName).toBe('GoodAlt');
	});

	it('seeds myScenes from listMyScenes response (legacy compat)', async () => {
		const clientMod = await import('./client');
		const { workspaceStore } = await import('./workspaceStore.svelte');

		vi.mocked(clientMod.listMyScenes).mockResolvedValueOnce([
			makeCharacterSceneInfo('SCENE_REFRESH_1'),
		]);

		await workspaceStore.refresh([{ characterId: 'CHAR_X', characterName: 'Xeno' }]);

		expect(workspaceStore.myScenes).toHaveLength(1);
		expect(workspaceStore.myScenes[0]?.sceneId).toBe('SCENE_REFRESH_1');
		expect(workspaceStore.myScenes[0]?.title).toBe('Scene SCENE_REFRESH_1');
		expect(workspaceStore.myScenes[0]?.role).toBe('member');
		expect(workspaceStore.myScenes[0]?.entryCount).toBe(5n);
	});
});

// ── 6. select() roster enrichment ────────────────────────────────────────────

describe('select roster enrichment', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it('enriches participants and observers on the WorkspaceScene after select', async () => {
		const clientMod = await import('./client');
		const altSessions = await import('./altSessions.svelte');
		const { workspaceStore } = await import('./workspaceStore.svelte');

		vi.mocked(altSessions.ensureSession).mockResolvedValue('SESSION_ENRICH');
		vi.mocked(clientMod.getScene).mockResolvedValueOnce({
			$typeName: 'holomush.scene.v1.SceneInfo',
			id: 'SCENE_ENRICH',
			title: 'The Lab',
			locationId: 'LOC2',
			state: 'active',
			tags: [],
			description: '',
			poseOrderMode: 'free',
			contentWarnings: [],
			visibility: 'open',
			ownerId: 'CHAR_P1',
			participants: [
				{ $typeName: 'holomush.scene.v1.ParticipantInfo', characterId: 'CHAR_P1', characterName: 'Petra' },
				{ $typeName: 'holomush.scene.v1.ParticipantInfo', characterId: 'CHAR_P2', characterName: 'Quinn' },
			],
			observers: [
				{ $typeName: 'holomush.scene.v1.ParticipantInfo', characterId: 'CHAR_O1', characterName: 'Orin' },
			],
		} as never);

		// Seed myScenes with a scene for CHAR_P1.
		vi.mocked(clientMod.listMyScenes).mockResolvedValueOnce([
			{
				$typeName: 'holomush.scene.v1.CharacterSceneInfo',
				role: 'owner',
				lastActivityMs: 0n,
				entryCount: 0n,
				scene: {
					$typeName: 'holomush.scene.v1.SceneInfo',
					id: 'SCENE_ENRICH',
					title: 'The Lab',
					locationId: 'LOC2',
					state: 'active',
					tags: [],
					description: '',
					poseOrderMode: 'free',
					contentWarnings: [],
					visibility: 'open',
					ownerId: 'CHAR_P1',
					participants: [],
					observers: [],
				},
			} as never,
		]);
		await workspaceStore.refresh([{ characterId: 'CHAR_P1', characterName: 'Petra' }]);

		await workspaceStore.select('SCENE_ENRICH', '', 'CHAR_P1');

		const scene = workspaceStore.myScenes.find(
			(s) => s.sceneId === 'SCENE_ENRICH' && s.asCharacterId === 'CHAR_P1',
		);
		expect(scene?.participants).toHaveLength(2);
		expect(scene?.participants?.[0]).toEqual({ id: 'CHAR_P1', name: 'Petra' });
		expect(scene?.participants?.[1]).toEqual({ id: 'CHAR_P2', name: 'Quinn' });
		expect(scene?.observers).toHaveLength(1);
		expect(scene?.observers?.[0]).toEqual({ id: 'CHAR_O1', name: 'Orin' });
	});

	it('does not abort select when getScene fails', async () => {
		const clientMod = await import('./client');
		const altSessions = await import('./altSessions.svelte');
		const { workspaceStore } = await import('./workspaceStore.svelte');

		vi.mocked(altSessions.ensureSession).mockResolvedValue('SESSION_NOENRICH');
		vi.mocked(clientMod.getScene).mockRejectedValueOnce(new Error('backend not ready'));

		// Should complete without throwing even if getScene fails.
		await expect(
			workspaceStore.select('SCENE_NOENRICH', '', 'CHAR_NE'),
		).resolves.not.toThrow();

		// setSceneFocus was still called (the main path succeeded).
		expect(clientMod.setSceneFocus).toHaveBeenCalledWith(
			'SESSION_NOENRICH',
			expect.any(String), // connectionId from awaitConnectionId (may vary across mock reset cycles)
			'SCENE_NOENRICH',
		);
	});
});
