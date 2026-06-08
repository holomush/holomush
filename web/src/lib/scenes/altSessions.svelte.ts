// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * altSessions manages per-character "alt" game sessions for the workspace.
 * Each alt runs a separate StreamEvents loop distinct from the main terminal
 * session, allowing the workspace to receive SCENE_ACTIVITY notifications
 * and live IC events independently of the terminal's stream.
 *
 * Stream loop mirrors the terminal page's hydrateAndStream pattern:
 *   STREAM_OPENED → capture connectionId (before replayComplete)
 *   REPLAY_COMPLETE → capture attachMomentMs
 *   STREAM_CLOSED → reject pending awaiters + evict session so the next
 *                   ensureSession()/select() call re-establishes cleanly
 *   SCENE_ACTIVITY → workspaceStore.bumpUnread(sceneId)
 *   event frame → workspaceStore.ingestEvent(sessionId, ev)
 *
 * Generation guard: every shared-state write checks
 *   `localGen === session.streamGeneration && !signal.aborted`
 * so a stale frame from a superseded stream cannot clobber active state.
 * On each (re)connect the session's streamGeneration is incremented and a
 * fresh AbortController is created; the prior controller is aborted first.
 *
 * Reconnect backoff: held on the AltSession so it survives across the while
 * loop iterations. Starts at 1 s, doubles to 30 s cap on each failed attempt,
 * resets to 1 s after a successful STREAM_OPENED.
 *
 * awaitConnectionId: resolves on STREAM_OPENED, rejects on close/error or
 * after a 10 s timeout. After rejection a fresh promise is installed so the
 * next reconnect/ensureSession can await again.
 */

import { ControlSignal } from '$lib/connect/holomush/web/v1/web_pb';
import { client } from './client';
import { workspaceStore } from './workspaceStore.svelte';
import { isStaleSession } from '$lib/util/stale';

export const AWAIT_CONNECTION_TIMEOUT_MS = 10_000;
const RECONNECT_FLOOR_MS = 1_000;
const RECONNECT_CAP_MS = 30_000;

interface ConnectionIdGate {
	promise: Promise<string>;
	resolve: (id: string) => void;
	reject: (reason?: unknown) => void;
}

interface AltSession {
	characterId: string;
	sessionId: string;
	/** Most-recently captured connectionId (null until STREAM_OPENED fires). */
	connectionId: string | null;
	/** Current gate for awaitConnectionId. Replaced after rejection. */
	gate: ConnectionIdGate;
	/** Monotonically increasing per-session stream generation. */
	streamGeneration: number;
	/** AbortController for the current stream. */
	controller: AbortController;
	/** Backoff delay for the next failed reconnect attempt (ms). */
	reconnectDelayMs: number;
}

// characterId → AltSession
const sessions = new Map<string, AltSession>();

// ── internal helpers ────────────────────────────────────────────────────────

function makeGate(): ConnectionIdGate {
	let resolve!: (id: string) => void;
	let reject!: (reason?: unknown) => void;
	const promise = new Promise<string>((res, rej) => {
		resolve = res;
		reject = rej;
	});
	// Suppress unhandled-rejection noise: the gate is always consumed by
	// awaitConnectionId (or closeSession), but if no consumer is currently
	// awaiting when the rejection fires, Node/vitest would surface it as an
	// unhandled rejection. The no-op catch keeps the promise observable while
	// preventing that noise; real consumers still see the rejection via race().
	promise.catch(() => {});
	return { promise, resolve, reject };
}

/**
 * Opens the StreamEvents loop for a given session. Runs until the stream ends,
 * erroring, or the session is evicted. Reconnects with backoff on transient
 * errors. Does NOT recurse — uses a while loop to avoid stack growth.
 */
