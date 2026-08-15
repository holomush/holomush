// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import type { AdminCharacter } from '$lib/connect/holomush/adminportal/v1/adminportal_pb';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';

/**
 * The eight fields the admin list projection carries, derived FROM the
 * generated message rather than restated beside it. A `Pick` keeps the field
 * names and their wire types (notably the two int64 nanosecond columns, which
 * arrive as bigint) from drifting into a second, hand-maintained web-side
 * character shape — and lets a test build a fixture without the `$typeName`
 * discriminator a full message literal would demand.
 */
export type CharacterRow = Pick<
  AdminCharacter,
  'id' | 'playerId' | 'playerUsername' | 'name' | 'status' | 'lastActiveAt' | 'createdAt' | 'version'
>;

/**
 * The five columns a header click can order by. `version` is deliberately
 * absent: it is a concurrency guard rather than a §11.3 field, and the wire
 * enum has no value that could express an ordering on it.
 */
export type CharacterSortField = 'name' | 'player' | 'status' | 'lastActive' | 'created';

/**
 * Singleton Connect client for the web client's admin layer — the single entry
 * point every admin surface calls, mirroring $lib/characters/client.ts.
 *
 * NO WRAPPER BELOW PASSES A SESSION TOKEN. CookieMiddleware injects
 * X-Session-Token on every request and the gateway lifts the caller's identity
 * from that header; a token in a request body would be a client-asserted
 * identity, which is why none of the Web* request messages declares one.
 */
export const client = createClient(WebService, transport);

/**
 * Reads the admin sections this caller may use. The response array IS the
 * server's answer — the nav is a projection of it and adds nothing to it. A
 * caller who may use none of them does not receive an empty list; the call is
 * refused, and the refusal resolves to the ordinary not-found.
 */
export async function listAdminSections() {
	const res = await client.webAdminListSections({});
	return res.sections;
}
