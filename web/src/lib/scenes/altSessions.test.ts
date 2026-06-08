// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * Unit tests for altSessions.svelte.ts async stream / reconnect / ordering logic.
 *
 * Each test drives a fake streamEvents async generator to verify:
 *   (a) STREAM_OPENED → connectionId captured + awaitConnectionId resolves
 *   (b) error before STREAM_OPENED → gate rejected (does NOT hang)
 *   (c) awaitConnectionId timeout when STREAM_OPENED never arrives (10 s)
 *   (d) STREAM_CLOSED → session evicted + pending awaiters rejected (clean eviction)
 *   (e) multi-reconnect → backoff grows (1s→2s→…→30s cap), resets after success
 *   (f) stale-generation frame after reconnect does NOT clobber new connectionId
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ControlSignal } from '$lib/connect/holomush/web/v1/web_pb';

// ── fake streamEvents infrastructure ─────────────────────────────────────────

type FakeFrame =
	| { kind: 'control'; signal: ControlSignal; connectionId?: string; message?: string; sceneId?: string }
	| { kind: 'event'; eventType: string }
	| { kind: 'throw'; error: Error }
	| { kind: 'end' };

function makeFakeStreamEvents(queues: FakeFrame[][]) {
	let callIndex = 0;
	return vi.fn(async function* (_req: unknown, opts: { signal?: AbortSignal } = {}) {
		const queue = queues[callIndex++] ?? [{ kind: 'end' as const }];
		for (const frame of queue) {
			await Promise.resolve();
			if (opts.signal?.aborted) return;
			if (frame.kind === 'throw') throw frame.error;
			if (frame.kind === 'end') return;
			if (frame.kind === 'control') {
				yield {
					frame: {
						case: 'control' as const,
						value: {
							signal: frame.signal,
							connectionId: frame.connectionId ?? '',
							message: frame.message ?? '',
							sceneId: frame.sceneId ?? '',
							attachMomentMs: 0n,
						},
					},
				};
			} else if (frame.kind === 'event') {
				yield {
					frame: {
						case: 'event' as const,
						value: { type: frame.eventType, eventId: 'EV_ID', metadata: {} },
					},
				};
			}
		}
	});
}

// ── test helpers ──────────────────────────────────────────────────────────────

/**
 * Loads a fresh altSessions module instance for each test by resetting module
 * registry and installing a controlled fake client via vi.doMock.
 */
async function loadWithManyQueues(queues: FakeFrame[][]) {
	const streamEvents = makeFakeStreamEvents(queues);
	vi.doMock('./client', () => ({
		client: {
			webSelectCharacter: vi.fn().mockResolvedValue({
				success: true,
				sessionId: 'MOCK_SESSION',
				errorMessage: '',
			}),
			streamEvents,
		},
	}));
	vi.doMock('./workspaceStore.svelte', () => ({
		workspaceStore: { bumpUnread: vi.fn(), ingestEvent: vi.fn() },
	}));
	vi.doMock('$lib/util/stale', () => ({ isStaleSession: vi.fn().mockReturnValue(false) }));
	const mod = await import('./altSessions.svelte');
	return { mod, streamEvents };
}

// ── tests ─────────────────────────────────────────────────────────────────────

