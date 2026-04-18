// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { Client } from '@connectrpc/connect';
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
		const at = typeof a.timestamp === 'bigint' ? a.timestamp : BigInt(a.timestamp ?? 0);
		const bt = typeof b.timestamp === 'bigint' ? b.timestamp : BigInt(b.timestamp ?? 0);
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

async function fetchOneStream(
	client: Client<typeof WebService>,
	sessionId: string,
	stream: string,
	count: number,
	signal?: AbortSignal,
): Promise<FetchResult> {
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
		return { ok: false, error: e };
	}
}
