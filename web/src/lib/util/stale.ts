// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ConnectError, Code } from '@connectrpc/connect';

/**
 * Detects "session is stale" RPC errors:
 *  - ConnectRPC Unauthenticated code
 *  - SESSION_NOT_FOUND or SESSION_EXPIRED in the message
 *
 * Used by the terminal page and the streamBackfill module to route the user
 * back to the landing page when their session cookie no longer references a
 * live server-side session (multi-tab logout, session expiry, etc.).
 */
export function isStaleSession(e: unknown): boolean {
	if (e instanceof ConnectError) {
		if (e.code === Code.Unauthenticated) return true;
		return e.message.includes('SESSION_NOT_FOUND') || e.message.includes('SESSION_EXPIRED');
	}
	if (e instanceof Error) {
		return e.message.includes('SESSION_NOT_FOUND') || e.message.includes('SESSION_EXPIRED');
	}
	return false;
}