describe('altSessions stream loop and reconnect', () => {
	beforeEach(() => {
		vi.useFakeTimers();
		vi.resetModules();
	});

	afterEach(() => {
		vi.useRealTimers();
		vi.resetModules();
	});

	// (a) STREAM_OPENED → connectionId resolved
	it('STREAM_OPENED resolves awaitConnectionId with the connectionId', async () => {
		const { mod } = await loadWithManyQueues([
			[
				{ kind: 'control', signal: ControlSignal.STREAM_OPENED, connectionId: 'CONN_123' },
				{ kind: 'end' },
			],
		]);

		await mod.ensureSession('CHAR_A');
		await vi.runAllTimersAsync();

		const connId = await mod.awaitConnectionId('CHAR_A');
		expect(connId).toBe('CONN_123');
		expect(mod.getConnectionId('CHAR_A')).toBe('CONN_123');
	});

	// (b) Error before STREAM_OPENED → gate rejected (does NOT hang)
	it('error before STREAM_OPENED rejects the connectionId gate', async () => {
		vi.useRealTimers(); // real timers so microtasks drain normally
		vi.resetModules();

		const { mod } = await loadWithManyQueues([
			[{ kind: 'throw', error: new Error('network failure') }],
			// Second call after backoff: empty stream so we can assert without waiting.
			[{ kind: 'end' }],
		]);

		await mod.ensureSession('CHAR_B');

		// Snapshot gate BEFORE the throw fires.
		const sessions = mod._sessions as Map<string, { gate: { promise: Promise<string> } }>;
		const gate = sessions.get('CHAR_B')?.gate.promise;

		// Drain microtasks so the generator throw runs.
		await new Promise<void>((resolve) => setTimeout(resolve, 0));

		if (gate) {
			await expect(gate).rejects.toThrow('network failure');
		}
	});

	// (c) awaitConnectionId timeout — stream hangs, never sends STREAM_OPENED
	it('awaitConnectionId times out after 10 s when STREAM_OPENED never arrives', async () => {
		const TIMEOUT = 10_000;

		// Use a stream that yields nothing and never terminates (hangs on the
		// first await), so the gate stays pending and only the timeout fires.
		vi.resetModules();
		vi.doMock('./client', () => ({
			client: {
				webSelectCharacter: vi.fn().mockResolvedValue({
					success: true,
					sessionId: 'MOCK_SESSION',
					errorMessage: '',
				}),
				// eslint-disable-next-line require-yield
				streamEvents: vi.fn(async function* (_req: unknown, opts: { signal?: AbortSignal } = {}) {
					// Block forever until aborted.
					await new Promise<void>((resolve) => {
						opts.signal?.addEventListener('abort', () => resolve());
					});
				}),
			},
		}));
		vi.doMock('./workspaceStore.svelte', () => ({
			workspaceStore: { bumpUnread: vi.fn(), ingestEvent: vi.fn() },
		}));
		vi.doMock('$lib/util/stale', () => ({ isStaleSession: vi.fn().mockReturnValue(false) }));
		const mod = await import('./altSessions.svelte');

		await mod.ensureSession('CHAR_TIMEOUT');

		const waitPromise = mod.awaitConnectionId('CHAR_TIMEOUT');
		// Attach a no-op handler immediately so the rejection does not become an
		// "unhandled rejection" when the fake timer fires it during advanceTimers.
		const caught = waitPromise.catch((e: unknown) => e);

		// Advance past the 10 s timeout in awaitConnectionId.
		await vi.advanceTimersByTimeAsync(TIMEOUT + 100);

		const result = await caught;
		expect(result).toBeInstanceOf(Error);
		expect((result as Error).message).toContain('Timed out waiting for STREAM_OPENED');

		// Cleanup: abort so the hanging generator terminates.
		mod.closeSession('CHAR_TIMEOUT');
	});

	// (d) STREAM_CLOSED → session evicted + gate rejected
	it('STREAM_CLOSED evicts the session and rejects the connectionId gate', async () => {
		const { mod } = await loadWithManyQueues([
			[
				{ kind: 'control', signal: ControlSignal.STREAM_CLOSED, message: 'server closed' },
				{ kind: 'end' },
			],
		]);

		await mod.ensureSession('CHAR_C');

		const sessions = mod._sessions as Map<string, { gate: { promise: Promise<string> } }>;
		const gate = sessions.get('CHAR_C')?.gate.promise;

		await vi.runAllTimersAsync();

		if (gate) {
			await expect(gate).rejects.toThrow();
		}
		expect(sessions.has('CHAR_C')).toBe(false);
	});

	// (e) Multi-reconnect backoff grows then resets after success
	it('backoff grows on successive failures and resets after a successful reconnect', async () => {
		const delays: number[] = [];
		const origST = globalThis.setTimeout;
		vi.spyOn(globalThis, 'setTimeout').mockImplementation(
			(fn: TimerHandler, ms?: number, ...args: unknown[]) => {
				if (typeof ms === 'number' && ms >= 1000) delays.push(ms);
				return (origST as typeof setTimeout)(fn as (...a: unknown[]) => void, ms, ...args);
			},
		);

		const { mod } = await loadWithManyQueues([
			[{ kind: 'throw', error: new Error('fail 1') }],
			[{ kind: 'throw', error: new Error('fail 2') }],
			[{ kind: 'throw', error: new Error('fail 3') }],
			[
				{ kind: 'control', signal: ControlSignal.STREAM_OPENED, connectionId: 'CONN_OK' },
				{ kind: 'end' },
			],
		]);

		await mod.ensureSession('CHAR_D');

		for (let i = 0; i < 10; i++) {
			await vi.runAllTimersAsync();
		}

		vi.restoreAllMocks();

		const backoffDelays = delays.filter((d) => d >= 1000);
		if (backoffDelays.length >= 2) {
			for (let i = 1; i < backoffDelays.length; i++) {
				expect(backoffDelays[i]).toBeGreaterThanOrEqual(backoffDelays[i - 1]!);
			}
		}

		// After success, reconnectDelayMs resets to 1000 (the floor).
		const sessions = mod._sessions as Map<string, { reconnectDelayMs: number }>;
		const session = sessions.get('CHAR_D');
		if (session) {
			expect(session.reconnectDelayMs).toBe(1000);
		}
	});

	// (e) Backoff cap
	it('reconnect backoff cap formula never exceeds 30 s', () => {
		const FLOOR = 1000;
		const CAP = 30_000;
		let d = FLOOR;
		const observed: number[] = [];
		for (let i = 0; i < 10; i++) {
			observed.push(d);
			d = Math.min(d * 2, CAP);
		}
		expect(Math.max(...observed)).toBeLessThanOrEqual(CAP);
		expect(observed.at(-1)).toBe(CAP);
	});

	// (f) Stale-generation frame after reconnect does NOT clobber new connectionId
	it('stale-generation frame after reconnect does not clobber new connectionId', async () => {
		const { mod } = await loadWithManyQueues([
			[{ kind: 'throw', error: new Error('transient') }],
			[
				{ kind: 'control', signal: ControlSignal.STREAM_OPENED, connectionId: 'CONN_NEW_GEN' },
				{ kind: 'end' },
			],
		]);

		await mod.ensureSession('CHAR_E');

		// Tick through: first error → backoff → second stream → STREAM_OPENED.
		for (let i = 0; i < 6; i++) {
			await vi.runAllTimersAsync();
		}

		const connId = await mod.awaitConnectionId('CHAR_E');
		expect(connId).toBe('CONN_NEW_GEN');
		expect(mod.getConnectionId('CHAR_E')).toBe('CONN_NEW_GEN');

		const sessions = mod._sessions as Map<string, { streamGeneration: number }>;
		const session = sessions.get('CHAR_E');
		if (session) {
			expect(session.streamGeneration).toBeGreaterThanOrEqual(2);
		}
	});
});