async function openStream(session: AltSession): Promise<void> {
	while (true) {
		// Snapshot generation + controller for this iteration.
		const localGen = session.streamGeneration;
		const localController = session.controller;
		const signal = localController.signal;

		if (signal.aborted) return;

		// Track whether STREAM_OPENED was received in this iteration.
		let streamOpened = false;

		try {
			for await (const response of client.streamEvents(
				{ sessionId: session.sessionId },
				{ signal },
			)) {
				if (response.frame.case === 'control') {
					const ctrl = response.frame.value;

					if (ctrl.signal === ControlSignal.STREAM_OPENED) {
						if (localGen !== session.streamGeneration || signal.aborted) continue;
						streamOpened = true; // mark for stream-end eviction guard below
						// Successful connect: reset backoff.
						session.reconnectDelayMs = RECONNECT_FLOOR_MS;
						session.connectionId = ctrl.connectionId;
						session.gate.resolve(ctrl.connectionId);

					} else if (ctrl.signal === ControlSignal.REPLAY_COMPLETE) {
						if (localGen !== session.streamGeneration || signal.aborted) continue;
						(session as AltSession & { attachMomentMs?: bigint }).attachMomentMs =
							ctrl.attachMomentMs;

					} else if (ctrl.signal === ControlSignal.STREAM_CLOSED) {
						if (localGen !== session.streamGeneration) return;
						// Server-initiated close: reject pending awaiters and evict.
						// The next ensureSession()/select() call will re-establish cleanly.
						session.connectionId = null;
						session.gate.reject(new Error(ctrl.message || 'Stream closed by server'));
						sessions.delete(session.characterId);
						return;

					} else if (ctrl.signal === ControlSignal.SCENE_ACTIVITY) {
						if (localGen !== session.streamGeneration || signal.aborted) continue;
						if (ctrl.sceneId) {
							workspaceStore.bumpUnread(ctrl.sceneId);
						}
					}
					// RECONNECTING / RECONNECTED: no-op for alt sessions (gateway handles).

				} else if (response.frame.case === 'event') {
					if (localGen !== session.streamGeneration || signal.aborted) continue;
					workspaceStore.ingestEvent(session.sessionId, response.frame.value);
				}
			}
			// Normal stream end (server closed without STREAM_CLOSED control frame).
			// If STREAM_OPENED was received the session stays in the map (connectionId
			// is valid; caller may call awaitConnectionId before the next select()).
			// If stream ended before STREAM_OPENED (e.g. server refused or immediate
			// close), reject any pending gate and evict so the next ensureSession
			// re-establishes cleanly.
			if (localGen === session.streamGeneration && !signal.aborted) {
				if (!streamOpened) {
					session.gate.reject(new Error('Stream ended before STREAM_OPENED'));
					sessions.delete(session.characterId);
				}
			}
			return;
		} catch (e) {
			if (signal.aborted) return;
			if (localGen !== session.streamGeneration) return;

			if (isStaleSession(e)) {
				session.connectionId = null;
				session.gate.reject(e);
				sessions.delete(session.characterId);
				return;
			}

			// Transient error: reject any pending gate (if stream never opened),
			// install a fresh gate, apply backoff, then loop to retry.
			if (!streamOpened) {
				session.gate.reject(e);
			}
			// Install a fresh gate for the next connect attempt so awaitConnectionId
			// on the next ensureSession/select can await the new STREAM_OPENED.
			session.gate = makeGate();
			session.connectionId = null;

			const delay = session.reconnectDelayMs;
			session.reconnectDelayMs = Math.min(delay * 2, RECONNECT_CAP_MS);

			// Increment generation + swap controller so this iteration is the
			// sole live stream when we come back around.
			session.streamGeneration += 1;
			session.controller.abort();
			session.controller = new AbortController();

			await new Promise<void>((resolve) => setTimeout(resolve, delay));
			// Continue the while loop.
		}
	}
}

// ── public API ───────────────────────────────────────────────────────────────

/**
 * Ensures an alt session exists for the given characterId.
 * Calls webSelectCharacter with clientType='comms_hub' to get/reattach a
 * session. Idempotent — returns the existing sessionId if already connected.
 */
export async function ensureSession(characterId: string): Promise<string> {
	const existing = sessions.get(characterId);
	if (existing) return existing.sessionId;

	const res = await client.webSelectCharacter({
		characterId,
		clientType: 'comms_hub',
	});

	if (!res.success) {
		throw new Error(`Failed to select character ${characterId}: ${res.errorMessage}`);
	}

	const sessionId = res.sessionId;
	const gate = makeGate();
	const controller = new AbortController();

	const session: AltSession = {
		characterId,
		sessionId,
		connectionId: null,
		gate,
		streamGeneration: 1,
		controller,
		reconnectDelayMs: RECONNECT_FLOOR_MS,
	};

	// Key by characterId for dedup (reattach-safe on the server side).
	sessions.set(characterId, session);

	// Start the stream loop (non-blocking; errors surface as reconnect/re-auth).
	openStream(session);

	return sessionId;
}

/**
 * Returns a promise that resolves once the STREAM_OPENED connectionId has
 * arrived for the character's alt session, or rejects on close/error/timeout.
 * Mirrors the terminal's waitForStreamReady (10 s timeout).
 *
 * Throws if no session exists for the given characterId.
 */
export async function awaitConnectionId(characterId: string): Promise<string> {
	const session = sessions.get(characterId);
	if (!session) {
		throw new Error(`No alt session for character ${characterId}`);
	}
	// Race the gate against a 10 s timeout (mirrors terminal's waitForStreamReady).
	// Cancel the timer when the gate settles first to prevent dangling rejections.
	return new Promise<string>((resolve, reject) => {
		const handle = setTimeout(
			() => reject(new Error('Timed out waiting for STREAM_OPENED')),
			AWAIT_CONNECTION_TIMEOUT_MS,
		);
		session.gate.promise.then(
			(id) => { clearTimeout(handle); resolve(id); },
			(err: unknown) => { clearTimeout(handle); reject(err as Error); },
		);
	});
}

/**
 * Returns the current connectionId for a character's alt session if available,
 * or null if the stream has not opened yet.
 */
export function getConnectionId(characterId: string): string | null {
	return sessions.get(characterId)?.connectionId ?? null;
}

/**
 * Returns the attachMomentMs captured from REPLAY_COMPLETE for the session,
 * or 0n if not yet received. Used by workspaceStore.select() for backfill.
 */
export function getAttachMomentMs(characterId: string): bigint {
	const s = sessions.get(characterId) as (AltSession & { attachMomentMs?: bigint }) | undefined;
	return s?.attachMomentMs ?? 0n;
}

/**
 * Tears down the alt session for a character (e.g. when the workspace closes).
 * Aborts the stream loop and rejects any pending connectionId awaiters.
 */
export function closeSession(characterId: string): void {
	const session = sessions.get(characterId);
	if (!session) return;
	session.gate.reject(new Error('Session closed'));
	session.controller.abort();
	sessions.delete(characterId);
}

/**
 * Tears down all active alt sessions. Called on workspace unmount / logout.
 */
export function closeAllSessions(): void {
	for (const [characterId] of sessions) {
		closeSession(characterId);
	}
}

// Exported for tests only.
export { sessions as _sessions };
