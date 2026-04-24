// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { Client } from '@connectrpc/connect';
import { ConnectError, Code } from '@connectrpc/connect';
import type { WebService, GameEvent } from '$lib/connect/holomush/web/v1/web_pb';

// ---------------------------------------------------------------------------
// Error types for cursor lifecycle
// ---------------------------------------------------------------------------

/** Server returned FAILED_PRECONDITION: cursor is stale. Drop it and re-query. */
export class CursorStaleError extends Error {
	override readonly name = 'CursorStaleError' as const;
	constructor(message: string) {
		super(message);
	}
}

/**
 * Server returned UNAVAILABLE after all retries: cold tier is lagging.
 * The cursor is still valid; surface this to the user but do NOT drop it.
 */
export class CursorLagError extends Error {
	override readonly name = 'CursorLagError' as const;
	constructor(message: string) {
		super(message);
	}
}

/** Server returned INVALID_ARGUMENT: programming error in cursor construction. */
export class CursorInvalidError extends Error {
	override readonly name = 'CursorInvalidError' as const;
	constructor(message: string) {
		super(message);
	}
}

// ---------------------------------------------------------------------------
// Backfill result (multi-stream, used by backfillStreams)
// ---------------------------------------------------------------------------

export interface BackfillResult {
	events: GameEvent[];
	failedStreams: string[];
}

export interface BackfillOpts {
	count?: number;
	signal?: AbortSignal;
}

// ---------------------------------------------------------------------------
// Single-page request/response (used by backfillPage and callers that need
// opaque cursors + pagination)
// ---------------------------------------------------------------------------

export interface BackfillRequest {
	sessionId: string;
	stream: string;
	count: number;
	/** Opaque cursor from a previous response. Omit or pass undefined to start from latest. */
	cursor?: Uint8Array;
	notBeforeMs?: bigint;
}

