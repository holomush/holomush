import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const { getScene, getPublishedScene } = vi.hoisted(() => ({ getScene: vi.fn(), getPublishedScene: vi.fn() }));
vi.mock('./client', () => ({ getScene, getPublishedScene }));
vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'SESS') }));

import { publishStore } from './publishStore.svelte';

beforeEach(() => {
	vi.clearAllMocks();
	publishStore.reset();
});

function makeEvent(type: string, scnId: string): { type: string; metadata: Record<string, unknown> } {
	return { type: `core-scenes:${type}`, metadata: { scene_id: scnId } };
}

describe('publishStore cold-start', () => {
	it('participant with active attempt loads the tally', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({
			id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 2, no: 0, pending: 3 },
		});
		await publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.voteInProgress).toBe(true);
		expect(publishStore.activeAttemptId).toBe('att-1');
		expect(publishStore.isParticipant).toBe(true);
		expect(publishStore.tally).toEqual({ yes: 2, no: 0, pending: 3 });
	});

	it('observer (PermissionDenied on tally) gets existence-only, no counts, no error', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockRejectedValue({ code: 'permission_denied' });
		await publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.voteInProgress).toBe(true);
		expect(publishStore.isParticipant).toBe(false);
		expect(publishStore.tally).toBeNull();
	});

	it('no active attempt → not in progress, no tally fetch', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: '', publishStatus: '' });
		await publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.voteInProgress).toBe(false);
		expect(getPublishedScene).not.toHaveBeenCalled();
	});
});

describe('publishStore loadColdStart concurrency + loading', () => {
	it('a superseded (slower) cold-start does not clobber the newer scene state', async () => {
		// Select scene A (slow getScene), then scene B (fast). A's late resolution
		// MUST bail via the sequence guard rather than overwrite B's state.
		let resolveA!: (v: unknown) => void;
		const aGetScene = new Promise((r) => {
			resolveA = r;
		});
		getScene.mockImplementation((_s: string, _c: string, scn: string) =>
			scn === 'SCENE_A'
				? aGetScene
				: Promise.resolve({ activePublishAttemptId: 'att-B', publishStatus: 'COOLOFF' }),
		);
		getPublishedScene.mockResolvedValue({ id: 'att-B', status: 'COOLOFF', voteSummary: { yes: 0, no: 1, pending: 2 } });

		const pA = publishStore.loadColdStart('C1', 'SCENE_A');
		const pB = publishStore.loadColdStart('C1', 'SCENE_B');
		await pB;
		expect(publishStore.activeAttemptId).toBe('att-B');
		expect(publishStore.phase).toBe('COOLOFF');

		// A resolves LATE — the guard must drop it.
		resolveA({ activePublishAttemptId: 'att-A', publishStatus: 'COLLECTING' });
		await pA;
		expect(publishStore.activeAttemptId).toBe('att-B');
		expect(publishStore.phase).toBe('COOLOFF');
	});

	it('loading is true during cold-start and false after it resolves', async () => {
		let resolveScene!: (v: unknown) => void;
		getScene.mockReturnValue(
			new Promise((r) => {
				resolveScene = r;
			}),
		);
		const p = publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.loading).toBe(true);
		resolveScene({ activePublishAttemptId: '', publishStatus: '' });
		await p;
		expect(publishStore.loading).toBe(false);
	});
});

