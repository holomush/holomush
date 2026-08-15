// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import type {
  AdminCharacter,
  AdminCharacterDetail,
} from '$lib/connect/holomush/adminportal/v1/adminportal_pb';
import {
  AdminCharacterSortField,
  AdminCharacterStatusFilter,
} from '$lib/connect/holomush/adminportal/v1/adminportal_pb';
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

/** The closed lifecycle filter vocabulary, plus the unfiltered case. */
export type CharacterStatusFilter = 'all' | 'active' | 'retired' | 'idle';

/**
 * The four options the one status control offers, in render order.
 *
 * `Idle` is listed because it is a real lifecycle value that could exist in
 * data. Listing it in a FILTER does not put it on a mutation wire, which is
 * what §10.6 prohibits — the lifecycle transitions are their own RPCs and
 * neither of them accepts a status value.
 */
export const ADMIN_STATUS_FILTERS: { value: CharacterStatusFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'active', label: 'Active' },
  { value: 'retired', label: 'Retired' },
  { value: 'idle', label: 'Idle' },
];

/**
 * The requested page size. It is a REQUEST, never a guarantee: the core clamps
 * it to at most 50 (adminportal.proto:282-286, :321-322) because a clamp on
 * this side would be bypassable by anything speaking gRPC to core directly. So
 * the rendered range is computed from what came back, not from what was asked.
 */
export const ADMIN_PAGE_SIZE = 50;

/** What the table draws: the server's page, and the server's own total. */
export interface CharacterPage {
  rows: CharacterRow[];
  /**
   * The count the SERVER computed with its own scalar COUNT over the same
   * filter, in the same read transaction as the page — correct even for a page
   * beyond the last one. The client never derives, sums or estimates it.
   *
   * A total is safe on THIS surface and only here: the admin list is not
   * privacy-partitioned, and its audience already sees every field it could
   * order by. It MUST NOT be cited as precedent for a count on the public
   * character directory, whose row set differs per viewer and where a total
   * would disclose the size of the withheld remainder.
   */
  totalCount: bigint;
}

/** Everything the list and search calls share. */
export interface CharacterQuery {
  sortField: CharacterSortField;
  descending: boolean;
  status: CharacterStatusFilter;
  /** The click-to-filter equality field; '' means no player filter. */
  playerId: string;
  /** 1-based. The core refuses anything below 1 rather than computing a negative offset. */
  page: number;
}

const SORT_FIELD_WIRE: Record<CharacterSortField, AdminCharacterSortField> = {
  name: AdminCharacterSortField.NAME,
  player: AdminCharacterSortField.PLAYER_USERNAME,
  status: AdminCharacterSortField.STATUS,
  lastActive: AdminCharacterSortField.LAST_ACTIVE_AT,
  created: AdminCharacterSortField.CREATED_AT,
};

const STATUS_WIRE: Record<CharacterStatusFilter, AdminCharacterStatusFilter> = {
  all: AdminCharacterStatusFilter.UNSPECIFIED,
  active: AdminCharacterStatusFilter.ACTIVE,
  retired: AdminCharacterStatusFilter.RETIRED,
  idle: AdminCharacterStatusFilter.IDLE,
};

const wireQuery = (q: CharacterQuery) => ({
  sortField: SORT_FIELD_WIRE[q.sortField],
  descending: q.descending,
  statusFilter: STATUS_WIRE[q.status],
  playerId: q.playerId,
  page: q.page,
  pageSize: ADMIN_PAGE_SIZE,
});

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

/** One page of the admin character list, in the server's own order. */
export async function listAdminCharacters(q: CharacterQuery): Promise<CharacterPage> {
  const res = await client.webAdminListCharacters(wireQuery(q));
  return { rows: res.characters, totalCount: res.totalCount };
}

/**
 * The same page, narrowed by a substring term.
 *
 * `term` IS THE RAW STRING THE OPERATOR TYPED, forwarded byte for byte. It is
 * not trimmed, lower-cased, NFKC-folded or length-gated here. Search equality
 * is defined SERVER-SIDE and nowhere else: the core normalizes the term through
 * the single charname pipeline that produced the stored normal form. A second
 * definition of which strings are equal — living in TypeScript, drifting on its
 * own schedule — is what this deliberately does not have.
 */
export async function searchAdminCharacters(
  q: CharacterQuery,
  term: string,
): Promise<CharacterPage> {
  const res = await client.webAdminSearchCharacters({ ...wireQuery(q), query: term });
  return { rows: res.characters, totalCount: res.totalCount };
}

/**
 * The single-character detail read behind the edit surface. It is a separate
 * call rather than a seed from a list row because the list projection carries
 * no profile prose at all — a bulk cross-player projection of player-authored
 * text is the thing the two-message split exists to prevent.
 */
export async function getAdminCharacter(
  characterId: string,
): Promise<AdminCharacterDetail | undefined> {
  const res = await client.webAdminGetCharacter({ characterId });
  return res.character;
}
