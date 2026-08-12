// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';

/**
 * Singleton Connect client for the web client's character layer — the single
 * entry point every character surface calls, mirroring $lib/scenes/client.ts.
 *
 * NO WRAPPER BELOW PASSES A SESSION TOKEN. CookieMiddleware injects
 * X-Session-Token on every request and the gateway lifts the caller's identity
 * from that header; a token in a request body would be a client-asserted
 * identity, which is why none of the Web* request messages declares one.
 */
export const client = createClient(WebService, transport);

/**
 * Reads one character's public profile as whichever viewer rung the caller's
 * cookie resolves to, including no cookie at all. A character below the
 * reachability floor and one that does not exist take the same outcome, so a
 * caller cannot tell a withheld profile from a nonexistent one.
 */
export async function getCharacterProfile(characterId: string) {
	const res = await client.webGetCharacterProfile({ characterId });
	return res.character;
}

/**
 * Reads the signed-in player's own roster, retired characters included, in the
 * owner audience's shape — id, name, description, the full profile map, the
 * lifecycle status and the concurrency token an edit sends back.
 */
export async function listMyCharacters() {
	const res = await client.webListMyCharacters({});
	return res.characters;
}

/**
 * Reads one owned character in the shape the edit forms write back. Ownership
 * is verified server-side; a character the caller does not own resolves the
 * same outcome as one that does not exist.
 */
export async function getMyCharacter(characterId: string) {
	const res = await client.webGetMyCharacter({ characterId });
	return res.character;
}

/**
 * Applies a partial edit to the character's stored profile.* rows. `paths` is
 * the set of snake_case field paths to apply; a field whose path is absent is
 * ignored rather than written, and an empty `paths` is a no-op the caller
 * should skip rather than send. Returns the post-write character, whose
 * `version` is the one the next edit must carry.
 */
export async function updateCharacterProfile(args: {
	characterId: string;
	expectedVersion: number;
	paths: string[];
	pronouns?: string;
	concept?: string;
	species?: string;
	age?: string;
	faction?: string;
	appearance?: string;
	personality?: string;
	biography?: string;
	rumors?: string;
	currently?: string;
	rpPreferences?: string;
	timezone?: string;
}) {
	const res = await client.webUpdateCharacterProfile({
		characterId: args.characterId,
		expectedVersion: args.expectedVersion,
		pronouns: args.pronouns ?? '',
		concept: args.concept ?? '',
		species: args.species ?? '',
		age: args.age ?? '',
		faction: args.faction ?? '',
		appearance: args.appearance ?? '',
		personality: args.personality ?? '',
		biography: args.biography ?? '',
		rumors: args.rumors ?? '',
		currently: args.currently ?? '',
		rpPreferences: args.rpPreferences ?? '',
		timezone: args.timezone ?? '',
		updateMask: { paths: args.paths },
	});
	return res.character;
}

/**
 * Replaces the in-world `look` text — the characters.description column, not a
 * profile.* row. An empty description clears the column. Returns the post-write
 * character so the caller re-reads its new version rather than guessing it.
 */
export async function updateCharacterDescription(args: {
	characterId: string;
	expectedVersion: number;
	description: string;
}) {
	const res = await client.webUpdateCharacterDescription({
		characterId: args.characterId,
		expectedVersion: args.expectedVersion,
		description: args.description,
	});
	return res.character;
}

/**
 * Points the signed-in player's default character at `characterId` and returns
 * the whole post-write roster, so the caller re-renders its cards from server
 * truth rather than patching a local copy.
 *
 * There is no expectedVersion: the write targets a `players` row, which carries
 * no concurrency token. A retired character is refused server-side.
 */
export async function setDefaultCharacter(characterId: string) {
	const res = await client.webSetDefaultCharacter({ characterId });
	return res.characters;
}
