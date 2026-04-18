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

export async function backfillStreams(
	// eslint-disable-next-line @typescript-eslint/no-unused-vars
	client: Client<typeof WebService>,
	// eslint-disable-next-line @typescript-eslint/no-unused-vars
	sessionId: string,
	streams: string[],
	// eslint-disable-next-line @typescript-eslint/no-unused-vars
	opts: BackfillOpts = {},
): Promise<BackfillResult> {
	if (streams.length === 0) {
		return { events: [], failedStreams: [] };
	}
	// Full implementation added in subsequent tasks.
	throw new Error('not implemented');
}
