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

async function refetchTally(): Promise<void> {
	if (!activeAttemptId) {
		tally = null;
		return;
	}
	const sessionId = await ensureSession(characterId);
	try {
		const snap = await getPublishedScene(sessionId, { characterId, publishedSceneId: activeAttemptId });
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
		stale = true; // transient: keep last-known tally, retry on next event
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
	// internal, exported for Task 3 + tests:
	_refetchTally: refetchTally,
	_setActiveAttempt: (id: string) => { activeAttemptId = id; },
};
