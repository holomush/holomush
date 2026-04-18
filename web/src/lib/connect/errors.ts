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
