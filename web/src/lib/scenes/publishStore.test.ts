import { describe, it, expect, vi, beforeEach } from 'vitest';

const { getScene, getPublishedScene } = vi.hoisted(() => ({ getScene: vi.fn(), getPublishedScene: vi.fn() }));
vi.mock('./client', () => ({ getScene, getPublishedScene }));
vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'SESS') }));

import { publishStore } from './publishStore.svelte';

beforeEach(() => {
	vi.clearAllMocks();
	publishStore.reset();
});

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
