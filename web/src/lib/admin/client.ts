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
 * not trimmed, lower-cased, compatibility-folded or length-gated here. (The
 * exact folding is deliberately not named: which form applies is the server's
 * business, and naming it here would start a second record of it.) Search equality
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
 * What the edit surface needs from a single-character read, stated
 * STRUCTURALLY rather than as the generated message type.
 *
 * `AdminCharacterDetail` satisfies this by construction, so `getAdminCharacter`
 * is assignable to a prop of this shape — but a test fixture can be written as
 * a plain object without the `$typeName` discriminator a full message literal
 * would demand, and the Sheet's contract says exactly which three things it
 * reads and no more.
 *
 * `character.version` is here because a version conflict is answered with a
 * fresh single-character read: the alert names the server's current version,
 * which is a NUMBER the client learned, never a string the server sent.
 */
export interface CharacterDetail {
  character?: { version: number };
  /** characters.description — the in-world `look` text. */
  description: string;
  /** The twelve governed profile values, keyed by their §7.2 path. */
  profile: Record<string, string>;
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

/**
 * The flat request field a mask path travels in: the path minus its `profile.`
 * prefix, which is the server's own naming rule
 * (adminProfileMaskablePaths -> WebAdminUpdateCharacterRequest fields 4-16).
 *
 * ONE RULE, not a second lookup table. A table would be a client-side copy of
 * a mapping the wire already fixes, and copies drift.
 */
export const adminWireField = (path: string): string =>
  path.startsWith('profile.') ? path.slice('profile.'.length) : path;

/**
 * One partial edit against the optimistic-concurrency guard.
 *
 * `paths` IS the update mask, and it carries only what changed. The server
 * compares paths by EXACT string against its closed thirteen-path allowlist —
 * no prefix, no wildcard, no dotted-subtree expansion — and an unlisted path is
 * rejected rather than ignored, so nothing here needs to police the set.
 *
 * `expectedVersion` is never defaulted and never clamped on this side. The core
 * refuses an absent, zero or negative value before any domain call, and answers
 * a stale one with Aborted; a client that filled it in would be turning the
 * guard off exactly when it matters.
 */
export async function updateAdminCharacter(args: {
  characterId: string;
  expectedVersion: number;
  paths: string[];
  values: Record<string, string>;
}): Promise<CharacterRow | undefined> {
  const req: Record<string, unknown> = {
    characterId: args.characterId,
    expectedVersion: args.expectedVersion,
    updateMask: { paths: args.paths },
  };
  for (const p of args.paths) req[adminWireField(p)] = args.values[p] ?? '';
  const res = await client.webAdminUpdateCharacter(req);
  return res.character;
}

/**
 * The retire transition. It sends a character id and a version and NOTHING
 * else: §9.3 keeps the lifecycle vocabulary off the wire so `idle` stays
 * unreachable, and there is no field on this request that could carry a status
 * value even if a caller wanted to.
 */
export async function retireAdminCharacter(
  characterId: string,
  expectedVersion: number,
): Promise<CharacterRow | undefined> {
  const res = await client.webAdminRetireCharacter({ characterId, expectedVersion });
  return res.character;
}

/** The un-retire transition, under the same guard rules and the same shape. */
export async function unretireAdminCharacter(
  characterId: string,
  expectedVersion: number,
): Promise<CharacterRow | undefined> {
  const res = await client.webAdminUnretireCharacter({ characterId, expectedVersion });
  return res.character;
}
