// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi } from 'vitest';
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
});
