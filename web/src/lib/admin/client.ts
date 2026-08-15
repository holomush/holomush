// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';

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