describe('publishStore onEvent', () => {
	beforeEach(() => { vi.useFakeTimers(); });
	afterEach(() => { vi.useRealTimers(); });

	it('participant vote_cast triggers a debounced single tally refetch', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({ id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 1, no: 0, pending: 4 } });
		await publishStore.loadColdStart('C1', 'SC1');
		getPublishedScene.mockClear();
		// three rapid votes
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(getPublishedScene).toHaveBeenCalledTimes(1);
	});

	it('observer ignores vote_cast entirely (no fetch)', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockRejectedValue({ code: 'permission_denied' });
		await publishStore.loadColdStart('C1', 'SC1'); // becomes observer
		getPublishedScene.mockClear();
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(getPublishedScene).not.toHaveBeenCalled();
	});

	it('ignores events for a different scene_id', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({ id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 0, no: 0, pending: 5 } });
		await publishStore.loadColdStart('C1', 'SC1');
		getScene.mockClear(); getPublishedScene.mockClear();
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'OTHER') as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(getPublishedScene).not.toHaveBeenCalled();
	});

	it('lifecycle event refetches GetScene (pointer) and updates active attempt', async () => {
		getScene.mockResolvedValueOnce({ activePublishAttemptId: '', publishStatus: '' }); // cold-start: none
		await publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.voteInProgress).toBe(false);
		getScene.mockResolvedValueOnce({ activePublishAttemptId: 'att-9', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({ id: 'att-9', status: 'COLLECTING', voteSummary: { yes: 0, no: 0, pending: 5 } });
		publishStore.onEvent(makeEvent('scene_publish_started', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(publishStore.activeAttemptId).toBe('att-9');
		expect(publishStore.voteInProgress).toBe(true);
	});

	it('participant→observer transition: losing participation mid-vote clears the tally', async () => {
		// Start as a participant with a visible tally.
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValueOnce({ id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 1, no: 0, pending: 4 } });
		await publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.isParticipant).toBe(true);
		expect(publishStore.tally).not.toBeNull();
		// The next refetch is denied (the character lost participation) → tally MUST clear.
		getPublishedScene.mockRejectedValueOnce({ code: 'permission_denied' });
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(publishStore.isParticipant).toBe(false);
		expect(publishStore.tally).toBeNull();
		expect(publishStore.voteInProgress).toBe(true); // still in progress (existence unchanged)
	});

	it('superseded in-flight tally refetch is dropped by the signal guard', async () => {
		// Start as participant.
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValueOnce({
			id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 1, no: 0, pending: 4 },
		});
		await publishStore.loadColdStart('C1', 'SC1');

		// First vote_cast: hold getPublishedScene pending via a deferred promise.
		let resolveFirst!: (v: unknown) => void;
		getPublishedScene.mockImplementationOnce(() => new Promise((r) => { resolveFirst = r; }));

		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300); // debounce fires; refetch #1 starts (pending)

		// Second vote_cast: aborts refetch #1 and starts refetch #2 with a newer tally.
		const newerTally = { yes: 3, no: 1, pending: 1 };
		getPublishedScene.mockResolvedValueOnce({
			id: 'att-1', status: 'COLLECTING', voteSummary: newerTally,
		});
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300); // second debounce fires; aborts AC1; refetch #2 resolves

		// Resolve the stale refetch #1 with bogus data — signal guard must drop it.
		resolveFirst({ id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 99, no: 99, pending: 99 } });
		await vi.advanceTimersByTimeAsync(0);

		expect(publishStore.tally).toEqual(newerTally);
	});

	it('lifecycle attempt-gone aborts a pending tally refetch and clears tally', async () => {
		// Start as participant.
		getScene.mockResolvedValueOnce({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValueOnce({
			id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 1, no: 0, pending: 4 },
		});
		await publishStore.loadColdStart('C1', 'SC1');

		// Hold the next tally refetch pending.
		let resolveStale!: (v: unknown) => void;
		getPublishedScene.mockImplementationOnce(() => new Promise((r) => { resolveStale = r; }));

		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300); // debounce fires; stale refetch starts (pending)

		// Lifecycle event: getScene returns empty → reloadPointer aborts inFlight, clears tally.
		getScene.mockResolvedValueOnce({ activePublishAttemptId: '', publishStatus: '' });
		publishStore.onEvent(makeEvent('scene_publish_resolved', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(0); // flush reloadPointer's async chain

		expect(publishStore.tally).toBeNull();
		expect(publishStore.voteInProgress).toBe(false);

		// Late resolution of the stale refetch must NOT repopulate the tally.
		resolveStale({ id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 1, no: 0, pending: 4 } });
		await vi.advanceTimersByTimeAsync(0);

		expect(publishStore.tally).toBeNull();
	});
});

