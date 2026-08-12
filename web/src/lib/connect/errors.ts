// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ConnectError, Code } from '@connectrpc/connect';

/**
 * Returns true when the given value is a ConnectRPC error with code
 * Unimplemented. Used by the web client to tolerate staged rollouts where
 * the server may not yet implement a new RPC.
 */
export function isUnimplementedError(e: unknown): boolean {
	return e instanceof ConnectError && e.code === Code.Unimplemented;
}

/**
 * Returns true when the given value is a ConnectRPC error with code NotFound.
 *
 * This is the ONLY condition `/c/[id]` branches on. 01-SPEC §9.6 returns one
 * code for "no such character" and for "below this viewer's reachability
 * floor" deliberately, and the facade returns one shared message literal for
 * both; a second client-side branch distinguishing them would reconstruct the
 * exact disclosure the uniform code exists to prevent.
 */
export function isNotFoundError(e: unknown): boolean {
	return e instanceof ConnectError && e.code === Code.NotFound;
}

/**
 * Returns true when the given value is a ConnectRPC error with code Aborted —
 * the code an optimistic-concurrency refusal arrives as, so a per-section save
 * on the character authoring surface can tell "someone else edited this" from
 * an ordinary failure.
 */
export function isAbortedError(e: unknown): boolean {
	return e instanceof ConnectError && e.code === Code.Aborted;
}

/**
 * Returns true when the given value is a ConnectRPC error with code
 * AlreadyExists — the character-creation surface's name-taken refusal.
 */
export function isAlreadyExistsError(e: unknown): boolean {
	return e instanceof ConnectError && e.code === Code.AlreadyExists;
}

/**
 * Returns true when the given value is a ConnectRPC error with code
 * InvalidArgument — the character-creation surface's name-invalid refusal,
 * whose server-supplied message is authored for players and is rendered
 * verbatim rather than replaced with client-side copy.
 */
export function isInvalidArgumentError(e: unknown): boolean {
	return e instanceof ConnectError && e.code === Code.InvalidArgument;
}
