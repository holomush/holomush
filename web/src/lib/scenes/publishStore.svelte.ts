// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ensureSession } from './altSessions.svelte';
import { getScene, getPublishedScene } from './client';

export type Tally = { yes: number; no: number; pending: number };

/** PermissionDenied means the caller is a non-participant observer. */
function isPermissionDenied(err: unknown): boolean {
	const code = (err as { code?: unknown })?.code;
	return code === 'permission_denied' || code === 7; // ConnectError string or numeric code
}

let activeAttemptId = $state('');
let phase = $state('');
let tally = $state<Tally | null>(null);
let isParticipant = $state(false);
let stale = $state(false);
let sceneId = $state('');
let characterId = $state('');

const LIFECYCLE = new Set([
	'core-scenes:scene_publish_started',
	'core-scenes:scene_publish_resolved',
	'core-scenes:scene_publish_withdrawn',
	'core-scenes:scene_publish_cooloff_started',
	'core-scenes:scene_publish_vote_attempts_extended',
]);
const VOTE_CAST = 'core-scenes:scene_publish_vote_cast';
const DEBOUNCE_MS = 300;

let debounceTimer: ReturnType<typeof setTimeout> | null = null;
let inFlight: AbortController | null = null;

function scheduleTallyRefetch(): void {
	if (debounceTimer) clearTimeout(debounceTimer);
	debounceTimer = setTimeout(() => {
		debounceTimer = null;
		inFlight?.abort();           // cancel any stale in-flight refetch
		inFlight = new AbortController();
		void refetchTally(inFlight.signal);
	}, DEBOUNCE_MS);
}

async function reloadPointer(): Promise<void> {
	const sessionId = await ensureSession(characterId);
	const scene = await getScene(sessionId, characterId, sceneId);
	activeAttemptId = scene?.activePublishAttemptId ?? '';
	phase = scene?.publishStatus ?? '';
	if (activeAttemptId) {
		scheduleTallyRefetch();
	} else {
		// Attempt is gone (resolved/withdrawn) — cancel any pending or in-flight
		// tally refetch so a late response cannot repopulate the tally.
		if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = null; }
		inFlight?.abort();
		inFlight = null;
		tally = null;
		isParticipant = false;
	}
}

function onEvent(ev: { type: string; metadata?: Record<string, unknown> }): void {
	const evScene = typeof ev.metadata?.['scene_id'] === 'string' ? ev.metadata['scene_id'] : '';
	if (evScene !== sceneId) return;                 // cross-scene isolation
	if (LIFECYCLE.has(ev.type)) { void reloadPointer(); return; }
	if (ev.type === VOTE_CAST) {
		if (!isParticipant) return;                  // observer: ignore vote_cast
		scheduleTallyRefetch();
	}
}

async function refetchTally(signal?: AbortSignal): Promise<void> {
	if (!activeAttemptId) {
		tally = null;
		return;
	}
	const sessionId = await ensureSession(characterId);
	try {
		const snap = await getPublishedScene(
			sessionId,
			{ characterId, publishedSceneId: activeAttemptId },
			signal,
		);
		if (signal?.aborted) return;                 // a newer refetch superseded us
		isParticipant = true;
		phase = snap.status;
		tally = snap.voteSummary
			? { yes: snap.voteSummary.yes, no: snap.voteSummary.no, pending: snap.voteSummary.pending }
			: { yes: 0, no: 0, pending: 0 };
		stale = false;
	} catch (err) {
		if (isPermissionDenied(err)) {
			isParticipant = false; // observer: existence only, never an error
			tally = null;
			return;
		}
		if (!signal?.aborted) stale = true; // transient: keep last-known tally, retry on next event
	}
}

async function loadColdStart(charId: string, scnId: string): Promise<void> {
	characterId = charId;
	sceneId = scnId;
	const sessionId = await ensureSession(charId);
	const scene = await getScene(sessionId, charId, scnId);
	activeAttemptId = scene?.activePublishAttemptId ?? '';
	phase = scene?.publishStatus ?? '';
	if (!activeAttemptId) {
		isParticipant = false;
		tally = null;
		return;
	}
	await refetchTally();
}

function reset(): void {
	if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = null; }
	inFlight?.abort(); inFlight = null;
	activeAttemptId = '';
	phase = '';
	tally = null;
	isParticipant = false;
	stale = false;
	sceneId = '';
	characterId = '';
}

export const publishStore = {
	get activeAttemptId() { return activeAttemptId; },
	get voteInProgress() { return activeAttemptId !== ''; },
	get phase() { return phase; },
	get tally() { return tally; },
	get isParticipant() { return isParticipant; },
	get stale() { return stale; },
	loadColdStart,
	reset,
	onEvent,
	// internal, exported for Task 3 + tests:
	_refetchTally: refetchTally,
	_setActiveAttempt: (id: string) => { activeAttemptId = id; },
};
