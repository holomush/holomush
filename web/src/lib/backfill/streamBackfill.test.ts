// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ConnectError, Code } from '@connectrpc/connect';
import {
	backfillStreams,
	backfillPage,
	CursorStaleError,
	CursorLagError,
	CursorInvalidError,
} from './streamBackfill';

function makeClient() {
	return {
		webQueryStreamHistory: vi.fn(),
	};
}

// ---------------------------------------------------------------------------
// backfillStreams (multi-stream, legacy API)
// ---------------------------------------------------------------------------

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
			nextCursor: new Uint8Array(),
		});

		const result = await backfillStreams(client as never, 'sess-1', ['location.l1']);
		expect(result.events.map((e) => e.eventId)).toEqual(['e-a', 'e-b']);
		expect(result.failedStreams).toEqual([]);
		expect(client.webQueryStreamHistory).toHaveBeenCalledWith(
			{
				sessionId: 'sess-1',
				stream: 'location.l1',
				count: 150,
				cursor: new Uint8Array(),
				notBeforeMs: 0n,
				notAfterMs: 0n,
			},
			expect.anything(),
		);
	});

	it('merges events from multiple streams in ascending (timestamp, eventId) order', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockImplementation((req: { stream: string }) => {
			if (req.stream === 'character.c1') {
				return Promise.resolve({
					events: [{ eventId: 'e-mid', timestamp: 100n, type: 'say' }],
					hasMore: false,
					nextCursor: new Uint8Array(),
				});
			}
			return Promise.resolve({
				events: [
					{ eventId: 'e-first', timestamp: 50n, type: 'say' },
					{ eventId: 'e-last', timestamp: 200n, type: 'say' },
				],
				hasMore: false,
				nextCursor: new Uint8Array(),
			});
		});

		const result = await backfillStreams(client as never, 'sess-1', [
			'character.c1',
			'location.l1',
		]);
		expect(result.events.map((e) => e.eventId)).toEqual(['e-first', 'e-mid', 'e-last']);
	});

	it('uses eventId as tiebreaker for same-timestamp events', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockImplementation((req: { stream: string }) => {
			if (req.stream === 'character.c1') {
				return Promise.resolve({
					events: [{ eventId: 'e-beta', timestamp: 100n, type: 'say' }],
					hasMore: false,
					nextCursor: new Uint8Array(),
				});
			}
			return Promise.resolve({
				events: [{ eventId: 'e-alpha', timestamp: 100n, type: 'say' }],
				hasMore: false,
				nextCursor: new Uint8Array(),
			});
		});

		const result = await backfillStreams(client as never, 'sess-1', [
			'character.c1',
			'location.l1',
		]);
		expect(result.events.map((e) => e.eventId)).toEqual(['e-alpha', 'e-beta']);
	});

	it('retries once on transient error, then succeeds', async () => {
		const client = makeClient();
		const transient = new ConnectError('network', Code.Unavailable);
		client.webQueryStreamHistory.mockRejectedValueOnce(transient).mockResolvedValueOnce({
			events: [{ eventId: 'e-x', timestamp: 10n, type: 'say' }],
			hasMore: false,
			nextCursor: new Uint8Array(),
		});

		const result = await backfillStreams(client as never, 'sess-1', ['location.l1'], {
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

		const result = await backfillStreams(client as never, 'sess-1', ['location.l1']);
		expect(result.events).toEqual([]);
		expect(result.failedStreams).toEqual(['location.l1']);
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
			const result = await backfillStreams(client as never, 'sess-1', ['location.l1']);
			expect(result.failedStreams).toEqual(['location.l1']);
			expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(1);
		}
	});

	it('deduplicates events appearing in multiple streams by eventId', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockImplementation((req: { stream: string }) => {
			if (req.stream === 'character.c1') {
				return Promise.resolve({
					events: [{ eventId: 'dup', timestamp: 100n, type: 'say' }],
					hasMore: false,
					nextCursor: new Uint8Array(),
				});
			}
			return Promise.resolve({
				events: [{ eventId: 'dup', timestamp: 100n, type: 'say' }],
				hasMore: false,
				nextCursor: new Uint8Array(),
			});
		});

		const result = await backfillStreams(client as never, 'sess-1', [
			'character.c1',
			'location.l1',
		]);
		expect(result.events.map((e) => e.eventId)).toEqual(['dup']);
	});

	// holomush-iu8j: backfillStreams MUST forward opts.notAfterMs to the
	// per-stream RPC so backfill is scoped to the Subscribe attach moment.
	// Without this, the cursor-bounded backfill fix has no effect on the
	// client side — backfill calls would land at the gateway with
	// notAfterMs=0 and the server would treat the query as unbounded,
	// re-opening the holomush-fujt connect-time replay/backfill race.
	it('forwards opts.notAfterMs to the per-stream RPC (iu8j)', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockResolvedValueOnce({
			events: [],
			hasMore: false,
			nextCursor: new Uint8Array(),
		});

		const attachMoment = 1700000999999n; // arbitrary epoch-ms
		await backfillStreams(client as never, 'sess-1', ['location.l1'], {
			notAfterMs: attachMoment,
		});

		expect(client.webQueryStreamHistory).toHaveBeenCalledWith(
			expect.objectContaining({ notAfterMs: attachMoment }),
			expect.anything(),
		);
	});

	// Boundary: opts.notAfterMs omitted MUST behave as "no upper bound"
	// (preserves back-compat with callers that don't yet pass the
	// attach moment).
	it('omits notAfterMs → defaults to 0n (no upper bound, back-compat)', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockResolvedValueOnce({
			events: [],
			hasMore: false,
			nextCursor: new Uint8Array(),
		});

		await backfillStreams(client as never, 'sess-1', ['location.l1']);

		expect(client.webQueryStreamHistory).toHaveBeenCalledWith(
			expect.objectContaining({ notAfterMs: 0n }),
			expect.anything(),
		);
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
		const promise = backfillStreams(client as never, 'sess-1', ['location.l1'], {
			signal: controller.signal,
		});
		controller.abort();
		await expect(promise).rejects.toThrow();
	});
});

