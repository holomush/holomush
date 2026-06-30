import { describe, it, expect, vi, beforeEach } from 'vitest';

const { getScene, getPublishedScene } = vi.hoisted(() => ({ getScene: vi.fn(), getPublishedScene: vi.fn() }));
vi.mock('./client', () => ({ getScene, getPublishedScene }));
vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'SESS') }));

import { publishStore } from './publishStore.svelte';

beforeEach(() => {
	vi.clearAllMocks();
	publishStore.reset();
});

import { afterEach } from 'vitest';

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
});
