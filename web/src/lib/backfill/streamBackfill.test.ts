// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi } from 'vitest';
import { ConnectError, Code } from '@connectrpc/connect';
import { backfillStreams } from './streamBackfill';

function makeClient() {
	return {
		webQueryStreamHistory: vi.fn(),
	};
}

describe('backfillStreams', () => {
	it('returns empty result for empty stream list without making RPC calls', async () => {
		const client = makeClient();
		const result = await backfillStreams(client as never, 'sess-1', []);
		expect(result.events).toEqual([]);
		expect(result.failedStreams).toEqual([]);
		expect(client.webQueryStreamHistory).not.toHaveBeenCalled();
	});

	it('fetches a single stream and returns its events', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockResolvedValueOnce({
			events: [
				{ eventId: 'e-a', timestamp: 100n, type: 'say' },
				{ eventId: 'e-b', timestamp: 200n, type: 'say' },
			],
			hasMore: false,
		});

		const result = await backfillStreams(client as never, 'sess-1', ['location:l1']);
		expect(result.events.map((e) => e.eventId)).toEqual(['e-a', 'e-b']);
		expect(result.failedStreams).toEqual([]);
		expect(client.webQueryStreamHistory).toHaveBeenCalledWith(
			{ sessionId: 'sess-1', stream: 'location:l1', count: 150, beforeId: '', notBeforeMs: 0n },
			expect.anything(),
		);
	});

	it('merges events from multiple streams in ascending (timestamp, eventId) order', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockImplementation((req: { stream: string }) => {
			if (req.stream === 'character:c1') {
				return Promise.resolve({
					events: [{ eventId: 'e-mid', timestamp: 100n, type: 'say' }],
					hasMore: false,
				});
			}
			return Promise.resolve({
				events: [
					{ eventId: 'e-first', timestamp: 50n, type: 'say' },
					{ eventId: 'e-last', timestamp: 200n, type: 'say' },
				],
				hasMore: false,
			});
		});

		const result = await backfillStreams(client as never, 'sess-1', [
			'character:c1',
			'location:l1',
		]);
		expect(result.events.map((e) => e.eventId)).toEqual(['e-first', 'e-mid', 'e-last']);
	});

	it('uses eventId as tiebreaker for same-timestamp events', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockImplementation((req: { stream: string }) => {
			if (req.stream === 'character:c1') {
				return Promise.resolve({
					events: [{ eventId: 'e-beta', timestamp: 100n, type: 'say' }],
					hasMore: false,
				});
			}
			return Promise.resolve({
				events: [{ eventId: 'e-alpha', timestamp: 100n, type: 'say' }],
				hasMore: false,
			});
		});

		const result = await backfillStreams(client as never, 'sess-1', [
			'character:c1',
			'location:l1',
		]);
		expect(result.events.map((e) => e.eventId)).toEqual(['e-alpha', 'e-beta']);
	});

	it('retries once on transient error, then succeeds', async () => {
		const client = makeClient();
		const transient = new ConnectError('network', Code.Unavailable);
		client.webQueryStreamHistory.mockRejectedValueOnce(transient).mockResolvedValueOnce({
			events: [{ eventId: 'e-x', timestamp: 10n, type: 'say' }],
			hasMore: false,
		});

		const result = await backfillStreams(client as never, 'sess-1', ['location:l1'], {
			count: 150,
		});
		expect(result.events.map((e) => e.eventId)).toEqual(['e-x']);
		expect(result.failedStreams).toEqual([]);
		expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(2);
	});

	it('records failed stream when transient error persists after retry', async () => {
		const client = makeClient();
		const transient = new ConnectError('network', Code.Unavailable);
		client.webQueryStreamHistory.mockRejectedValueOnce(transient).mockRejectedValueOnce(transient);

		const result = await backfillStreams(client as never, 'sess-1', ['location:l1']);
		expect(result.events).toEqual([]);
		expect(result.failedStreams).toEqual(['location:l1']);
		expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(2);
	});

	it('does NOT retry on permanent errors (PermissionDenied, NotFound, InvalidArgument)', async () => {
		const permanents = [
			new ConnectError('denied', Code.PermissionDenied),
			new ConnectError('missing', Code.NotFound),
			new ConnectError('bad', Code.InvalidArgument),
		];
		for (const err of permanents) {
			const client = makeClient();
			client.webQueryStreamHistory.mockRejectedValueOnce(err);
			const result = await backfillStreams(client as never, 'sess-1', ['location:l1']);
			expect(result.failedStreams).toEqual(['location:l1']);
			expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(1);
		}
	});

	it('rejects with abort reason when AbortSignal is triggered mid-flight', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockImplementation(
			(_req: unknown, opts?: { signal?: AbortSignal }) =>
				new Promise((_resolve, reject) => {
					opts?.signal?.addEventListener('abort', () => {
						reject(new DOMException('aborted', 'AbortError'));
					});
				}),
		);

		const controller = new AbortController();
		const promise = backfillStreams(client as never, 'sess-1', ['location:l1'], {
			signal: controller.signal,
		});
		controller.abort();
		await expect(promise).rejects.toThrow();
	});
});