describe('publishStore caller vote (confirmed + in-flight)', () => {
	beforeEach(() => { vi.useFakeTimers(); });
	afterEach(() => { vi.useRealTimers(); });

	async function asParticipant() {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({
			id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 1, no: 0, pending: 4 },
		});
		await publishStore.loadColdStart('C1', 'SC1');
	}

	it('_markVotePending sets the in-flight ballot dark and locks (castInFlight)', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		expect(publishStore.pendingVote).toBe(true);
		expect(publishStore.castInFlight).toBe(true);
		expect(publishStore.myVote).toBeNull(); // not confirmed yet
	});

	it('ack promotes the in-flight ballot to confirmed and unlocks (brighten)', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		publishStore._ackVote('att-1');
		expect(publishStore.pendingVote).toBeNull(); // promoted
		expect(publishStore.myVote).toBe(true);      // confirmed (bright)
		expect(publishStore.castInFlight).toBe(false); // unlocked
	});

	it('a failed re-cast reverts to the previously confirmed vote, not null', async () => {
		await asParticipant();
		// First cast confirmed: Yes (ack promotes directly).
		publishStore._markVotePending(true);
		publishStore._ackVote('att-1');
		expect(publishStore.myVote).toBe(true);
		// Re-cast No, then it fails → clearVote.
		publishStore._markVotePending(false);
		expect(publishStore.pendingVote).toBe(false);
		publishStore._clearVote('att-1');
		expect(publishStore.pendingVote).toBeNull();
		expect(publishStore.castInFlight).toBe(false);
		expect(publishStore.myVote).toBe(true); // previous confirmed retained
	});

	it('vote state from a prior attempt does not bleed into a new attempt', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		expect(publishStore.pendingVote).toBe(true);
		getScene.mockResolvedValueOnce({ activePublishAttemptId: 'att-2', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValueOnce({ id: 'att-2', status: 'COLLECTING', voteSummary: { yes: 0, no: 0, pending: 5 } });
		publishStore.onEvent({ type: 'core-scenes:scene_publish_started', metadata: { scene_id: 'SC1' } } as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(publishStore.activeAttemptId).toBe('att-2');
		expect(publishStore.pendingVote).toBeNull(); // scoped out by attempt guard
		expect(publishStore.myVote).toBeNull();
		expect(publishStore.castInFlight).toBe(false);
	});

	it('a stale ack/clear from a superseded attempt cannot stomp the newer attempt\'s vote state', async () => {
		await asParticipant(); // active attempt: att-1
		publishStore._markVotePending(true); // cast in flight on att-1
		expect(publishStore.pendingVote).toBe(true);

		// The active attempt moves on to att-2 while att-1's cast is still in flight
		// (e.g. att-1 was withdrawn/resolved and a new attempt started before the
		// RPC for att-1 resolved).
		publishStore._setActiveAttempt('att-2');
		// att-1's vote state is scoped out by the attempt guard on the getters.
		expect(publishStore.myVote).toBeNull();
		expect(publishStore.pendingVote).toBeNull();

		// att-2 now has its own cast in flight, to prove the stale att-1 ack/clear
		// cannot touch it.
		publishStore._markVotePending(false);
		expect(publishStore.pendingVote).toBe(false);

		// The stale att-1 ack fires late — it MUST be a no-op: att-2's pending vote
		// stays untouched and myVote is NOT falsely promoted onto att-2.
		publishStore._ackVote('att-1');
		expect(publishStore.myVote).toBeNull(); // NOT promoted onto att-2
		expect(publishStore.pendingVote).toBe(false); // att-2's pending vote untouched
		expect(publishStore.castInFlight).toBe(true); // still locked for att-2's real cast

		// A stale att-1 clear fires late too — also a no-op.
		publishStore._clearVote('att-1');
		expect(publishStore.pendingVote).toBe(false); // still untouched
		expect(publishStore.castInFlight).toBe(true); // still locked
	});

	it('reset clears all vote state', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		publishStore.reset();
		expect(publishStore.myVote).toBeNull();
		expect(publishStore.pendingVote).toBeNull();
		expect(publishStore.castInFlight).toBe(false);
	});
});
