// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { Client } from '@connectrpc/connect';
import { ConnectError, Code } from '@connectrpc/connect';
import type { WebService, GameEvent } from '$lib/connect/holomush/web/v1/web_pb';

export interface BackfillResult {
	events: GameEvent[];
	failedStreams: string[];
}

export interface BackfillOpts {
	count?: number;
	signal?: AbortSignal;
}

const DEFAULT_COUNT = 150;

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

	return { events, failedStreams };
}

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
					beforeId: '',
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
