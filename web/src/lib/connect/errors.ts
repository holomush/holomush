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

/** Which screen an admin-portal refusal resolves to. */
export type AdminFailureClass = 'denial' | 'infrastructure';

/**
 * Classifies an admin-portal refusal into the one of two screens it resolves
 * to. It reads the gRPC code and NOTHING else.
 *
 * Splitting refusal-from-outage is a DIFFERENT AXIS from splitting on which
 * refusal arrived. The first is safe: a viewer who may not use this surface and
 * one who may see the same retry screen during an outage, so the split does not
 * correlate with what the caller is allowed to do. The second would reconstruct
 * the enumeration oracle the whole surface is built to close, which is why no
 * authored source here reads or renders a refusal code string.
 *
 * The function is TOTAL, and its residue is deliberate. Unavailable,
 * DeadlineExceeded and a non-ConnectError transport throw are infrastructure.
 * EVERYTHING else — including Unauthenticated, Internal, Unknown, an
 * unexpected FailedPrecondition, and any code added later — falls to 'denial'.
 *
 * That default is FAIL-SAFE, not fail-open: the denial branch renders the
 * ordinary not-found, which is the most conservative thing this surface can
 * show and exactly what an unauthorised viewer already sees. Defaulting the
 * other way would render a retry state for a genuine refusal, telling the
 * viewer something is here to retry — which is itself the disclosure.
 */
export function classifyAdminFailure(e: unknown): AdminFailureClass {
	if (e instanceof ConnectError) {
		if (e.code === Code.Unavailable || e.code === Code.DeadlineExceeded) {
			return 'infrastructure';
		}
		return 'denial';
	}
	// A throw that never reached a server carries no answer about the caller.
	return 'infrastructure';
}