// ---------------------------------------------------------------------------
// backfillPage (single stream, opaque cursor, LAG/STALE/INVALID)
// ---------------------------------------------------------------------------

describe('backfillPage', () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it('passes cursor bytes through to the RPC request', async () => {
		const client = makeClient();
		const cursorBytes = new Uint8Array([1, 2, 3, 4]);
		client.webQueryStreamHistory.mockResolvedValueOnce({
			events: [{ eventId: 'e-1', timestamp: 10n }],
			hasMore: false,
			nextCursor: new Uint8Array(),
		});

		await backfillPage(client as never, {
			sessionId: 'sess-1',
			stream: 'location.l1',
			count: 50,
			cursor: cursorBytes,
		});

		expect(client.webQueryStreamHistory).toHaveBeenCalledWith({
			sessionId: 'sess-1',
			stream: 'location.l1',
			count: 50,
			cursor: cursorBytes,
			notBeforeMs: 0n,
		});
	});

	it('sends empty cursor when cursor is undefined', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockResolvedValueOnce({
			events: [],
			hasMore: false,
			nextCursor: new Uint8Array(),
		});

		await backfillPage(client as never, {
			sessionId: 'sess-1',
			stream: 'location.l1',
			count: 50,
		});

		expect(client.webQueryStreamHistory).toHaveBeenCalledWith(
			expect.objectContaining({ cursor: new Uint8Array() }),
		);
	});

	it('returns nextCursor from the response for pagination chaining', async () => {
		const client = makeClient();
		const nextCursorBytes = new Uint8Array([10, 20, 30]);
		client.webQueryStreamHistory.mockResolvedValueOnce({
			events: [{ eventId: 'e-1', timestamp: 10n }],
			hasMore: true,
			nextCursor: nextCursorBytes,
		});

		const resp = await backfillPage(client as never, {
			sessionId: 'sess-1',
			stream: 'location.l1',
			count: 50,
		});

		expect(resp.hasMore).toBe(true);
		expect(resp.nextCursor).toEqual(nextCursorBytes);
	});

	it('passes nextCursor as cursor on subsequent page request', async () => {
		const client = makeClient();
		const page1Cursor = new Uint8Array([10, 20, 30]);

		client.webQueryStreamHistory
			.mockResolvedValueOnce({
				events: [{ eventId: 'e-1', timestamp: 10n }],
				hasMore: true,
				nextCursor: page1Cursor,
			})
			.mockResolvedValueOnce({
				events: [{ eventId: 'e-2', timestamp: 20n }],
				hasMore: false,
				nextCursor: new Uint8Array(),
			});

		const page1 = await backfillPage(client as never, {
			sessionId: 'sess-1',
			stream: 'location.l1',
			count: 50,
		});
		const page2 = await backfillPage(client as never, {
			sessionId: 'sess-1',
			stream: 'location.l1',
			count: 50,
			cursor: page1.nextCursor,
		});

		expect(client.webQueryStreamHistory).toHaveBeenNthCalledWith(
			2,
			expect.objectContaining({ cursor: page1Cursor }),
		);
		expect(page2.events.map((e) => e.eventId)).toEqual(['e-2']);
	});

	it('throws CursorStaleError on FAILED_PRECONDITION without retrying', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockRejectedValueOnce(
			new ConnectError('cursor is stale', Code.FailedPrecondition),
		);

		await expect(
			backfillPage(client as never, {
				sessionId: 'sess-1',
				stream: 'location.l1',
				count: 50,
				cursor: new Uint8Array([1, 2, 3]),
			}),
		).rejects.toBeInstanceOf(CursorStaleError);

		expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(1);
	});

	it('throws CursorInvalidError on INVALID_ARGUMENT without retrying', async () => {
		const client = makeClient();
		client.webQueryStreamHistory.mockRejectedValueOnce(
			new ConnectError('bad cursor', Code.InvalidArgument),
		);

		await expect(
			backfillPage(client as never, {
				sessionId: 'sess-1',
				stream: 'location.l1',
				count: 50,
			}),
		).rejects.toBeInstanceOf(CursorInvalidError);

		expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(1);
	});

	it('retries with backoff on UNAVAILABLE and succeeds before exhausting retries', async () => {
		const client = makeClient();
		const lag = new ConnectError('lagging', Code.Unavailable);

		// Fail twice then succeed.
		client.webQueryStreamHistory
			.mockRejectedValueOnce(lag)
			.mockRejectedValueOnce(lag)
			.mockResolvedValueOnce({
				events: [{ eventId: 'e-ok', timestamp: 5n }],
				hasMore: false,
				nextCursor: new Uint8Array(),
			});

		const promise = backfillPage(client as never, {
			sessionId: 'sess-1',
			stream: 'location.l1',
			count: 50,
		});

		// Advance through the first two backoff delays (250ms + 500ms).
		await vi.advanceTimersByTimeAsync(250);
		await vi.advanceTimersByTimeAsync(500);

		const resp = await promise;
		expect(resp.events.map((e) => e.eventId)).toEqual(['e-ok']);
		expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(3);
	});

	it('throws CursorLagError after all five retries exhausted on UNAVAILABLE', async () => {
		const client = makeClient();
		const lag = new ConnectError('lagging', Code.Unavailable);

		// Fail 6 times (1 initial + 5 retries).
		client.webQueryStreamHistory.mockRejectedValue(lag);

		// Attach rejection handler immediately to avoid unhandled rejection.
		const promise = backfillPage(client as never, {
			sessionId: 'sess-1',
			stream: 'location.l1',
			count: 50,
		});
		const assertion = expect(promise).rejects.toBeInstanceOf(CursorLagError);

		// Advance through all 5 backoff intervals: 250+500+1000+2000+4000 = 7750ms.
		await vi.advanceTimersByTimeAsync(250);
		await vi.advanceTimersByTimeAsync(500);
		await vi.advanceTimersByTimeAsync(1000);
		await vi.advanceTimersByTimeAsync(2000);
		await vi.advanceTimersByTimeAsync(4000);

		await assertion;
		// 1 initial + 5 retries = 6 attempts.
		expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(6);
	});

	it('re-throws unknown non-Connect errors without wrapping', async () => {
		const client = makeClient();
		const unknownErr = new TypeError('something exploded');
		client.webQueryStreamHistory.mockRejectedValueOnce(unknownErr);

		await expect(
			backfillPage(client as never, {
				sessionId: 'sess-1',
				stream: 'location.l1',
				count: 50,
			}),
		).rejects.toBeInstanceOf(TypeError);
	});
});
