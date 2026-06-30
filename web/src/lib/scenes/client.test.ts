import { describe, it, expect, vi, beforeEach } from 'vitest';

// vi.hoisted() ensures these are available when vi.mock factories run (hoisted above imports).
const { webStartScenePublish, webCastPublishSceneVote, webWithdrawScenePublish, webGetPublishedScene } =
	vi.hoisted(() => ({
		webStartScenePublish: vi.fn(),
		webCastPublishSceneVote: vi.fn(),
		webWithdrawScenePublish: vi.fn(),
		webGetPublishedScene: vi.fn(),
	}));

vi.mock('@connectrpc/connect', () => ({
	createClient: () => ({
		webStartScenePublish,
		webCastPublishSceneVote,
		webWithdrawScenePublish,
		webGetPublishedScene,
	}),
}));
vi.mock('$lib/transport', () => ({ transport: {} }));

import {
	startScenePublish,
	castPublishSceneVote,
	withdrawScenePublish,
	getPublishedScene,
} from './client';

beforeEach(() => vi.clearAllMocks());

describe('publish client wrappers', () => {
	it('startScenePublish sends sessionId/characterId/sceneId and returns the response', async () => {
		webStartScenePublish.mockResolvedValue({ publishedSceneId: 'att-1', attemptNumber: 1 });
		const res = await startScenePublish('S1', { characterId: 'C1', sceneId: 'SC1' });
		expect(webStartScenePublish).toHaveBeenCalledWith({ sessionId: 'S1', characterId: 'C1', sceneId: 'SC1' });
		expect(res.publishedSceneId).toBe('att-1');
	});

	it('castPublishSceneVote forwards the boolean vote', async () => {
		webCastPublishSceneVote.mockResolvedValue({});
		await castPublishSceneVote('S1', { characterId: 'C1', publishedSceneId: 'att-1', vote: true });
		expect(webCastPublishSceneVote).toHaveBeenCalledWith({
			sessionId: 'S1', characterId: 'C1', publishedSceneId: 'att-1', vote: true,
		});
	});

	it('withdrawScenePublish forwards the attempt id', async () => {
		webWithdrawScenePublish.mockResolvedValue({});
		await withdrawScenePublish('S1', { characterId: 'C1', publishedSceneId: 'att-1' });
		expect(webWithdrawScenePublish).toHaveBeenCalledWith({
			sessionId: 'S1', characterId: 'C1', publishedSceneId: 'att-1',
		});
	});

	it('getPublishedScene returns the full response (with voteSummary)', async () => {
		webGetPublishedScene.mockResolvedValue({
			id: 'att-1', sceneId: 'SC1', attemptNumber: 1, status: 'COLLECTING',
			failureReason: '', voteSummary: { yes: 2, no: 0, pending: 3 },
		});
		const res = await getPublishedScene('S1', { characterId: 'C1', publishedSceneId: 'att-1' });
		expect(res.status).toBe('COLLECTING');
		expect(res.voteSummary?.yes).toBe(2);
	});
});