export interface BackfillResponse {
	events: GameEvent[];
	hasMore: boolean;
	/** Empty when has_more is false. Persist this to resume pagination. */
	nextCursor: Uint8Array;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const DEFAULT_COUNT = 150;

/** Backoff schedule for UNAVAILABLE (LAG) retries, per spec §4.4. */
const LAG_BACKOFF_MS = [250, 500, 1000, 2000, 4000] as const;

// ---------------------------------------------------------------------------
// backfillPage — single stream, single page, opaque cursor, with LAG/STALE
// ---------------------------------------------------------------------------

/**
 * Fetch one page of history for a single stream.
 *
 * - On UNAVAILABLE (LAG): retries with the spec §4.4 schedule (250/500/1000/2000/4000ms).
 *   After all retries exhausted, throws CursorLagError. The cursor is still valid.
 * - On FAILED_PRECONDITION (STALE): throws CursorStaleError. Caller must drop cursor.
 * - On INVALID_ARGUMENT: throws CursorInvalidError. Programming error.
 */
export async function backfillPage(
	client: Client<typeof WebService>,
	req: BackfillRequest,
): Promise<BackfillResponse> {
	// attempt=0 is the initial try; LAG_BACKOFF_MS.length retries follow.
	// Total attempts = 1 + LAG_BACKOFF_MS.length (≈6) for ≈7.75s total backoff.
	// The final iteration throws CursorLagError rather than sleeping — the
	// cursor remains valid; the caller decides what to surface to the user.
	for (let attempt = 0; attempt <= LAG_BACKOFF_MS.length; attempt++) {
		try {
			const resp = await client.webQueryStreamHistory({
				sessionId: req.sessionId,
				stream: req.stream,
				count: req.count,
				cursor: req.cursor ?? new Uint8Array(),
				notBeforeMs: req.notBeforeMs ?? 0n,
			});
			return {
				events: resp.events,
				hasMore: resp.hasMore,
				nextCursor: resp.nextCursor,
			};
		} catch (err) {
			if (err instanceof ConnectError) {
				switch (err.code) {
					case Code.Unavailable:
						if (attempt < LAG_BACKOFF_MS.length) {
							await sleep(LAG_BACKOFF_MS[attempt]);
							continue;
						}
						throw new CursorLagError(err.message);
					case Code.FailedPrecondition:
						throw new CursorStaleError(err.message);
					case Code.InvalidArgument:
						throw new CursorInvalidError(err.message);
				}
			}
			throw err;
		}
	}
	// Unreachable: loop exits via return or throw.
	throw new CursorLagError('unreachable');
}

// ---------------------------------------------------------------------------
// backfillStreams — multi-stream fan-out, merges + deduplicates results.
// This is the higher-level API used by the terminal page.
// ---------------------------------------------------------------------------

/**
 * Backfill the most recent events for a set of streams in parallel, merging
 * results in ascending (timestamp, eventId) order with cross-stream dedup.
 *
 * Uses opaque cursor bytes. The legacy `beforeId` field is no longer sent.
 */
export async function backfillStreams(
	client: Client<typeof WebService>,
	sessionId: string,
	streams: string[],
	opts: BackfillOpts = {},
): Promise<BackfillResult> {
	if (streams.length === 0) {
		return { events: [], failedStreams: [] };
	}
	const count = opts.count ?? DEFAULT_COUNT;

	const results = await Promise.all(
		streams.map((stream) => fetchOneStream(client, sessionId, stream, count, opts.signal)),
	);

	if (opts.signal?.aborted) {
		throw opts.signal.reason;
	}

	const events: GameEvent[] = [];
	const failedStreams: string[] = [];
	for (let i = 0; i < results.length; i++) {
		const r = results[i];
		if (r.ok) {
			events.push(...r.events);
		} else {
			failedStreams.push(streams[i]);
		}
	}

	events.sort((a, b) => {
		const at = a.timestamp;
		const bt = b.timestamp;
		if (at < bt) return -1;
		if (at > bt) return 1;
		const aid = a.eventId ?? '';
		const bid = b.eventId ?? '';
		if (aid < bid) return -1;
		if (aid > bid) return 1;
		return 0;
	});

	// Deduplicate events that appeared in multiple streams (e.g. a `say` is
	// present on both character and location streams). Keep the first
	// occurrence after the stable sort above so ordering is preserved.
	const seenIds = new Set<string>();
	const dedupedEvents: GameEvent[] = [];
	for (const ev of events) {
		if (ev.eventId) {
			if (seenIds.has(ev.eventId)) continue;
			seenIds.add(ev.eventId);
		}
		dedupedEvents.push(ev);
	}

	return { events: dedupedEvents, failedStreams };
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

type FetchResult = { ok: true; events: GameEvent[] } | { ok: false; error: unknown };

const RETRY_DELAY_MS = 500;

function isRetryable(e: unknown): boolean {
	if (!(e instanceof ConnectError)) return true; // non-Connect errors: treat as transient
	switch (e.code) {
		case Code.Unavailable:
		case Code.DeadlineExceeded:
		case Code.Internal:
		case Code.Unknown:
			return true;
		default:
			return false;
	}
}

async function fetchOneStream(
	client: Client<typeof WebService>,
	sessionId: string,
	stream: string,
	count: number,
	signal?: AbortSignal,
): Promise<FetchResult> {
	for (let attempt = 0; ; attempt++) {
		try {
			const resp = await client.webQueryStreamHistory(
				{
					sessionId,
					stream,
					count,
					cursor: new Uint8Array(),
					notBeforeMs: 0n,
				},
				{ signal },
			);
			return { ok: true, events: resp.events };
		} catch (e) {
			if (signal?.aborted) {
				return { ok: false, error: e };
			}
			if (attempt >= 1 || !isRetryable(e)) {
				return { ok: false, error: e };
			}
			await sleep(RETRY_DELAY_MS, signal);
		}
	}
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
	return new Promise((resolve, reject) => {
		const onAbort = () => {
			clearTimeout(t);
			reject(signal?.reason);
		};
		const t = setTimeout(() => {
			signal?.removeEventListener('abort', onAbort);
			resolve();
		}, ms);
		signal?.addEventListener('abort', onAbort, { once: true });
	});
}
